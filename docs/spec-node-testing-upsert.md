# Spec: 节点测试统一化 + NodeKey Upsert + 带宽测试 + 一键清理失败节点

> 决策依据：CONTEXT.md、docs/spec-node-management.md、ADR 0008/0009/0012/0013。
> 2026-07-17 grilling 已钻透（四项决策均取最大档：机场节点可编辑覆盖层 / 消失节点保留标记 stale / 带宽下行+上行 / 全套一次做）。

## Problem Statement

节点状态页只读、测试能力割裂、刷新会抹掉检测结果。具体六个缺口：

1. **刷新静默抹掉真实检测状态**：聚合每轮 `a.nodes = allNodesWithHealth`（`aggregator.go:309`）整池替换成 fetch 出的全新 struct，新 struct 的 `DetectionLastCheck` 为零值，于是 `checkHealth`（`aggregator.go:397`）的降级逻辑把真实检测确认过的 `Available` 用 TCP 结果覆盖。真实测过"可用/不可用"的节点，下一轮刷新后回退成 TCP 判定。`SaveNodePool`（`nodes.go:12`）也是 `DELETE + 全量 INSERT`。
2. **测试路径不统一**：单节点测试（`Detector.TestNode` quick/real）返回瞬时 `TestResult`、**从不持久化**；批量解锁检测（`Detector.DetectAll`）写 `node_health`、状态页从 `GetLatestDetectionResults` 回读。单节点"真实检测"跑完在状态页看不到、也不影响后续清理。
3. **没有带宽测试**：只有连通性/延迟，无法判断节点是"通但慢到不可用"。
4. **状态页不能查看/编辑节点**：机场节点只能屏蔽、看不到完整信息（server/port/协议/各测试结果）；自建节点编辑要跳去 SelfNodes 页。
5. **机场订阅下架的节点直接消失**：整池替换后，机场这轮没返回的节点连同其测试历史一起从池里蒸发，无法"测完再决定删不删"。
6. **失败节点只能逐个处理**：无"一键真实测全部 → 二次确认后批量屏蔽(机场)/禁用或删除(自建)失败节点"的闭环。

## Solution

- **统一测试模块**：`Detector` 暴露单一 `RunTest(ctx, node, mode)`，mode ∈ `{quick, real, bandwidth}`。三档全部经同一持久化路径写 `node_health`（带宽档扩展列），状态页/清理闭环从同一数据源回读。单节点即时测试与批量检测复用同一 `RunTest`。
- **NodeKey Upsert（核心修复）**：以 `NodeKey()`（`server:port[:SNI]`，见 anytls 备注）为稳定唯一 ID。刷新时把旧池按 NodeKey 建索引，对 fetch 出的新节点 **carry forward** 旧节点的检测状态（`DetectionLastCheck` / 真实 `Available` / `Latency` / 带宽结果）。`SaveNodePool` 改 upsert（`INSERT … ON CONFLICT(node_key) DO UPDATE`）。
- **消失节点保留并标记 stale**：fetch 里不再出现的机场节点不删除，标记 `Stale=true` 并记 `LastSeen`；订阅生成时排除 stale；状态页可见并可一键清理。自建节点永不 stale（不来自 fetch）。
- **带宽测试（单节点起步）**：经 mihomo adapter 下行拉固定大小文件测 Mbps + 上行 POST 测 Mbps，可配置 URL/大小/超时/合格阈值。
- **状态页查看 + 编辑**：每行"查看"抽屉展示全字段 + quick/real/bandwidth 三档结果；机场节点可编辑覆盖层（改名/改地区等，跨刷新 upsert 保留）；自建节点内联编辑复用现有 API。
- **一键清理闭环**：一键真实测全部（复用批量检测 scope=all）→ 结果落 `node_health` → 前端筛出失败节点 → 二次确认 → 批量屏蔽（机场）/ 禁用或删除（自建）。

## User Stories

