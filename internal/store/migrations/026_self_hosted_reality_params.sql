-- 026: self_hosted_nodes 表新增 SNI + VLESS Reality 参数列(spec #70 一键转自建)。
-- 与 internal/store/store.go migrate() 中的 addColumnIfMissing 调用保持同步:
-- 本文件仅作 schema 参考,真正执行器是按列存在性幂等的 addColumnIfMissing
-- (同 021/022/024 先例:.sql 参考 + Go 幂等执行器)。
-- 旧数据兼容:既有行默认 ''(非 reality),由用户后续编辑或 from-pool 转换
-- 重新填充,无需 backfill。
ALTER TABLE self_hosted_nodes ADD COLUMN sni TEXT NOT NULL DEFAULT '';
ALTER TABLE self_hosted_nodes ADD COLUMN flow TEXT NOT NULL DEFAULT '';
ALTER TABLE self_hosted_nodes ADD COLUMN reality_public_key TEXT NOT NULL DEFAULT '';
ALTER TABLE self_hosted_nodes ADD COLUMN reality_short_id TEXT NOT NULL DEFAULT '';
ALTER TABLE self_hosted_nodes ADD COLUMN client_fingerprint TEXT NOT NULL DEFAULT '';
