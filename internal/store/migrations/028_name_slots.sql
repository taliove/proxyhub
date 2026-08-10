-- 028: name_slots 表——节点名称槽位(ADR 0047 / issue #95,tracking #94)。
-- 命名所有权翻转:名字是用户资产,(user_id, name) 主键,指向当前占用它的
-- node_key;空 node_key = 预建空槽(先起名后挑节点)。双向唯一:一个节点在
-- 同一用户下只能占一个槽位(部分唯一索引,空键豁免)。
-- 与 internal/store/store.go migrate() 中的 CREATE TABLE IF NOT EXISTS 保持同步:
-- 新表走主 schema 建表路径,新库旧库同源,本文件仅作 schema 参考。
-- 存量 node_overrides.display_name 的回填由 Go 侧 migrateOverridesToNameSlots
-- 幂等执行(同名冲突 updated_at 新占旧让,旧行保留进人工待处理)。
CREATE TABLE IF NOT EXISTS name_slots (
    user_id    INTEGER NOT NULL DEFAULT 0,
    name       TEXT NOT NULL,
    node_key   TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, name)
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_name_slots_node
    ON name_slots(user_id, node_key) WHERE node_key != '';
