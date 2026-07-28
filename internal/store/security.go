package store

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

func isNoRows(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}

// bannedUntilTimeLayout is the on-disk layout for banned_ips timestamps: UTC
// "2006-01-02 15:04:05", the only shape SQLite's own datetime() can parse
// (ADR 0010). Binding a time.Time directly would let the modernc driver
// serialise it via time.Time.String(), monotonic-clock suffix included, which
// datetime() reads as NULL.
const bannedUntilTimeLayout = "2006-01-02 15:04:05"

// IsBanned 检查 IP 是否处于封禁期。
// banned_until 按裸文本读出后自行解析，兼容新格式与旧的 Go String 格式
// （见 parseBannedUntil）。无法解析的值按"未封禁"处理并告警，避免脏数据把
// 管理员永久锁在门外。
func (s *Store) IsBanned(ip string, now time.Time) (bool, error) {
	var bannedUntil sql.NullString
	err := s.db.QueryRow(
		`SELECT CAST(banned_until AS TEXT) FROM banned_ips WHERE ip = ?`, ip,
	).Scan(&bannedUntil)
	if err != nil {
		if isNoRows(err) {
			return false, nil
		}
		return false, fmt.Errorf("query ban: %w", err)
	}
	if !bannedUntil.Valid || bannedUntil.String == "" {
		return false, nil
	}
	until, ok := parseBannedUntil(bannedUntil.String)
	if !ok {
		slog.Warn("banned_ips: unparsable banned_until, treating ip as not banned",
			"ip", ip, "value", bannedUntil.String)
		return false, nil
	}
	return until.After(now), nil
}

// RecordLoginFailure 记录一次登录失败。
// 失败次数达到 threshold 时封禁 banDuration，返回是否触发封禁。
func (s *Store) RecordLoginFailure(ip string, threshold int, banDuration time.Duration, now time.Time) (bool, error) {
	if ip == "" {
		return false, errors.New("ip is required")
	}

	tx, err := s.db.Begin()
	if err != nil {
		return false, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	nowStr := now.UTC().Format(bannedUntilTimeLayout)
	_, err = tx.Exec(`
		INSERT INTO banned_ips (ip, fail_count, updated_at) VALUES (?, 1, ?)
		ON CONFLICT(ip) DO UPDATE SET fail_count = fail_count + 1, updated_at = ?`,
		ip, nowStr, nowStr)
	if err != nil {
		return false, fmt.Errorf("record failure: %w", err)
	}

	var failCount int
	if err := tx.QueryRow(`SELECT fail_count FROM banned_ips WHERE ip = ?`, ip).Scan(&failCount); err != nil {
		return false, fmt.Errorf("query fail count: %w", err)
	}

	banned := failCount >= threshold
	if banned {
		bannedUntil := now.Add(banDuration).UTC().Format(bannedUntilTimeLayout)
		if _, err := tx.Exec(
			`UPDATE banned_ips SET banned_until = ?, fail_count = 0 WHERE ip = ?`,
			bannedUntil, ip); err != nil {
			return false, fmt.Errorf("set ban: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit: %w", err)
	}
	return banned, nil
}

// ResetLoginFailures 登录成功后清空失败计数
func (s *Store) ResetLoginFailures(ip string) error {
	_, err := s.db.Exec(`DELETE FROM banned_ips WHERE ip = ?`, ip)
	if err != nil {
		return fmt.Errorf("reset failures: %w", err)
	}
	return nil
}

// BanIP 立即封禁 IP 一段时间（用于蜜罐命中等高危场景，无需累计失败次数）。
// 返回封禁截止时间。
func (s *Store) BanIP(ip string, banDuration time.Duration, now time.Time) (time.Time, error) {
	if ip == "" {
		return time.Time{}, errors.New("ip is required")
	}
	bannedUntil := now.Add(banDuration)
	_, err := s.db.Exec(`
		INSERT INTO banned_ips (ip, fail_count, banned_until, updated_at)
		VALUES (?, 0, ?, ?)
		ON CONFLICT(ip) DO UPDATE SET banned_until = excluded.banned_until, updated_at = excluded.updated_at`,
		ip, bannedUntil.UTC().Format(bannedUntilTimeLayout), now.UTC().Format(bannedUntilTimeLayout))
	if err != nil {
		return time.Time{}, fmt.Errorf("ban ip: %w", err)
	}
	return bannedUntil, nil
}

// GetSetting 读取全局设置项，不存在返回 ErrNotFound。
// 数据模型多租户化(ticket 06)后读写落在 system_settings;遗留 settings 表仅
// 作回滚备份保留,contract 阶段才删除。每用户覆盖走 GetUserSetting。
func (s *Store) GetSetting(key string) (string, error) {
	var value string
	err := s.db.QueryRow(`SELECT value FROM system_settings WHERE key = ?`, key).Scan(&value)
	if err != nil {
		if isNoRows(err) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("get setting: %w", err)
	}
	return value, nil
}

// SetSetting 写入全局设置项(落 system_settings,见 GetSetting 注释)。
func (s *Store) SetSetting(key, value string) error {
	if key == "" {
		return errors.New("key is required")
	}
	_, err := s.db.Exec(`
		INSERT INTO system_settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value)
	if err != nil {
		return fmt.Errorf("set setting: %w", err)
	}
	return nil
}
