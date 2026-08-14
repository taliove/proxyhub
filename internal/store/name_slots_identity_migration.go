package store

import "fmt"

// migrateNameSlotsIdentity 槽位身份迁移(032 / issue #112):name_slots 表重建为
// 自增 ID 主键,(user_id, name) 主键替换为字面名部分唯一索引(仅当名字不含
// {index} 时唯一——为 issue #113 的「含 {index} 模板重名放行」在 DB 层先行就位;
// 本票应用层查重不变,行为零变化)。重建按 rowid 顺序拷贝,自增 ID 即创建顺序,
// 存量行 created_at/updated_at 原样保留。幂等:id 列已存在则整体跳过。
func (s *Store) migrateNameSlotsIdentity() error {
	if s.columnExistsUnlocked("name_slots", "id") {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin name_slots identity migration: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmts := []string{
		`CREATE TABLE name_slots_new (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id    INTEGER NOT NULL DEFAULT 0,
			name       TEXT NOT NULL,
			node_key   TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		// rowid 顺序 = 历史插入顺序,迁移后 ID 序与创建顺序一致
		`INSERT INTO name_slots_new (user_id, name, node_key, created_at, updated_at)
			SELECT user_id, name, node_key, created_at, updated_at FROM name_slots ORDER BY rowid`,
		`DROP TABLE name_slots`,
		`ALTER TABLE name_slots_new RENAME TO name_slots`,
		`CREATE UNIQUE INDEX idx_name_slots_name_literal
			ON name_slots(user_id, name) WHERE name NOT LIKE '%{index}%'`,
		`CREATE UNIQUE INDEX idx_name_slots_node
			ON name_slots(user_id, node_key) WHERE node_key != ''`,
	}
	for _, stmt := range stmts {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("name_slots identity migration: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit name_slots identity migration: %w", err)
	}
	return nil
}
