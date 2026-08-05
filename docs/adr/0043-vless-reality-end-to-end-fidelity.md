# ADR 0043: VLESS Reality 参数全链路保真

## 状态

accepted

## 日期

2026-08-05

## 上下文

机场订阅中大量节点是 VLESS + Reality(flow: xtls-rprx-vision、reality-opts、servername、client-fingerprint)类型。历史上分享链接解析只提取 uuid/server/port/type/security,Reality 参数当场丢弃,导致:

- Clash / v2ray 两种订阅输出、Xray 数据面、mihomo 检测面全都拿不到 reality 配置,节点被当成普通明文 VLESS,与真实服务器握手必然失败(issue #49);
- 实测某机场订阅 47 个节点中大量为此类,整个机场不可用。

修复横跨解析、节点模型、快照持久化、Clash 生成、v2ray 生成、Xray 数据面六个面(spec #58,tickets #59-#63),需要一份决策记录固定跨面口径。

## 决策

1. **reality 判定:`RealityPublicKey` 非空即 reality**,不引入单独的 security 枚举字段。解析层只负责不丢参数(security=reality 时 TLS 记 true,语义为存在 TLS 层),判定全部交给下游消费方。
2. **节点模型新增四个平铺字段**:`Flow`、`RealityPublicKey`、`RealityShortID`、`ClientFingerprint`(沿用 Node 平铺字段惯例,同 SNI/GrpcServiceName),随 nodes 快照表持久化(022,addColumnIfMissing 幂等补列)。
3. **v2ray 订阅输出按结构化字段重造完整链接**,不走 RawLink 回放:RawLink 仅服务 share-uri 二维码端点的原样回放,订阅下发需要标准化名 fragment,重造才能让名称标准化(ADR 0012)继续生效。
4. **空值约定三处(clash/v2ray/xray)对齐**:fp 缺省补 chrome;flow/sid/serverName(sni)空则省略对应键;clash 侧 reality 分支强制 `tls: true`(reality-opts 无 tls 不生效,防 pbk 非空但 TLS=false 的畸形节点静默退化明文)。
5. **接受一次性 node-key churn**:此前 vless 解析从不设置 SNI,存量行 key 为 server:port;新解析对所有带 sni=/servername= 的 vless 链接(tls 与 reality 皆然)产出 server:port:sni 新 key。升级后首次刷新旧 key 行标 stale(保留期后自动清理),检测状态由周期健康检查重建。快照表本就随刷新重建,相比迁移删行不会造成刷新前的订阅空窗(见 migrations/022 注释与升级路径测试)。

## 后果

- reality 节点导入后可过真实检测,订阅下发后 mihomo / v2rayN / Xray 客户端可直接连接。
- 检测面零代码改动:真实代理检测复用 ClashProxy(detection/mihomo.go),修复随生成器流入;由检测锁定测试保证 mihomo 实际接受生成的 reality 配置。
- 存量带 sni 的 vless 节点在升级后首次刷新经历一次 stale 更替,保留期内 UI 可见"已下架"旧行,7 天自动清理。
- 出界(spec #58 Out of Scope):Xray 数据面其余 transport(ws/grpc/非 reality TLS)、trojan/ss/vmess 的参数保真、Clash YAML 格式机场订阅解析(#48)。

## 相关决策

- ADR 0005(预览=/sub 同一渲染链):reality 修复在共享渲染函数内,预览所见即所得自动保持。
- ADR 0012(节点名称标准化):v2ray 重造链接 fragment 继续用标准化名。
- ADR 0009(屏蔽名单按 NodeKey):node-key churn 的 stale/清理语义复用其既有机制。
