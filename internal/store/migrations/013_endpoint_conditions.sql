-- 013: 订阅地址的节点范围筛选条件(动态查询)。
-- conditions 存结构化筛选谓词的 JSON(机场/地区/标签/关键词,见 internal/subfilter.Conditions);
-- 空串=不筛选=全量(现状行为,零回归)。拉取订阅时在内存池上按条件求值,不落静态清单,
-- 机场刷新后自动跟随(见 design-subscription-conditions.md)。
--
-- 接入点:internal/store/store.go 的 migrate() 用 addColumnIfMissing 施加本迁移
-- (ADD COLUMN 的幂等标记是列存在性,而非表存在性;endpoints 恒存在,
--  若走 applyMigrationFile 的表存在性标记会被误判为已应用 -> 死迁移。同 011 先例)。
ALTER TABLE endpoints ADD COLUMN conditions TEXT NOT NULL DEFAULT '';
