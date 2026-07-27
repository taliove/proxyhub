-- 021: node_blocks / node_overrides 主键重建为 (user_id, node_key) 联合主键(多租户写隔离)。
-- 与 internal/store/node_ownership_migration.go 保持同步:本文件仅作 schema 参考,
-- 真正执行器是 migrateNodeOwnershipScope(PRAGMA 检测主键形态,幂等)。
CREATE TABLE node_blocks_scoped (
	user_id    INTEGER NOT NULL DEFAULT 0,
	node_key   TEXT NOT NULL,
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	PRIMARY KEY (user_id, node_key)
);
INSERT INTO node_blocks_scoped (user_id, node_key, created_at)
	SELECT user_id, node_key, created_at FROM node_blocks;
DROP TABLE node_blocks;
ALTER TABLE node_blocks_scoped RENAME TO node_blocks;
CREATE INDEX IF NOT EXISTS idx_node_blocks_user_id ON node_blocks(user_id);

CREATE TABLE node_overrides_scoped (
	user_id      INTEGER NOT NULL DEFAULT 0,
	node_key     TEXT NOT NULL,
	display_name TEXT NOT NULL DEFAULT '',
	region       TEXT NOT NULL DEFAULT '',
	updated_at   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	PRIMARY KEY (user_id, node_key)
);
INSERT INTO node_overrides_scoped (user_id, node_key, display_name, region, updated_at)
	SELECT user_id, node_key, display_name, region, updated_at FROM node_overrides;
DROP TABLE node_overrides;
ALTER TABLE node_overrides_scoped RENAME TO node_overrides;
CREATE INDEX IF NOT EXISTS idx_node_overrides_user_id ON node_overrides(user_id);
