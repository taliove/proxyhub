# 机场模型 - 设计

## 概述

机场是上游订阅提供方(CONTEXT.md),ProxyHub 从其订阅 URL 拉取节点聚合成池。本文描述机场的领域模型:两种来源类型、各自的刷新/测试/清空语义,以及用量信息模型。术语以 CONTEXT.md 为准;来源类型与 url 空串的取舍见 [ADR 0034](adr/0034-manual-airport-source-type.md)。

## 来源类型

`airports.source_type` 二值:`url`(拉取型,默认,历史行迁移后落此值)与 `manual`(手动机场)。

| 维度 | 拉取型 (url) | 手动机场 (manual) |
| --- | --- | --- |
| 节点来源 | 订阅 URL 拉取 | 用户粘贴机场面板导出的订阅内容 |
| `url` 列 | 真实订阅 URL | 空串(无 URL 概念) |
| 创建 | 填名称 + URL | 填名称,随后粘贴导入入池 |
| 更新节点 | 单机场刷新 / 全量刷新 | 显式重贴(粘贴即一次单机场 upsert) |
| 定时/全量刷新 | 参与 | 跳过(无 URL 可拉) |
| 健康检查 | 全量刷新时随拉取集检查 | 不参与周期健康检查;可用性靠检测状态保留 + 手动检测(检查动作/机场测试) |
| 机场节点清空 | 清除(下轮刷新冲回) | 豁免(无 URL 可拉,清空后永不回来) |
| 机场测试 | 全语义 | URL 拉取诊断段 N/A;评分走"URL 不可达且池有节点"权重重归一(现成语义) |

手动机场节点是机场节点(不是自建节点):走完整的地区识别、名称标准化与三道过滤链;自建节点的 FailBack 豁免语义不适用于它。

## 粘贴导入

端点 `POST /api/airports/{id}/import`(仅 manual 机场,其他 400):

1. **凭证红线**:粘贴内容含节点凭证,故走同步 HTTP 而非 job kind(params_json 会落库回显);内容不落库、不进日志、不进 jobs params,系统不保存粘贴原文。
2. 体积上限 1MiB(约 200 条分享链接 ~80KB,余量充足),超限 413。
3. 内容识别:`subscription.DecodeSubscription` 先试整体 base64(机场面板标准导出),失败按明文多行处理;该 helper 与 URL 拉取(fetcher)、机场测试诊断(airporttest)三处共用,不再有抄写副本。
4. 逐行解析(`ParseWithStats`),部分行失败不阻断:成功行入池,失败行逐行报告(行号 + 原因,明细上限 200 条,计数始终精确)。行内重复链接后条覆盖前条(`DedupeByNodeKey`,同 NodeKey upsert 语义)。
5. 入池语义同单机场刷新:`poolops.UpsertAirportNodes`(该机场旧节点 carry-forward,其他机场不动,不跑健康检查);互斥域与刷新/机场测试同一 `refreshStartMu` 临界区,冲突 409。

## per-source 合并(MergePool stale 陷阱)

全量刷新成功路径的池合并是 per-source 的:stale 扫描集 = 成功拉取来源 ∪ 旧池中"不再现存启用"的来源,保留集 = 本轮未成功拉取但仍现存启用的机场(手动跳过/拉取失败/取消未启动)。两条边界缺一不可:

- 只保成功集会把手动机场节点整批标 stale(它们永不在拉取集内);
- 无差别保留会让被禁用/被删除/被改名(旧名)的机场节点永久 active、持续下发订阅——这些属合法消失,照旧进 MergePool stale 扫描下架。

同一手法复用于取消路径(`mergePartialOnCancel`);自建节点在成功路径随注入走 MergePool、取消路径走保留侧。合并尾部按 NodeKey 去重:保留侧与新拉取集同 key 的旧节点丢弃(新拉取集优先),防跨来源同 server:port 双份。实现:`aggregator.mergePerSource`。

## 用量信息

机场的流量与有效期元数据(CONTEXT.md「用量信息」),只展示不联动(不发告警、不自动停用):

- 存储(`airports` 表):`usage_upload` / `usage_download` / `usage_total`(字节)、`usage_expire`(unix 秒)、`web_page_url`;全部零值 = 未知不展示。
- 拉取型:每次订阅拉取从响应头捕获(`subscription-userinfo` 的 upload/download/total/expire;`profile-web-page-url` 的官网),覆盖更新;响应头缺失时保留既有值(尤其官网,避免抹掉手填)。
- 手动型:粘贴导入/编辑时手填(剩余/总流量、过期日期、官网,全部可选);用户填"剩余",存储模型是 upload/download/total——已用 = 总量 - 剩余(钳制 ≥0)计 download,上行未知计 0;空值 = 显式清空(与拉取路径的保留语义相反)。
- 展示:机场列表(剩余百分比 + 进度条 + 到期日)与详情抽屉(完整数字 + 官网链接);临期(<7 天)/已过期/流量将尽(剩余 <10%)标红。

## 机场测试适配

手动机场的机场测试走现成"URL 不可达且池有节点"语义:任务执行时按 `source_type` 跳过 URL 拉取,诊断段显式标 `manual_source`(N/A,区别于"拉取失败"的 HTTPStatus=0);池有节点照常抽样检活,评分权重按 5:3:1 重归一;池空则 failed,文案引导重新粘贴导入。
