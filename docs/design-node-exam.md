# 节点深度体检 - 设计

## 概述

深度体检对**单个节点**做一次纵向深挖,产出一份三段报告:稳定性、多地域测速、流媒体/AI 解锁。为什么是这三段、为什么解锁归批量而这两段归单节点,见 [ADR 0015](adr/0015-node-exam-capability-boundary.md);为什么三段串行、稳定性独占,见 [ADR 0017](adr/0017-exam-orchestration-serial-stability-exclusive.md);解锁判定的 kind 注册表与保守判定,见 [ADR 0016](adr/0016-unlock-detection-kind-registry.md)。本文描述"是什么",随代码演进。

## 报告模型

一次体检产出一个 `ExamReport`(`internal/detection/exam.go`),三段各一个可空指针(某段失败则为空):

```go
type ExamReport struct {
    Stability   *StabilityMetrics   `json:"stability,omitempty"`
    RegionSpeed *RegionSpeedMetrics `json:"region_speed,omitempty"`
    Unlock      *UnlockMetrics      `json:"unlock,omitempty"`
}
```

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

8 个 Linode DC 探测点(`internal/detection/exam_regionspeed.go`),纯 HTTP 下载 `100MB-<dc>.bin` 切片,每区测 TTFB 延迟 + 下行速率(不测上行):

美西 / 美东 / 法兰克福 / 新加坡 / 东京 / 悉尼 / 多伦多 / 孟买。

每区一行 `RegionResult{code, name, ttfb_ms, down_mbps, error}`;单区失败只置 `error`,不拖垮整段。DC 域名做成常量表便于随 Linode 实况调整。

### 解锁段(UnlockMetrics)

复用批量检测的 `Result`,逐目标一条,顺序同 `DefaultUnlockTargets`。关键字段:

- `level`:流媒体三档 `full` / `originals_only` / `blocked`;AI/通用目标不填。
- `region`:出口国家码(如 `JP`),尽力解析,解析不到留空。

字段语义与保守取值规则见 [ADR 0016](adr/0016-unlock-detection-kind-registry.md)。

## 关键流程

### 实时体检(SSE)

`GET /api/nodes/exam/stream?node_key=...`(或 `self_node_id=...`)建一个节点会话,按 [ADR 0017](adr/0017-exam-orchestration-serial-stability-exclusive.md) 的串行顺序跑,分段推事件:

```
sample(逐采样点) -> section_done(metrics) -> region(逐区) -> section_done(region_speed)
  -> unlock_result(逐目标) -> section_done(unlock) -> done
```

**成功完成**才落一条历史;中途失败/取消**不落盘**(语义钉在 `handleNodeExamStream` 及其测试)。

### 历史存储与查询

- 表 `exam_history(id, node_key, report_json, created_at)`(迁移 `010_exam_history.sql`)。
- 写入后**修剪该节点至最近 50 条**(`SaveExamHistory`,`id` 倒序保留 50,其余删)。
- 查询:`GET /api/nodes/exam/latest`(最近一次,无历史返回 JSON `null`)、`GET /api/nodes/exam/history`(时间倒序全量,无历史返回 `[]`)。两者缺 `node_key`/`self_node_id` 返回 400。

## 前端展示

三段的展示逻辑拆成**纯函数 + 展示段组件**,实时体检与历史报告卡**复用同一套**(无双份实现):

- 纯函数:`web/src/components/exam/{stability,regionspeed,unlock,examhistory}.ts`(评分档位、格式化、sparkline 坐标、相对时间、行摘要、时间线视图模型),node 环境 vitest 覆盖。
- 段组件:`StabilitySection.vue` / `RegionSpeedSection.vue` / `UnlockSection.vue`。
  - 实时:`NodeExamDialog.vue` 喂 SSE 流式数据(含采样序列 -> 画 sparkline)。
  - 历史:`ExamReportCard.vue` 喂一份静态 `ExamReport`(无采样序列 -> `show-sparkline=false`)。
- 入口:
  - **节点行摘要徽标**(`NodeTable.vue`):最近一次体检的"稳定性 87 · 3小时前",无历史不占位。当前无批量 latest 接口,按当前页节点并发拉 `latest`(`useExamSummaries`)。
  - **抽屉体检历史时间线**(`NodeDetailDrawer.vue` + `ExamHistoryTimeline.vue`):历次体检列表(时间/稳定性分/解锁摘要),空历史给"去跑一次深度体检"引导态;点开任一条渲染完整三段报告卡。50 条上限,时间线内分批渲染。

## 与其他模块的边界

- 依赖 `internal/detection`(会话、采样器、测速器、解锁判定)与 `internal/store`(历史读写),不反向依赖 server 层以外。
- 解锁判定层与批量检测共享 `Result` 与 kind 注册表;体检只是它的另一个调用点。

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