1. 作为用户，我真实检测确认过的节点，希望刷新后仍保持该状态，而非退回 TCP 判定。
2. 作为用户，我想让单节点测试和批量检测走同一套逻辑与展示，避免两处结果对不上。
3. 作为用户，我想对单个节点做带宽测试，知道它下行/上行各多少 Mbps。
4. 作为用户，我想带宽测试的探测 URL/文件大小/超时/合格阈值可配置，适配不同网络与流量预算。
5. 作为用户，我想在状态页展开任一节点看它的完整信息（含隐藏的 server/port/协议）和三档测试的最新结果。
6. 作为用户，我想直接在状态页编辑机场节点的展示信息（如改名、改地区），且这些编辑跨刷新保留、不被下轮 fetch 冲掉。
7. 作为用户，我想直接在状态页编辑自建节点，不用跳去另一个页面。
8. 作为用户，机场这轮订阅没返回的节点，我希望它仍留在列表里并标为"已下架"，这样我能先测/先确认再决定删不删。
9. 作为用户，我想已下架(stale)的节点自动不进订阅，避免下发失效节点。
10. 作为用户，我想一键对全部节点做真实检测，然后系统把失败的节点筛出来给我看。
11. 作为用户，我想对筛出的失败节点二次确认后一键处理：机场节点批量屏蔽，自建节点批量禁用或删除。
12. 作为用户，我想清理操作前看到将影响哪些节点、各多少个，避免误删。
13. 作为用户，我想机场节点的编辑覆盖层可以清除，恢复用机场原始信息。
14. 作为用户，我想带宽测试失败（超时/低于阈值）时看到明确原因，而非只是"不可用"。
15. 作为用户，我想 upsert 后订阅顺序仍稳定（不因去删改而乱序）。

## Implementation Decisions

### A. NodeKey Upsert（internal/aggregator, internal/store, internal/subscription）

**内存池 carry-forward（改 `aggregator.execute`）**
- fetch + 地区识别 + 注入自建后，得到本轮 `allNodes`。
- 用旧池 `a.nodes` 建 `map[NodeKey]*Node`。对每个新节点：若旧池存在同 NodeKey，把旧节点的 `DetectionLastCheck / Available / Latency / Bandwidth* / DisplayName覆盖` carry 过来（新节点仅覆盖 fetch 能提供的连接参数与原始 Name/Region）。
- 旧池中本轮 fetch 未出现的**机场**节点：标记 `Stale=true`、`LastSeen` 保持上次值，并入本轮池尾部（自建节点由注入保证一定在场，不参与 stale）。
- 结果赋给 `a.nodes`。carry-forward 逻辑抽到 `internal/subscription`（纯函数 `MergePool(old, fetched []*Node) []*Node`）便于单测，聚合只调用。
- **不改** `checkHealth` 降级判定本身（`DetectionLastCheck` 非零就不覆盖 Available）——carry-forward 让它对老节点自然生效。

**Node 字段（internal/subscription/types.go）**
```go
Stale     bool      `json:"stale,omitempty"`      // 机场订阅本轮未返回,保留待清理
LastSeen  time.Time `json:"last_seen,omitempty"`  // 最近一次在 fetch 中出现的时间
// 带宽结果(最近一次带宽测试)
BandwidthDownMbps float64   `json:"bandwidth_down_mbps,omitempty"`
BandwidthUpMbps   float64   `json:"bandwidth_up_mbps,omitempty"`
BandwidthCheck    time.Time `json:"bandwidth_check,omitempty"`
```
> Node 已有 `NodeKey()`，无需新增 ID 字段——NodeKey 即唯一 ID。

