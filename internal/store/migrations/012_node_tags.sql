-- 012: 节点自动标签表(node_key x tag)。
-- 标签从测试结果派生(见 internal/nodetag.Derive):解锁类/出网类/质量类。
-- 重算=事务内全量替换该节点标签(幂等);节点下线/删除时清理。
--
-- 接入点:internal/store/store.go 的 migrate() 用 applyMigrationFile 施加本迁移
-- (表存在性作已应用标记,node_tags 为新表,与 010/009/008 同法)。
CREATE TABLE IF NOT EXISTS node_tags (
    node_key TEXT NOT NULL,
    tag      TEXT NOT NULL,
    PRIMARY KEY (node_key, tag)
);

-- 按标签反查(票据 20/23 的谓词筛选:找出带某标签的所有节点)。
CREATE INDEX IF NOT EXISTS idx_node_tags_tag ON node_tags(tag);
