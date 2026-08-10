-- 030: endpoints 表新增 status_node_enabled 列——虚拟状态节点开关
-- (ADR 0047 / issue #102,tracking #94)。开启时该订阅地址输出第一位注入
-- 合成哑节点,名称动态展示监控摘要(在线数/故障名单/更新时间)。
-- 默认 0 = 关(零回归)。
-- 与 internal/store/store.go migrate() 中的 addColumnIfMissing 保持同步:
-- 本文件仅作 schema 参考,真正执行器是按列存在性幂等的 addColumnIfMissing
-- (同 013/022/024/025/027 先例)。
ALTER TABLE endpoints ADD COLUMN status_node_enabled INTEGER NOT NULL DEFAULT 0;