**持久化（internal/store/nodes.go + schema）**
- `nodes` 表加列（`addColumnIfMissing`，勿改建表语句破坏旧库）：`node_key TEXT`、`stale INTEGER DEFAULT 0`、`last_seen TIMESTAMP`、`bandwidth_down REAL DEFAULT 0`、`bandwidth_up REAL DEFAULT 0`、`bandwidth_check TIMESTAMP`、`detection_last_check TIMESTAMP`、`display_name_override TEXT DEFAULT ''`、`region_override TEXT DEFAULT ''`。
- 给 `nodes.node_key` 建唯一索引：`CREATE UNIQUE INDEX IF NOT EXISTS idx_nodes_node_key ON nodes(node_key)`。旧库可能有重复 NodeKey 历史行——迁移时先去重（保留 position 最小的一行）再建唯一索引，失败降级为非唯一索引并记日志（不阻断启动）。
- `SaveNodePool` 改为：事务内先 `UPDATE nodes SET stale=1`（全标记），再对本轮池 `INSERT … ON CONFLICT(node_key) DO UPDATE SET …, stale=excluded.stale, position=excluded.position`。本轮未出现的历史行保持 stale=1。position 仍随本轮顺序写入。
- `LoadNodePool` 读出新列，回填 `Stale/LastSeen/Bandwidth*/DetectionLastCheck` 及覆盖层。

### B. 机场节点编辑覆盖层（internal/store, internal/server, web）

- **新表** `node_overrides`：`node_key TEXT PRIMARY KEY, display_name TEXT DEFAULT '', region TEXT DEFAULT '', updated_at TIMESTAMP`。选独立表而非 nodes 列，因覆盖层独立于刷新快照生命周期（清空 nodes 快照不该丢覆盖），且语义清晰（用户意图 vs 机器产物）。
- store：`SetNodeOverride(key, displayName, region string) error`（upsert）、`ClearNodeOverride(key string) error`、`ListNodeOverrides() (map[string]NodeOverride, error)`。
- 覆盖应用点：`aggregator.execute` carry-forward 后、写内存池前，套用覆盖（`region` 非空盖 `Region`；`display_name` 非空作为强制 DisplayName 覆盖）。serve-time 标准化对已有强制 DisplayName 的节点跳过重命名。
- API：`PUT /api/nodes/override` body `{node_key, display_name?, region?}`、`DELETE /api/nodes/override` body `{node_key}`，均 requireAuth。
- 仅机场节点可编辑覆盖；自建节点走既有 `PUT /api/self-nodes/{id}`（编辑真实记录，非覆盖层）。前端据 `source` 分流。

### C. 统一测试模块（internal/detection, internal/server, web）

- `Detector.RunTest(ctx, node, mode) TestResult`，mode ∈ `quick|real|bandwidth`。`TestNode` 保留为 `RunTest` 薄封装（兼容既有单节点测试调用）。
- `TestResult` 扩展：`DownMbps float64`、`UpMbps float64`（仅 bandwidth 档非零）。
- **持久化统一**：单节点测试完成后也写 `node_health`（复用 `SaveDetectionResults`），target_name 用 `connectivity`（real/quick）或 `bandwidth`（带宽档）。这样状态页对单节点测试也能回读。带宽结果同时写回内存节点的 `Bandwidth*` 字段。
- `node_health` 加列（`addColumnIfMissing`）：`down_mbps REAL DEFAULT 0`、`up_mbps REAL DEFAULT 0`。`SaveDetectionResults` 与 `GetLatestDetectionResults` 带上这两列。
- 单节点测试 handler（`handleTestNode`）：带宽档超时放宽（下行+上行需更长），用独立超时常量。

### D. 带宽测试（internal/detection）

- `detectBandwidth(ctx, adapter, node) TestResult`：
  - 下行：经 mihomo adapter GET 配置的 URL（默认 Cloudflare `https://speed.cloudflare.com/__down?bytes=N`，N 默认 10MB），读满计时，`Mbps = bytes*8 / elapsed / 1e6`。
  - 上行：经 adapter POST 配置 URL（默认 `https://speed.cloudflare.com/__up`）发 N 字节（默认 5MB），计时算 Mbps。
  - 任一方向超时/出错 → `Available=false` 且 `Error` 记明方向与原因；两方向都成功且均 ≥ 阈值 → `Available=true`。
