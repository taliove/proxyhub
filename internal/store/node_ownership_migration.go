package store

import "fmt"

// migrateNodeOwnershipScope 把 node_blocks / node_overrides 的主键从单列 node_key
// 重建为 (user_id, node_key) 联合主键(多租户写隔离):同一节点可被不同用户
// 独立屏蔽/覆盖,互不串扰。ticket 06 已铺 user_id 列,本迁移只改主键形态。
// 幂等:user_id 已在主键中(新装库或已迁移)直接跳过。
// 与 migrations/021_node_ownership_scope.sql 保持同步(.sql 为 schema 参考,
// 真正执行器是本函数——主键形态检测只能走 PRAGMA,纯 SQL 无法幂等)。
func (s *Store) migrateNodeOwnershipScope() error {
	tables := []struct {
		name    string
		columns string // 除主键外的列定义(与现表一致)
		copyCols string
	}{
		{
			name: "node_blocks",
			columns: `
	user_id    INTEGER NOT NULL DEFAULT 0,
	node_key   TEXT NOT NULL,
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	PRIMARY KEY (user_id, node_key)`,
			copyCols: "user_id, node_key, created_at",
		},
		{
			name: "node_overrides",
			columns: `
	user_id      INTEGER NOT NULL DEFAULT 0,
	node_key     TEXT NOT NULL,
	display_name TEXT NOT NULL DEFAULT '',
	region       TEXT NOT NULL DEFAULT '',
	updated_at   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	PRIMARY KEY (user_id, node_key)`,
			copyCols: "user_id, node_key, display_name, region, updated_at",
		},
	}

	for _, tbl := range tables {
		scoped, err := s.hasUserIDInPK(tbl.name)
		if err != nil {
			return fmt.Errorf("check %s pk: %w", tbl.name, err)
		}
		if scoped {
			continue
		}
		tmp := tbl.name + "_scoped"
		stmts := []string{
			`DROP TABLE IF EXISTS ` + tmp,
			`CREATE TABLE ` + tmp + ` (` + tbl.columns + `)`,
			`INSERT INTO ` + tmp + ` (` + tbl.copyCols + `) SELECT ` + tbl.copyCols + ` FROM ` + tbl.name,
			`DROP TABLE ` + tbl.name,
			`ALTER TABLE ` + tmp + ` RENAME TO ` + tbl.name,
			`CREATE INDEX IF NOT EXISTS idx_` + tbl.name + `_user_id ON ` + tbl.name + `(user_id)`,
		}
		for _, q := range stmts {
			if _, err := s.db.Exec(q); err != nil {
				return fmt.Errorf("rebuild %s: %w", tbl.name, err)
			}
		}
	}
	return nil
}

// hasUserIDInPK 报告 user_id 是否在表主键中(PRAGMA table_info:pk>0 即主键成员)。
func (s *Store) hasUserIDInPK(table string) (bool, error) {
	rows, err := s.db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			cid     int
			name    string
			ctype   string
			notNull int
			dflt    any
			pk      int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notNull, &dflt, &pk); err != nil {
			return false, err
		}
		if name == "user_id" && pk > 0 {
			return true, nil
		}
	}
	return false, rows.Err()
}
