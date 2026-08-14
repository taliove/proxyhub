-- 032: name_slots 身份迁移(issue #112,tracking #94)——槽位身份从名字翻转为内部自增 ID。
--
-- 目标 schema(新库由 store.go 内嵌 schema 直接建出;既有库由 Go 迁移
-- migrateNameSlotsIdentity 表重建,幂等、保留存量行 created_at):
--
--   - id 自增主键:槽位身份与名字解耦,API 按 ID 寻址;
--     改名/重名放行(#113)不再触碰身份。
--   - idx_name_slots_name_literal:字面名(不含 {index} 模板)的 (user_id, name)
--     部分唯一索引——DB 层为「含 {index} 模板重名放行」(#113)先行就位;
--     本票应用层查重逻辑不变(行为零变化)。
--   - idx_name_slots_node:(user_id, node_key) 非空部分唯一索引保持不变
--     (双向唯一:一个节点在同一用户下只占一个槽位)。

CREATE TABLE IF NOT EXISTS name_slots (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER NOT NULL DEFAULT 0,
    name       TEXT NOT NULL,
    node_key   TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_name_slots_name_literal
    ON name_slots(user_id, name) WHERE name NOT LIKE '%{index}%';
CREATE UNIQUE INDEX IF NOT EXISTS idx_name_slots_node
    ON name_slots(user_id, node_key) WHERE node_key != '';
