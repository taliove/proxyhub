package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// migrateNodeServerGeo 为既有库幂等补建 node_server_geo 表(issue #37)。
// 新库已由 store.go 基础 schema 建出;CREATE TABLE IF NOT EXISTS 每次启动重跑安全。
func (s *Store) migrateNodeServerGeo() error {
	if _, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS node_server_geo (
			host         TEXT PRIMARY KEY,
			country_code TEXT NOT NULL DEFAULT '',
			resolved_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		return fmt.Errorf("create node_server_geo table: %w", err)
	}
	return nil
}

// GetServerGeo 读取节点 Server(域名)的 GeoIP 缓存行。ok=false 表示无记录。
// country_code 空串 = 负缓存行(DNS 失败/无记录);新鲜度判定(TTL)在调用方(识别层)。
func (s *Store) GetServerGeo(host string) (code string, resolvedAt time.Time, ok bool) {
	var resolvedAtStr string
	err := s.db.QueryRow(`SELECT country_code, resolved_at FROM node_server_geo WHERE host = ?`, host).
		Scan(&code, &resolvedAtStr)
	if errors.Is(err, sql.ErrNoRows) {
		return "", time.Time{}, false
	}
	if err != nil {
		return "", time.Time{}, false
	}
	return code, parseTimeOrZero(&resolvedAtStr), true
}

// PutServerGeo 写入/覆盖一行缓存(INSERT OR REPLACE:主键替换天然幂等;
// 并发写安全由 SQLite 单写者连接池保证,见 store.Open 的 SetMaxOpenConns(1))。
// code 空串 = 负缓存。
func (s *Store) PutServerGeo(host, code string) error {
	if _, err := s.db.Exec(`INSERT OR REPLACE INTO node_server_geo
		(host, country_code, resolved_at) VALUES (?, ?, CURRENT_TIMESTAMP)`, host, code); err != nil {
		return fmt.Errorf("put server geo %s: %w", host, err)
	}
	return nil
}
