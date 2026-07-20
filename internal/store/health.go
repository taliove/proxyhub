package store

import (
	"fmt"
)

// HealthRecord 一次节点健康检查记录
type HealthRecord struct {
	NodeKey   string
	Name      string
	Source    string
	Available bool
	LatencyMS int
}

// RecordHealth 批量写入健康检查结果
func (s *Store) RecordHealth(records []HealthRecord) error {
	if len(records) == 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(
		`INSERT INTO node_health (node_key, name, source, available, latency_ms) VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()

	for _, rec := range records {
		if _, err := stmt.Exec(rec.NodeKey, rec.Name, rec.Source, boolToInt(rec.Available), rec.LatencyMS); err != nil {
			return fmt.Errorf("insert health: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// PruneHealth 清理 N 天前的健康历史，避免数据库无限增长
func (s *Store) PruneHealth(days int) error {
	_, err := s.db.Exec(
		fmt.Sprintf(`DELETE FROM node_health WHERE checked_at < datetime('now', '-%d days')`, days))
	if err != nil {
		return fmt.Errorf("prune health: %w", err)
	}
	return nil
}
