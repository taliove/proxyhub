-- 008: Traffic distribution support
-- Adds tables for Xray-core proxy relay configuration and statistics

-- Distribution global configuration (single row)
CREATE TABLE IF NOT EXISTS distribution_config (
    id             INTEGER PRIMARY KEY CHECK (id = 1),
    enabled        INTEGER NOT NULL DEFAULT 0,
    listen_port    INTEGER NOT NULL DEFAULT 10808,
    domain         TEXT NOT NULL DEFAULT '',
    protocol       TEXT NOT NULL DEFAULT 'vless',
    network        TEXT NOT NULL DEFAULT 'tcp',
    uuid           TEXT NOT NULL DEFAULT '',
    tls            INTEGER NOT NULL DEFAULT 0,
    cert_path      TEXT NOT NULL DEFAULT '',
    key_path       TEXT NOT NULL DEFAULT '',
    updated_at     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Initialize with default config
INSERT OR IGNORE INTO distribution_config (id, enabled) VALUES (1, 0);

-- Distribution paths (traffic routing rules)
CREATE TABLE IF NOT EXISTS distribution_paths (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    name               TEXT NOT NULL,
    path               TEXT NOT NULL UNIQUE,
    upstream_node_keys TEXT NOT NULL DEFAULT '[]',
    lb_strategy        TEXT NOT NULL DEFAULT 'random',
    total_upload       INTEGER NOT NULL DEFAULT 0,
    total_download     INTEGER NOT NULL DEFAULT 0,
    total_connections  INTEGER NOT NULL DEFAULT 0,
    last_access        TIMESTAMP,
    enabled            INTEGER NOT NULL DEFAULT 1,
    created_at         TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_distribution_paths_path ON distribution_paths(path);
CREATE INDEX IF NOT EXISTS idx_distribution_paths_enabled ON distribution_paths(enabled);

-- Distribution statistics (time-series traffic data)
CREATE TABLE IF NOT EXISTS distribution_stats (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    path_id     INTEGER NOT NULL REFERENCES distribution_paths(id) ON DELETE CASCADE,
    timestamp   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    upload      INTEGER NOT NULL DEFAULT 0,
    download    INTEGER NOT NULL DEFAULT 0,
    connections INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_distribution_stats_path ON distribution_stats(path_id, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_distribution_stats_timestamp ON distribution_stats(timestamp DESC);
