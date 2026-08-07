-- 027: node_overrides 表新增 favorite 列——节点收藏(issue #83,tracking #54)。
-- favorite=1 表示该用户收藏了此节点;维度与覆盖层一致:(user_id, node_key)。
-- 与 internal/store/store.go migrate() 中的 addColumnIfMissing 调用保持同步:
-- 本文件仅作 schema 参考,真正执行器是按列存在性幂等的 addColumnIfMissing
-- (同 013/022/024/025/026 先例:.sql 参考 + Go 幂等执行器;node_overrides 恒存在,
--  若走 applyMigrationFile 的表存在性标记会被误判为已应用 -> 死迁移)。
-- 语义:收藏是纯展示层标记(列表星标 + 筛选),不参与订阅过滤链,与 #55 精选
-- (下发层)互不影响。旧数据无需 backfill(默认 0=未收藏)。
ALTER TABLE node_overrides ADD COLUMN favorite INTEGER NOT NULL DEFAULT 0;
