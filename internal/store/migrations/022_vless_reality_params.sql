-- 022: nodes 快照表新增 VLESS Reality 参数列(spec #58, ticket 1)。
-- 与 internal/store/store.go migrate() 中的 addColumnIfMissing 调用保持同步:
-- 本文件仅作 schema 参考,真正执行器是按列存在性幂等的 addColumnIfMissing
-- (同 021 先例:.sql 参考 + Go 幂等执行器)。
-- 旧数据兼容:既有行默认 ''(非 reality),下一轮机场刷新按新解析重新填充,无需 backfill。
-- 一次性 key 变迁(显式决策,见同目录本文件引入时的提交说明):
-- 此前 vless 解析从不设置 SNI,存量 vless 行 node_key 为 server:port;
-- 新解析对所有带 sni=/servername= 的 vless 链接(security=tls 与 reality 皆然)
-- 产出 server:port:sni 的新 key。升级后首次刷新,这些节点的旧 key 行被标 stale
-- (保留期内展示为已下架,超期自动清理),检测状态由周期健康检查自动重建。
-- 接受此一次性 churn:快照表本就随刷新重建,且相比迁移删行不会造成刷新前的订阅空窗。
-- SNI 补齐本身是 spec #58 的显式实现决策("vless 解析补齐 SNI"),非附带损伤。
ALTER TABLE nodes ADD COLUMN flow TEXT NOT NULL DEFAULT '';
ALTER TABLE nodes ADD COLUMN reality_public_key TEXT NOT NULL DEFAULT '';
ALTER TABLE nodes ADD COLUMN reality_short_id TEXT NOT NULL DEFAULT '';
ALTER TABLE nodes ADD COLUMN client_fingerprint TEXT NOT NULL DEFAULT '';
