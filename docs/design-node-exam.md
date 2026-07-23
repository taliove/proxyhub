# 节点深度体检 - 设计

## 概述

深度体检对**单个节点**做一次纵向深挖,产出一份四段报告:出网信息、稳定性、多地域测速、流媒体/AI 解锁。为什么解锁归批量而稳定性/多地域归单节点,见 [ADR 0015](adr/0015-node-exam-capability-boundary.md);为什么各段串行、稳定性独占,见 [ADR 0017](adr/0017-exam-orchestration-serial-stability-exclusive.md);为什么出网前置为第一段、全失败短路,见 [ADR 0020](adr/0020-exam-egress-first-and-shortcircuit.md);解锁判定的 kind 注册表与保守判定,见 [ADR 0016](adr/0016-unlock-detection-kind-registry.md);四项加权总分与渐进/归一双模式,见 [ADR 0022](adr/0022-exam-weighted-score-dual-mode.md)。本文描述"是什么",随代码演进。

## 检查动作分层

节点页把面向用户的检查能力收敛为**四个同名同义的手动动作**,批量(作用于勾选集)与单节点(行内下拉 / 详情抽屉)共用同一套词汇与启动器(单节点即 `node_keys` 只含本节点),深度体检是其中最重的一档。四个动作按"由轻到重"分层,复用体检报告模型的不同子集:

| 动作 | 覆盖段 | 落库来源(`ExamReport.Source`) | 运行时 | 前端入口 |
| --- | --- | --- | --- | --- |
| 1 出网快速检测 | 出网连通性 + 延迟 + 真实请求 + 配置的解锁目标 | 走 `batch_detection`(非体检),写解锁结果 + 重算标签 | 全局单例(`detecting`),不落 `exam_history` | 批量栏 / 行内下拉 / 抽屉按钮 |
| 2 出网+稳定性 | 出网 + 稳定性(两段) | `stability_check`,落 `exam_history`(不抢"最近体检"单一事实源) | `batch_stability` kind / 单节点 SSE 流 | 批量栏 + `NodeStabilityDialog` |
| 3 快速测速 | 基准下行 + 上行(基准行口径) | 不落体检(带宽字段随节点快照) | `test/stream?mode=speedtest` / `batch_speedtest` | 批量栏 + `BandwidthTestDialog`(mode=speedtest) |
| 4 深度体检 | 完整四段(出网→稳定性→多地域→解锁) | 空 Source = 完整体检口径,落 `exam_history` | `batch_exam` kind / 单节点 SSE 流 | 批量栏 + `NodeExamDialog` |

要点:动作 2 只是深度体检的前两段用独立入口暴露(`stability_check` 来源标记区分),动作 4 才是完整流水线。动作 1 走的是可用性检测链路(`batch_detection`)而非体检编排,故不产 `ExamReport`、不落 `exam_history`。另有**本机实测**(浏览器端验收测量,见 [design-client-speedtest](design-client-speedtest.md))作为独立页入口,与上述四个服务端检测动作正交,不属于体检分层。

## 报告模型

一次体检产出一个 `ExamReport`(`internal/detection/exam.go`),四段各一个可空指针(某段失败/缺失则为空):

```go
type ExamReport struct {
    Stability   *StabilityMetrics   `json:"stability,omitempty"`
    RegionSpeed *RegionSpeedMetrics `json:"region_speed,omitempty"`
    Unlock      *UnlockMetrics      `json:"unlock,omitempty"`
    Egress      *EgressMetrics      `json:"egress,omitempty"`
}
```

### 出网信息段(EgressMetrics)

体检第一段(前置以便用户 ~5s 内见出口画像),经节点并行探测三类小请求(`internal/detection/egress.go`、`exam_egress.go`):

| 字段 | 含义 |
|---|---|
| `IPv4` | 出口 IPv4:IP、国家码(`country_code`)、`hosting`(机房 vs 住宅)、error |
| `IPv6` | 出口 IPv6:`available` + IP;不可达(available=false, error 空)是明确负判定,非失败 |
| `DNS` | 出口 DNS 解析器 IP 与归属地;`leak`=解析器国家 != 出口国家的疑似泄露 |

