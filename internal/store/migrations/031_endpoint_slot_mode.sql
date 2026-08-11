-- 031: endpoints 表新增 slot_mode 列——槽位模式订阅(ADR 0047 / issue #94 后续)。
-- 0(默认)= 池模式:精选/节点范围/关键词等过滤链照旧(零回归);
-- 1 = 槽位模式:只下发当前有槽位挂载的节点,名字即槽位名,顺序按槽位名固定;
-- 精选/节点范围/关键词筛选在该模式下不生效。
-- 与 internal/store/store.go migrate() 中的 addColumnIfMissing 保持同步:
-- 本文件仅作 schema 参考,真正执行器是按列存在性幂等的 addColumnIfMissing
-- (同 013/022/030 先例)。
ALTER TABLE endpoints ADD COLUMN slot_mode INTEGER NOT NULL DEFAULT 0;
