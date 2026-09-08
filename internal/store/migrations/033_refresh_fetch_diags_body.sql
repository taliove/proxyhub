-- 033: refresh_fetch_diags 扩容(issue #143):body 字节数与超时标记。
-- 与 internal/store/store.go migrate() 中的 addColumnIfMissing 调用保持同步:
-- 本文件仅作 schema 参考,真正执行器是按列存在性幂等的 addColumnIfMissing
-- (同 021/022 先例:.sql 参考 + Go 幂等执行器)。
-- 旧数据兼容:既有行默认 0/false(字节数未知、非超时),随 refresh_runs
-- 滚动清理自然淘汰,无需 backfill。
ALTER TABLE refresh_fetch_diags ADD COLUMN body_bytes INTEGER NOT NULL DEFAULT 0;
ALTER TABLE refresh_fetch_diags ADD COLUMN timed_out INTEGER NOT NULL DEFAULT 0;
