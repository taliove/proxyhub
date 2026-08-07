-- 023: self_hosted_nodes 身份唯一约束(issue #67):UNIQUE(user_id, server, port, protocol)。
-- 与 internal/store/selfnodes_identity_migration.go 保持同步:本文件仅作 schema 参考,
-- 真正执行器是 migrateSelfHostedIdentityUnique(先清存量重复再建索引,幂等)。
-- 存量重复清理口径:按身份保留 MIN(id)(最早一行),与"先来先得"直觉一致。
DELETE FROM self_hosted_nodes WHERE id NOT IN (
	SELECT MIN(id) FROM self_hosted_nodes GROUP BY user_id, server, port, protocol
);
CREATE UNIQUE INDEX idx_self_hosted_nodes_identity
	ON self_hosted_nodes(user_id, server, port, protocol);
