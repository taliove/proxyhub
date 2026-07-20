-- 深度体检历史:每次深度体检完成追加一条报告 JSON,每节点保留最近 50 条(写入后修剪)。
-- report_json 只含稳定性/测速/解锁等聚合指标,按设计不含任何节点会话凭证。
CREATE TABLE IF NOT EXISTS exam_history (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    node_key    TEXT NOT NULL,
    report_json TEXT NOT NULL,
    created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_exam_history_node ON exam_history(node_key, id DESC);
