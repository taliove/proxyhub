package store

import (
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// AuditEvent 审计事件记录
type AuditEvent struct {
	ID        int64     `json:"id"`
	EventType string    `json:"event_type"` // login_success | login_failure | honeypot_ban | threshold_ban
	IP        string    `json:"ip"`
	Username  string    `json:"username"`
	Detail    string    `json:"detail"`
	UserAgent string    `json:"user_agent"`
	CreatedAt time.Time `json:"created_at"`
}

// AuditFilter 审计事件过滤条件
type AuditFilter struct {
	EventTypes []string  // 为空则不过滤
	IP         string    // 为空则不过滤
	Since      time.Time // 零值则不过滤（只看 Since 之后的）
}

// RecordAuditEvent 记录一条审计事件。
// created_at 显式写入 UTC "2006-01-02 15:04:05" 格式，保证与 SQLite datetime() 兼容
// （modernc 驱动的 CURRENT_TIMESTAMP 会写成 RFC3339 的 "T...Z" 形式，datetime() 无法解析）。
func (s *Store) RecordAuditEvent(eventType, ip, username, detail, userAgent string) error {
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	_, err := s.db.Exec(
		`INSERT INTO audit_logs (event_type, ip, username, detail, user_agent, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		eventType, ip, username, detail, userAgent, now)
	if err != nil {
		return fmt.Errorf("insert audit log: %w", err)
	}
	return nil
}

// ListAuditEvents 列出审计事件（倒序：最新在前），支持过滤和分页
func (s *Store) ListAuditEvents(filter AuditFilter, limit, offset int) ([]*AuditEvent, int, error) {
	// 构造 WHERE 子句
	var whereClauses []string
	var args []any

	if len(filter.EventTypes) > 0 {
		placeholders := make([]string, len(filter.EventTypes))
		for i, et := range filter.EventTypes {
			placeholders[i] = "?"
			args = append(args, et)
		}
		whereClauses = append(whereClauses, fmt.Sprintf("event_type IN (%s)", strings.Join(placeholders, ",")))
	}
	if filter.IP != "" {
		whereClauses = append(whereClauses, "ip = ?")
		args = append(args, filter.IP)
	}
	if !filter.Since.IsZero() {
		// 用 datetime() 包裹两侧，强制 SQLite 按时间语义比较（避免 TIMESTAMP 列的
		// 字符串亲和性导致的比较异常）。created_at 存的是 UTC，Since 也转 UTC。
		whereClauses = append(whereClauses, "datetime(created_at) >= datetime(?)")
		args = append(args, filter.Since.UTC().Format("2006-01-02 15:04:05"))
	}

	where := ""
	if len(whereClauses) > 0 {
		where = "WHERE " + strings.Join(whereClauses, " AND ")
	}

	// 总数
	var total int
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM audit_logs %s", where)
	if err := s.db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count audit logs: %w", err)
	}

	// 分页查询
	query := fmt.Sprintf(`SELECT id, event_type, ip, username, detail, user_agent, created_at
		FROM audit_logs %s ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`, where)
	args = append(args, limit, offset)
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query audit logs: %w", err)
	}
	defer rows.Close()

	var events []*AuditEvent
	for rows.Next() {
		var e AuditEvent
		var createdAt string
		var detail sql.NullString // detail 可空
		var userAgent sql.NullString // user_agent 可空（兼容旧记录）
		if err := rows.Scan(&e.ID, &e.EventType, &e.IP, &e.Username, &detail, &userAgent, &createdAt); err != nil {
			return nil, 0, fmt.Errorf("scan audit event: %w", err)
		}
		e.Detail = detail.String
		e.UserAgent = userAgent.String
		e.CreatedAt = parseSQLiteTime(createdAt)
		events = append(events, &e)
	}
	return events, total, rows.Err()
}

// PruneAuditLogs 删除早于指定时间的审计记录。
// 用 datetime() 包裹两侧按时间语义比较，与 ListAuditEvents 一致（见 ADR 0010）。
func (s *Store) PruneAuditLogs(olderThan time.Time) error {
	cutoff := olderThan.UTC().Format("2006-01-02 15:04:05")
	_, err := s.db.Exec(`DELETE FROM audit_logs WHERE datetime(created_at) < datetime(?)`, cutoff)
	if err != nil {
		return fmt.Errorf("prune audit logs: %w", err)
	}
	return nil
}

// BannedIP 封禁 IP 信息
type BannedIP struct {
	IP          string    `json:"ip"`
	FailCount   int       `json:"fail_count"`
	BannedUntil time.Time `json:"banned_until"` // 零值表示未封禁（只有失败计数）
}

// ListBannedIPs 列出所有 banned_ips 表中的记录（含封禁中 + 失败计数未达阈值的）。
// banned_until 走 parseBannedUntil，兼容新 UTC 字符串格式与旧 Go String 格式。
func (s *Store) ListBannedIPs() ([]*BannedIP, error) {
	rows, err := s.db.Query(`SELECT ip, fail_count, CAST(banned_until AS TEXT) FROM banned_ips ORDER BY updated_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("query banned ips: %w", err)
	}
	defer rows.Close()

	var banned []*BannedIP
	for rows.Next() {
		var b BannedIP
		var bannedUntilStr sql.NullString
		if err := rows.Scan(&b.IP, &b.FailCount, &bannedUntilStr); err != nil {
			return nil, fmt.Errorf("scan banned ip: %w", err)
		}
		if bannedUntilStr.Valid && bannedUntilStr.String != "" {
			if until, ok := parseBannedUntil(bannedUntilStr.String); ok {
				b.BannedUntil = until
			} else {
				slog.Warn("banned_ips: unparsable banned_until, reporting as not banned",
					"ip", b.IP, "value", bannedUntilStr.String)
			}
		}
		banned = append(banned, &b)
	}
	return banned, rows.Err()
}

// UnbanIP 手动解封 IP（清空 banned_until 和 fail_count）
func (s *Store) UnbanIP(ip string) error {
	_, err := s.db.Exec(`UPDATE banned_ips SET banned_until = NULL, fail_count = 0 WHERE ip = ?`, ip)
	if err != nil {
		return fmt.Errorf("unban ip: %w", err)
	}
	return nil
}
