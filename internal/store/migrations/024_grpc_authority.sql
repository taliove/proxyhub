-- 024: nodes 快照表与 self_hosted_nodes 表新增 gRPC authority 列(spec #72, ticket 1)。
-- 与 internal/store/store.go migrate() 中的 addColumnIfMissing 调用保持同步:
-- 本文件仅作 schema 参考,真正执行器是按列存在性幂等的 addColumnIfMissing
-- (同 021/022 先例:.sql 参考 + Go 幂等执行器)。
-- 旧数据兼容:既有行默认 ''(非 grpc 或无 authority),下一轮机场刷新按新解析
-- 重新填充,无需 backfill;自建节点 authority 由用户后续编辑填充。
ALTER TABLE nodes ADD COLUMN grpc_authority TEXT NOT NULL DEFAULT '';
ALTER TABLE self_hosted_nodes ADD COLUMN grpc_authority TEXT NOT NULL DEFAULT '';
