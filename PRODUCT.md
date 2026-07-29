# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

主要用户是**小团体运营者**:自托管 ProxyHub,给家人/朋友/小团体开普通用户账号并分配配额的人。他一人承担全部运维职责——接入机场、维护节点池、分发订阅地址、处理告警与排障。普通用户(被托管的账号)在自己的用户空间内管理机场、节点池与订阅地址;终端设备只认订阅地址,无账号体系。

## Product Purpose

把多个机场订阅聚合成一个统一订阅地址:定时拉取上游节点,经健康检查、去重、地区与关键词筛选后形成节点池,向每台设备暴露一个订阅链接。客户端只认这一个链接,节点优劣由系统替运营者操心。成功意味着:运营者不需要逐个客户端手工维护节点,订阅地址长期稳定,节点问题在影响用户前被系统发现并可被运营者看见。

## Positioning

相邻产品(订阅转换器、单机场面板)无法照抄的机制是:**健康检查驱动的节点池 + 一个稳定不变的聚合订阅地址**。节点池持续被延迟测速、真实请求、体检流水线评估,订阅生成只是池的投影;配合自建节点兜底、多用户空间隔离、管理面纵深防御(强制 MFA、IP2Ban、登录验证码、拉取防护三道守卫),以及"打开黑盒"的运营可见性(机场测试评分维度拆解、任务中心、拉取统计、审计流水)。

## Operating Context

运行在个人 VPS 上,一键安装器配好 systemd + Caddy HTTPS,`proxyhubctl` 承担日常运维(status/logs/backup/update/rotate-path)。客户端是 Clash / V2Ray(mihomo 内核内嵌)。告警走飞书 Webhook。界面语言为中文。

未来设计工作覆盖三类界面(已确认的 surface 边界):

1. **管理后台 SPA**(`web/`)——现有唯一已实现的界面,运营者与普通用户的主战场;
2. **落地页/官网**——尚不存在,目前由 README 承担对外介绍职能;
3. **CLI/终端体验**——安装脚本输出与 `proxyhubctl` 的品牌呈现。

## Capabilities and Constraints

- 单二进制部署:内嵌前端、SQLite 与 mihomo 内核,零外部依赖;后端 Go 1.22+,前端 Vue 3 + Element Plus(底座不换,设计层在 CSS 自定义属性上自建,见 ADR 0014 与 docs/design-frontend.md)。
- 领域术语严格定义在 CONTEXT.md(机场/订阅地址/节点/聚合/刷新/体检/拉取防护等),任何界面文案与文档必须遵守。
- 安全姿态是产品事实而非选配:仅监听环回、Caddy 强制 HTTPS、管理后台随机路径、登录失败封禁、强制 MFA、备份加密;`/sub` 公开入口有黑名单/地域白名单/限频三道守卫。
- 支持 VMess / VLESS / Trojan / Shadowsocks 协议;代理协议族与订阅格式(Clash / V2Ray)由客户端生态约束。
- 待定:落地页/官网的内容策略与托管方式尚未决策;CLI 品牌呈现目前只有图标与版本输出,未形成规范。

## Brand Commitments

- 名称 **ProxyHub**;图标 `web/public/proxyhub-icon.png`;品牌组件 `BrandMark.vue` / `Wordmark.vue`。
- 界面与文档语言为中文(commit message 除外)。
- 视觉方向:**基调保留、细节可调**——docs/design-frontend.md 确立的"电波青 + 现代极简工具风"(灰阶为主、高密度、描边分层、暗色一等公民、无多余装饰)继续作为基础框架,色彩/密度等局部细节允许演进,不锁死。

## Evidence on Hand

- README、docs/(DEPLOY/SECURITY/FAQ/DEVELOPMENT)、CONTEXT.md 术语表、docs/design-frontend.md 设计规范、docs/adr/ 决策记录。
- 品牌图标 `web/public/proxyhub-icon.png`。
- 没有客户评价、案例、第三方基准数据——未来任何对外材料不得虚构这些。

## Product Principles

1. **一个链接喂饱所有设备**——复杂度收进系统,运营者与终端用户的接触面都只有一个稳定订阅地址。
2. **打开黑盒,不让人猜**——评分拆解维度、任务有进度可取消、防护拦截留痕;任何自动化结论都能被运营者追问"为什么"。
3. **运营者效率优先**——高密度、键盘可达、批量操作与单点操作共用词汇;这是工具,不是展示品。
4. **安全是默认值**——防御纵深内建于安装与默认配置,不依赖运营者记得去开。
