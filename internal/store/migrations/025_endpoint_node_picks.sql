-- 025: endpoints 表新增 node_picks 列——订阅地址级精选(spec #70 / issue #79)。
-- node_picks 存精选节点集的 JSON(NodeKey 字符串数组);空串=未配置=全量(零回归)。
-- 与 internal/store/store.go migrate() 中的 addColumnIfMissing 调用保持同步:
-- 本文件仅作 schema 参考,真正执行器是按列存在性幂等的 addColumnIfMissing
-- (同 013/022/024 先例:.sql 参考 + Go 幂等执行器;endpoints 恒存在,若走
--  applyMigrationFile 的表存在性标记会被误判为已应用 -> 死迁移)。
-- 语义:非空精选在订阅过滤链(filteredNodes)最前做候选集替换(池∩精选先行),
-- 再流经既有关键词/屏蔽/stale/可用性过滤;NodeKey 记忆——改名仍命中,
-- 下架自然失效、复活自动恢复。旧数据无需 backfill(空串即未配置)。
ALTER TABLE endpoints ADD COLUMN node_picks TEXT NOT NULL DEFAULT '';
