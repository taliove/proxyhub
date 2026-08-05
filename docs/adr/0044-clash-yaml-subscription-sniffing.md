# ADR 0044: Clash YAML 机场订阅的内容嗅探解析

## 状态

accepted

## 日期

2026-08-05

## 上下文

很多机场的订阅转换地址通过参数强制输出 Clash YAML(如 `?clash=1`),以 `application/octet-stream` + 文件下载形式返回。历史上机场订阅只支持按行解析分享链接(允许整体 base64),这类地址添加时全部行解析失败,报"没有有效节点"(issue #48)。实测该类 YAML 中大量节点为 VLESS+Reality,ADR 0043 修复的参数保真链条对它们同样适用——缺的是入口格式这一环。

## 决策

1. **内容嗅探,与传输特征无关**(决策已与用户确认):不依赖 query 参数、Content-Type、文件名——实测机场对这些信号的返回五花八门。嗅探点固定在共享解析边界(`DecodeSubscription` 之后、`ParseWithStats` 行解析之前,单点接入),URL 拉取、机场测试诊断、手动机场粘贴导入三入口自动同时受益。
2. **命中规则**:内容按 YAML 解析为顶层 map 且 `proxies` 键为非空列表。base64 链接列表在此探测下是 YAML 标量(非 map),天然落空;含冒号元数据行、YAML 列表形态、注释中的 proxies 字样等误判面由专门测试钉死。
3. **只映射节点模型已有字段**(决策已与用户确认):五协议(vmess/vless/trojan/ss/anytls)的凭据与传输字段,含 vless reality 全参数(servername/sni→SNI、flow、reality-opts、client-fingerprint,与 ADR 0043 判定口径一致);ss obfs 系插件映射为 SIP002 形态,与行解析产出对齐。ws-opts 的 path/host 等模型无字段的传输参数出界(与 v2ray 路径现状一致的已知缺口)。
4. **容错语义与行解析一致**:未知 type 跳过并计入解析失败;全部失败由调用方报"no valid nodes found";元数据伪节点过滤与 NodeKey 去重复用既有管道,不发明第二套。
5. **失败行号守字段契约**:`LineFailure.Line` 恒为原文 1 起始行号(手动粘贴入口前端按编辑器行号展示)——YAML 模式用 `yaml.Node` 解码取真实源行号,不用 proxies 数组下标。
6. **YAML 来源节点 RawLink 为空**:没有原始分享链接可回放,share-uri 端点走既有回退重建(VLESS Reality 自 ADR 0043 起重建保真)。

## 后果

- `?clash=1` 类订阅地址、Clash YAML 全文粘贴均可导入;与 ADR 0043 叠加后,reality 节点从导入到下发的全链路贯通。
- 拉取 UA 维持 v2rayN 系不变:链接列表形态信息损耗最小,YAML 支持是兜底而非首选。
- hysteria2 等模型未支持协议在 YAML 路径同样跳过计数(不报错),接入新协议时两处(行解析 + YAML 映射)需同步。
- 手动机场与拉取机场共用边界,手动机场同等受益(CONTEXT.md 手动机场定义不变)。

## 相关决策

- ADR 0043(VLESS Reality 参数全链路保真):本 ADR 的 vless 映射直接复用其字段与判定口径,两者叠加构成完整链路。
- ADR 0034(手动机场来源类型):手动粘贴入口的 YAML 支持经由共享边界自动获得。
