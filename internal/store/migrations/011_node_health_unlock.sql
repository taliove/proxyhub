-- 011: node_health 解锁级别与命中区域列
-- 专用解锁判定(netflix 等)在 detection.Result 上填充 Level/Region;
-- 本迁移给 node_health 补两列以持久化,供 /nodes 视图透出。
-- generic 结果两列为空串(NOT NULL DEFAULT ''),序列化时按 omitempty 省略。
--
-- 接入点:internal/store/store.go 的 migrate() 用 addColumnIfMissing 施加本迁移
-- (ADD COLUMN 的幂等标记是列存在性,而非表存在性;node_health 恒存在,
--  若走 applyMigrationFile 的表存在性标记会被误判为已应用 -> 死迁移)。
ALTER TABLE node_health ADD COLUMN level TEXT NOT NULL DEFAULT '';
ALTER TABLE node_health ADD COLUMN region TEXT NOT NULL DEFAULT '';
