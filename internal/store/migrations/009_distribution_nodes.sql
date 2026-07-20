-- 009: Distribution nodes table
-- Adds dedicated table for distribution nodes that appear alongside airport and self-hosted nodes

CREATE TABLE IF NOT EXISTS distribution_nodes (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    name               TEXT NOT NULL,
    region             TEXT NOT NULL DEFAULT '',
    distribution_path  TEXT NOT NULL UNIQUE,
    upstream_node_keys TEXT NOT NULL DEFAULT '[]',
    lb_strategy        TEXT NOT NULL DEFAULT 'random',
    enabled            INTEGER NOT NULL DEFAULT 1,
    created_at         TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at         TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_distribution_nodes_path ON distribution_nodes(distribution_path);
CREATE INDEX IF NOT EXISTS idx_distribution_nodes_enabled ON distribution_nodes(enabled);