- 配置项（settings，复用 `GetSystemSettings`）：`bandwidth_down_url` / `bandwidth_up_url` / `bandwidth_down_bytes` / `bandwidth_up_bytes` / `bandwidth_timeout_sec` / `bandwidth_min_down_mbps` / `bandwidth_min_up_mbps`，缺省用代码默认常量。前端 Settings 页加这些项。

### E. 一键清理失败节点闭环（internal/server, web）

- **测全部**：复用现有 `POST /api/detection/trigger` scope=`all`（已存在），前端"一键真实检测全部"即触发它，轮询 `/detection/status`。
- **筛失败**：检测完成后前端用现有 `/api/nodes` 筛 `available=false`（已支持），得到失败节点列表。
- **批量处理**（新 API）：`POST /api/nodes/cleanup` body `{node_keys: []string, action: "block"|"disable"|"delete"}`：
  - `block`：机场节点批量屏蔽（复用 `BlockNodes`）。
  - `disable`/`delete`：自建节点批量启停/删除（按 node_key 反查 self_node id）。
  - 混合列表：按 source 分流——机场走 block，自建走 disable/delete；返回各类计数。
- 前端 Nodes 页加"清理失败节点"入口：弹窗展示"将屏蔽 X 个机场节点 / 禁用(或删除) Y 个自建节点"，二次确认后调用。删除为高风险，默认 action=block(机场)+disable(自建)，删除需用户显式切换并再确认。

### F. 状态页查看/编辑（web/src/views/Nodes.vue）

- 现有 expand 行改造为"详情抽屉/展开"：全字段（server/port/协议/network/tls/sni/region/source/latency）+ 三档测试结果块（connectivity 通过率、带宽 down/up Mbps + 时间）+ stale 标记 + 操作按钮（编辑覆盖/编辑自建、测试三档、屏蔽/清理）。
- 列表加 stale 列/标签；stale 节点行置灰。
- 测试下拉加"带宽测试"档（复用 `useNodeTest`，扩展 mode 类型与结果展示）。
- 编辑：机场→覆盖层弹窗（display_name/region + 清除按钮）；自建→复用 SelfNodes 表单组件（抽成可复用组件或跳转带 id）。