**出网全失败短路**:IPv4/IPv6/DNS 三项皆 error = 节点根本出不去,编排跳过后续段、推 error 终态、不落历史(判定与边界见 [ADR 0020](adr/0020-exam-egress-first-and-shortcircuit.md))。出口国家码是节点地区的**地面真相**,体检收口时回写节点 region(见 [ADR 0021](adr/0021-node-region-truth-chain-egress-first.md))。

### 稳定性段(StabilityMetrics)

采样器 30s(可配)× 1Hz HEAD/GET `https://www.gstatic.com/generate_204`,得到延迟序列,聚合出:

| 字段 | 含义 |
|---|---|
| `loss_rate` | 丢包率 0..1(超时=丢) |
| `mean/median/p95/p99_ms` | 成功样本延迟统计 |
| `jitter_ms` | 相邻成功样本延迟差的平均绝对值 |
| `score` | 0..100 稳定性评分 |

**评分公式(实现事实)**:`score = 100 * (0.5*丢包分 + 0.25*抖动分 + 0.25*P95分)`,各分量归一到 0..1(`1 - loss`、`1 - jitter/100ms`、`1 - p95/latencyNorm`,后两者钳位)。即**丢包权重最大**,抖动与 P95 延迟各占其半。

> 注:早期 spec 设想用 "P99/中位长尾比" 作延迟分量,落地时改用 P95 直接归一(更简单、边界更稳)。以实现为准。

原始采样序列(`StabilitySample[]`)只在**实时 SSE** 阶段逐点推送给前端画 sparkline,**不写入 `ExamReport`**。故历史报告只有聚合指标,没有逐点序列 —— 历史报告卡不重画 sparkline。

### 多地域测速段(RegionSpeedMetrics)

实际测量序列是 **基准对照行 + 8 区**(`internal/detection/exam_regionspeed.go`):

- **基准行**(`code=baseline`,第一行):经节点打 Cloudflare 就近 POP(`speed.cloudflare.com/__down`),作为该节点带宽上限的对照参考。总分的速度分量、`fast` 自动标签都以基准行下行为准。
- **8 区探测点**:美西 / 美东 / 法兰克福 / 新加坡 / 东京 / 悉尼 / 多伦多 / 孟买。

每区测 TTFB 延迟 + 下行速率 + 上行速率(`down_mbps`/`up_mbps`;下行失败则不测上行,下行成功但上行失败则 `error` 标记上行问题)。每区一行 `RegionResult{code, name, ttfb_ms, down_mbps, up_mbps, error}`;单区失败只置 `error`,不拖垮整段。DC 域名做成常量表便于随 Linode 实况调整。

### 解锁段(UnlockMetrics)

复用批量检测的 `Result`,逐目标一条,顺序同 `DefaultUnlockTargets`。关键字段:

- `level`:流媒体三档 `full` / `originals_only` / `blocked`;AI/通用目标不填。
- `region`:出口国家码(如 `JP`),尽力解析,解析不到留空。

字段语义与保守取值规则见 [ADR 0016](adr/0016-unlock-detection-kind-registry.md)。

## 关键流程

### 实时体检(SSE)

`GET /api/nodes/exam/stream?node_key=...`(或 `self_node_id=...`)建一个节点会话,按 [ADR 0017](adr/0017-exam-orchestration-serial-stability-exclusive.md) / [ADR 0020](adr/0020-exam-egress-first-and-shortcircuit.md) 的串行顺序(出网 -> 稳定性 -> 多地域 -> 解锁)跑,分段推事件:

```
egress(逐类) -> section_done(egress)
  [出网全失败 -> error 终态 + done,短路后续段]
  -> sample(逐采样点) -> section_done(metrics)
  -> region(逐区) -> section_done(region_speed)
  -> unlock_result(逐目标) -> section_done(unlock) -> done
```

