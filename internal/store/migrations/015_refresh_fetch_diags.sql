-- 常规刷新的每机场结构化拉取诊断(ticket 0018)。
-- 全量刷新与单机场刷新对每个机场写一行:HTTP 状态、拉取耗时、
-- 解析成功节点数、解析失败行数;随 refresh_runs 滚动清理。
CREATE TABLE IF NOT EXISTS refresh_fetch_diags (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id         INTEGER NOT NULL REFERENCES refresh_runs(id),
    airport        TEXT NOT NULL,
    airport_id     INTEGER NOT NULL DEFAULT 0,
    http_status    INTEGER NOT NULL DEFAULT 0,
    duration_ms    INTEGER NOT NULL DEFAULT 0,
    node_count     INTEGER NOT NULL DEFAULT 0,
    parse_failures INTEGER NOT NULL DEFAULT 0,
    error          TEXT NOT NULL DEFAULT '',
    created_at     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_refresh_fetch_diags_run ON refresh_fetch_diags(run_id);
