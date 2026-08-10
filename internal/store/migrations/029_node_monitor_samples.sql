-- 029: node_monitor_samples 表——订阅节点监控探测打点(ADR 0047 / issue #99)。
-- 每次 TCP 探测一行:连通性 + 握手耗时,供告警状态机(issue #100)与前端
-- 节点详情/趋势(issue #103)消费。按 node_key 物理维度(跨用户去重后只探
-- 一次),保留 7 天,由 nodemon 每轮 prune。
-- 与 internal/store/store.go migrate() 中的 CREATE TABLE IF NOT EXISTS 保持同步:
-- 新表走主 schema 建表路径,本文件仅作 schema 参考。
CREATE TABLE IF NOT EXISTS node_monitor_samples (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    node_key   TEXT NOT NULL,
    ok         INTEGER NOT NULL,
    latency_ms INTEGER NOT NULL DEFAULT 0,
    checked_at TIMESTAMP NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_node_monitor_samples_key_time
    ON node_monitor_samples(node_key, checked_at);