**成功完成**才落一条历史;中途失败/取消/出网短路**不落盘**。体检不再绑死在 SSE 请求上,而是长在通用异步任务运行时(见 [ADR 0019](adr/0019-generic-async-jobs-runtime.md))上:后台任务化、多客户端可附加同一场体检、进程重启有恢复语义。落历史/重算标签/回写地区在任务 `OnComplete` 收口(`onExamComplete`),消除取消到达时刻的竞态。

### 批量体检

批量体检(`batch_exam` kind,可续跑)对一组节点逐个跑体检,串行/低并发,游标续跑,每节点完成即落历史 + 触发标签重算。同样长在 [ADR 0019](adr/0019-generic-async-jobs-runtime.md) 的运行时上。启动请求带 `mode` 参数:`simplified`(默认,兼容老客户端;出网 + 稳定性 + 基准下行,跳过 8 区与解锁)或 `full`(完整四段)。空值按 `simplified`,未知值 400 拒绝、不静默降级。节点页「深度体检」批量动作固定发 `mode=full`(收敛后批量入口只暴露完整口径),精简档保留给内部/兼容调用。

### 历史存储与查询

- 表 `exam_history(id, node_key, report_json, created_at)`(迁移 `010_exam_history.sql`)。
- 写入后**修剪该节点至最近 50 条**(`SaveExamHistory`,`id` 倒序保留 50,其余删)。
- 查询:`GET /api/nodes/exam/latest`(最近一次,无历史返回 JSON `null`)、`GET /api/nodes/exam/history`(时间倒序全量,无历史返回 `[]`)。两者缺 `node_key`/`self_node_id` 返回 400。

## 体检总分

总分是从 `ExamReport` 派生的**纯函数**(`web/src/components/exam/score.ts`,`calculateExamScore`),四项加权:稳定性 40% + 速度 25%(基准行下行对数映射)+ 解锁 20% + 出网质量 15%,映射到五档(极好/良好/一般/较差/很差)。缺段处理靠 `mode` 参数切换:进行中用 `progressive`(未到达段计 0,分数由小到大爬升),完成态/历史/分享用 `normalized`(缺段按有数据项归一化);四段全有数据时两模式等价。出网全失败或缺出网段时总分强制 0 并标 `unreliable`。完整权重、双模式与可信度规则见 [ADR 0022](adr/0022-exam-weighted-score-dual-mode.md)。不落存储,历史报告用当前公式实时重算。

## 分享卡

体检完成态与历史报告卡都可生成一张分享 PNG(`ExamShareCard.vue` + 纯函数 `sharecard.ts`,前端 html-to-image 渲染,下载 + 复制剪贴板)。分享卡是独立设计的版式,不是对话框截图。

**隐私默认保护**是分享卡的安全约束:

- 节点名**默认打码**(`maskNodeName`:保留可辨识前缀,尾段以 `***` 遮蔽),单一主开关「显示全部信息」控制脱敏摘要版 vs 全量版。
- **默认序列化不含任何 IP**:出口 IP、入口 IP(节点 server 地址,高敏)、出口 DNS 仅在显式开启全量版时才出现。入口 IP 经参数传入,不在报告数据里。
- UUID/密码等凭证类字段永不出现在卡片上。

节点分享另有二维码入口:节点行/抽屉可出该节点分享 URI(机场节点用订阅解析出的原始链接,自建节点经生成器产出)的二维码,扫码即导入单节点;无链接时降级为不显示入口而非报错。

## 前端展示

四段的展示逻辑拆成**纯函数 + 展示段组件**,实时体检与历史报告卡**复用同一套**(无双份实现):

- 纯函数:`web/src/components/exam/{egress,stability,regionspeed,unlock,score,sharecard,examhistory}.ts`(评分档位、格式化、sparkline 坐标与坐标轴留白、相对时间、行摘要、总分派生、打码、时间线视图模型),node 环境 vitest 覆盖。
- 段组件:`EgressSection.vue` / `StabilitySection.vue` / `RegionSpeedSection.vue` / `UnlockSection.vue` / `OverallScoreSection.vue`。
  - 实时:`NodeExamDialog.vue` 喂 SSE 流式数据(含采样序列 -> 画 sparkline;总分按 `progressive` 模式随段到达爬升)。
  - 历史:`ExamReportCard.vue` 喂一份静态 `ExamReport`(无采样序列 -> `show-sparkline=false`;总分按 `normalized` 模式)。
