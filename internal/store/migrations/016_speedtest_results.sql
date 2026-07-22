-- 本机实测历史:浏览器端测速完成追加一条,node_key 为 NULL 表示直连/未标注。
-- 每 key(含直连桶)保留最近 50 条(写入后修剪),孤儿历史(节点已不在池)保留不级联删。
-- 只含链路指标与客户端自报信息,不含任何节点会话凭证。
CREATE TABLE IF NOT EXISTS speedtest_results (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    node_key        TEXT,
    down_mbps       REAL NOT NULL DEFAULT 0,
    up_mbps         REAL NOT NULL DEFAULT 0,
    idle_latency_ms REAL NOT NULL DEFAULT 0,
    jitter_ms       REAL NOT NULL DEFAULT 0,
    client_info     TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_speedtest_results_node ON speedtest_results(node_key, id DESC);