### Schema Changes
```sql
-- nodes 增量列(addColumnIfMissing)
ALTER TABLE nodes ADD COLUMN node_key TEXT;                          -- 回填 = server||':'||port(||':'||sni)
ALTER TABLE nodes ADD COLUMN stale INTEGER NOT NULL DEFAULT 0;
ALTER TABLE nodes ADD COLUMN last_seen TIMESTAMP;
ALTER TABLE nodes ADD COLUMN detection_last_check TIMESTAMP;
ALTER TABLE nodes ADD COLUMN bandwidth_down REAL NOT NULL DEFAULT 0;
ALTER TABLE nodes ADD COLUMN bandwidth_up REAL NOT NULL DEFAULT 0;
ALTER TABLE nodes ADD COLUMN bandwidth_check TIMESTAMP;
-- 去重后建唯一索引(去重失败降级为普通索引)
CREATE UNIQUE INDEX IF NOT EXISTS idx_nodes_node_key ON nodes(node_key);

-- node_health 增量列
ALTER TABLE node_health ADD COLUMN down_mbps REAL NOT NULL DEFAULT 0;
ALTER TABLE node_health ADD COLUMN up_mbps REAL NOT NULL DEFAULT 0;

-- 机场节点编辑覆盖层(独立表,独立于快照生命周期)
CREATE TABLE IF NOT EXISTS node_overrides (
    node_key     TEXT PRIMARY KEY,
    display_name TEXT NOT NULL DEFAULT '',
    region       TEXT NOT NULL DEFAULT '',
    updated_at   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

### API Contracts
- `PUT /api/nodes/override` `{node_key, display_name?, region?}` → 200
- `DELETE /api/nodes/override` `{node_key}` → 200
- `POST /api/nodes/cleanup` `{node_keys:[], action:"block"|"disable"|"delete"}` → `{success, blocked, disabled, deleted}`
- `POST /api/nodes/test` 扩展 mode 支持 `"bandwidth"`，响应增 `down_mbps`/`up_mbps`
- `GET /api/nodes` nodeView 增 `stale`、`last_seen`、`bandwidth_down_mbps`、`bandwidth_up_mbps`；`unlock_results` 增 `bandwidth` 目标项
- settings 读写增带宽配置键

## Testing Decisions

好测试只验外部行为，不验实现细节。沿用现有 seam（`newTestServer`+`authCookie`+`httptest`；store 用内存 SQLite；detector 用 httptest 后端）。

- **subscription 单测** `MergePool`（新纯函数）：老节点检测状态 carry-forward；fetch 消失的机场节点标 stale 且保留；自建节点不标 stale；同 NodeKey 连接参数用新值；顺序稳定。
- **store 单测**（prior art `nodes_test.go`）：`SaveNodePool` upsert 幂等（两次写同池不炸唯一索引）；stale 标记回环；`node_overrides` upsert/clear/list；`SaveDetectionResults`/`GetLatestDetectionResults` 带宽列回环。
- **detection 单测**（prior art `testnode_test.go`）：`RunTest` bandwidth 档对 httptest 后端算出合理 Mbps；下行/上行任一超时→不可用且 Error 指明方向；低于阈值→不可用。
- **server 集成测试**（prior art `server_test.go`/`nodetest_handler_test.go`）：
  - 刷新两轮，第一轮真实检测置某节点 Available=false，第二轮 fetch 仍含它 → 状态保持 false（回归 bug#1）。
  - 机场节点覆盖层 PUT 改名/改地区 → `/api/nodes` 生效、刷新后仍在；DELETE 清除后回退原值。
  - 单节点 bandwidth 测试 → `/api/nodes` 能回读该节点 bandwidth 结果。
  - fetch 去掉某机场节点 → 该节点 `stale=true` 仍在列表、但 `/sub` 不含它。
  - `/api/nodes/cleanup` 混合列表：机场进屏蔽名单、自建被禁用/删除，计数正确。
- **回归**：既有 `/sub`、屏蔽名单、标准化、自建注入测试全绿（本改动不得破坏既有过滤链）。

## Out of Scope

- 多节点批量带宽测试（先做单节点即时带宽；批量走后续，避免一次拉爆流量与并发）。
- 带宽历史趋势图（本轮只存最近一次结果，趋势记 backlog）。
- stale 节点自动过期清理（本轮只标记 + 手动清理；定时 GC 记 backlog）。
- 覆盖层改 server/port/协议（改这些等于换节点，NodeKey 会变、覆盖层失配；只允许改 display_name/region 这类展示层字段）。
- 上行测试的公共探测点稳定性兜底（默认给 Cloudflare，失败让用户改配置）。

## Further Notes

- **NodeKey 是唯一 ID**：`server:port[:SNI]`。anytls 复用同 server:port 靠 SNI 区分，故键含 SNI（见既有 types.go 注释 + [[anytls-protocol]] 记忆）。覆盖层/清理/带宽结果全部以它为键，与既有 node_blocks/node_health 一致，不引入新 ID 体系。
- **标准化是浅拷贝**（`StandardizeNodes` 里 `cp := *n`），列表视图读的是 clone；所以检测/带宽结果统一从 `node_health` 回读（`toNodeViews` 已如此），别指望写回 clone 能持久。
- **fail-open 延续**：读覆盖层/带宽配置/检测结果失败时降级（跳过覆盖、用默认配置、不附带结果），不整体失败。
- **删除是高风险**：清理闭环删除自建节点前必须二次确认且默认不选删除，遵循 CLAUDE 安全约束。
- 无 git remote 语义下，本 spec 作为本地实现依据（同 docs/spec-node-management.md）。
- 既有两处默认模板测试从 main 起就红（见 [[proxyhub-known-failing-tests]]），别去动。