- 入口:
  - **节点行摘要徽标**(`NodeTable.vue`):最近一次体检的"稳定性 87 · 3小时前",无历史不占位。当前无批量 latest 接口,按当前页节点并发拉 `latest`(`useExamSummaries`)。
  - **抽屉体检历史时间线**(`NodeDetailDrawer.vue` + `ExamHistoryTimeline.vue`):历次体检列表(时间/稳定性分/解锁摘要),空历史给"去跑一次深度体检"引导态;点开任一条渲染完整四段报告卡。50 条上限,时间线内分批渲染。

## 与其他模块的边界

- 依赖 `internal/detection`(会话、采样器、测速器、解锁判定)与 `internal/store`(历史读写),不反向依赖 server 层以外。
- 解锁判定层与批量检测共享 `Result` 与 kind 注册表;体检只是它的另一个调用点。
- 体检报告是自动标签的数据源之一:每场体检完成后触发该节点标签重算(解锁/出网质量/稳定性档/fast),派生与落库见 [ADR 0023](adr/0023-auto-tag-derivation-persist-recompute.md)。
- 任务化机制(后台运行、附加、取消、续跑)全部下沉通用运行时,见 [ADR 0019](adr/0019-generic-async-jobs-runtime.md)。

## NodeKey Upsert Carry-Forward 规则

节点刷新时,通过 `internal/subscription.MergePool` 将旧池节点的状态合并到新池。匹配键为 `NodeKey()`(`server:port[:SNI]`),合并规则:

### Carry-Forward 字段清单

从旧节点保留到新节点的字段(新节点 fetch 不提供):

- **检测状态**:`DetectionLastCheck` / `Available` / `Latency` — 真实检测确认过的可用性与延迟,不被 TCP 快速判定覆盖
- **带宽结果**:`BandwidthDownMbps` / `BandwidthUpMbps` / `BandwidthCheck` — 最近一次带宽测试结果
- **用户覆盖**:`DisplayName` 覆盖(来自 `node_overrides` 表,通过 `aggregator.execute` 应用) — 机场节点用户改名/改地区跨刷新保留

### 不 Carry-Forward 的字段

从新节点覆盖的字段(fetch 提供,每轮更新):

- **连接参数**:`Server` / `Port` / `Password` / `UUID` / `Network` / `TLS` / `SNI` / 所有协议特定字段 — 机场可能更新节点配置,必须用新值
- **原始元数据**:`Name`(机场原名) / `Region`(地区识别结果,每轮重跑) / `Source` / `AirportID`

### Stale 生命周期

机场订阅本轮未返回的节点(fetch 结果中无匹配 NodeKey):

1. **标记阶段**:`Stale=true`,`LastSeen` 保持上次出现时间,节点仍留在池中(不删除)
2. **订阅排除**:serve-time 订阅生成(`internal/subscription/generator.go`)过滤掉 `Stale=true` 节点,不下发给客户端
3. **状态页可见**:前端节点列表显示 stale 标记(置灰行),用户可查看完整信息与历史
4. **手动清理**:通过 `/api/nodes/cleanup` 批量屏蔽或删除,需用户二次确认
5. **不自动过期**:不实现定时 GC,避免误删用户仍想保留测试的节点

**自建节点永不 stale**:不来自 fetch,由 `internal/subscription.InjectSelfNodes` 注入,用户通过 `self_nodes` 表显式管理启停/删除。

### 深度体检结果不混入节点快照

多地域测速(`RegionSpeedMetrics`)与解锁判定(`UnlockMetrics`)属于深度体检(`ExamReport`),存储在独立表 `exam_history`,不写入 `nodes` 表,不参与 carry-forward。节点快照只保留轻量检测状态(可用性/延迟/带宽),深度体检有独立历史查询接口(`/api/nodes/exam/history`)。
