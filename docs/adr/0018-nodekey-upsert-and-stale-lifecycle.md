# ADR 0018: NodeKey Upsert 与消失节点 Stale 生命周期

## 状态
**已采纳** — 2026-07-17

## 背景

聚合器每轮刷新时 `a.nodes = allNodesWithHealth` 整池替换,导致三个问题:

1. **检测状态丢失**:新 struct 的 `DetectionLastCheck` 为零值,`checkHealth` 降级逻辑把真实检测确认过的 `Available` 用 TCP 判定覆盖。真实测过的节点,下一轮刷新后回退成 TCP 判定。
2. **持久化全量替换**:`SaveNodePool` 执行 `DELETE + 全量 INSERT`,每轮都丢弃旧池,无法保留任何历史。
3. **消失节点无迹可查**:机场订阅本轮未返回的节点连同其测试历史一起蒸发,用户无法"测完再决定删不删"。

这三个问题的根因是缺少**节点唯一标识**与**跨轮合并语义**。

## 决策

### 1. NodeKey 作为稳定唯一标识

采用 `NodeKey()` 方法生成的 `server:port[:SNI]` 作为节点唯一标识(anytls 协议复用同 server:port 靠 SNI 区分,故键含 SNI)。NodeKey 成为:

- 内存池合并的匹配键
- 数据库 upsert 的唯一约束键
- 覆盖层(`node_overrides`)、带宽结果、清理操作的引用键

不引入新的数字 ID,与既有 `node_blocks` / `node_health` 的键语义保持一致。

### 2. Carry-Forward 合并语义

刷新流程改为:fetch + 地区识别 + 注入自建后,对本轮 `allNodes` 按 NodeKey 匹配旧池:

- **匹配到旧节点**:carry forward 旧节点的检测状态(`DetectionLastCheck` / `Available` / `Latency`)、带宽结果(`Bandwidth*`)、用户编辑覆盖层(`DisplayName` 覆盖)。新节点仅覆盖 fetch 能提供的连接参数与原始 `Name` / `Region`。
- **旧池中未匹配的机场节点**:标记 `Stale=true`,保持上次 `LastSeen`,并入本轮池尾部。
- **自建节点**:由注入保证一定在场,不参与 stale 判定。

合并逻辑抽取为纯函数 `internal/subscription.MergePool(old, fetched []*Node) []*Node`,聚合器只负责调用。

### 3. 消失节点 Stale 生命周期

机场订阅本轮未返回的节点:

- **标记 stale**:`Stale=true`,`LastSeen` 保持上次出现时间,继续留在池中。
- **订阅生成排除**:serve-time 订阅生成时过滤掉 `Stale=true` 的节点,不下发给客户端。
- **手动清理**:状态页可见 stale 节点并支持一键清理(批量屏蔽或删除)。
- **不自动过期**:本轮不实现定时 GC,避免误删用户仍想保留的节点。

自建节点永不标记 stale(不来自 fetch,用户显式管理)。

### 4. 数据库 Upsert 语义

`SaveNodePool` 改为事务内:

1. `UPDATE nodes SET stale=1`(全标记)
2. 对本轮池 `INSERT ... ON CONFLICT(node_key) DO UPDATE SET ..., stale=excluded.stale, position=excluded.position`

本轮未出现的历史行保持 `stale=1`。`position` 随本轮顺序写入,保持订阅顺序稳定。

`nodes` 表新增 `node_key TEXT` 列,建立唯一索引 `idx_nodes_node_key`。旧库迁移时先去重(保留 `position` 最小的一行)再建唯一索引,失败降级为非唯一索引并记日志(不阻断启动)。

### 5. 机场节点编辑覆盖层

用户对机场节点的展示层编辑(改名、改地区)独立于刷新快照生命周期,存储在独立表 `node_overrides`:

```sql
CREATE TABLE IF NOT EXISTS node_overrides (
    node_key     TEXT PRIMARY KEY,
    display_name TEXT NOT NULL DEFAULT '',
    region       TEXT NOT NULL DEFAULT '',
    updated_at   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

选择独立表而非 `nodes` 表列,因为:

- 覆盖层独立于刷新快照生命周期(清空 nodes 快照不应丢失用户编辑)
- 语义清晰(用户意图 vs 机器产物)
- 支持清除覆盖恢复原始值

覆盖应用点:`aggregator.execute` 在 carry-forward 后、写内存池前,套用覆盖(非空值覆盖对应字段)。自建节点不走覆盖层,直接编辑 `self_nodes` 表真实记录。

### 6. 带宽测试统一持久化

扩展 `Node` 结构与 `nodes` 表,支持带宽测试结果持久化:

- `BandwidthDownMbps` / `BandwidthUpMbps`:最近一次带宽测试的下行/上行速率
- `BandwidthCheck`:带宽测试时间戳

带宽测试结果同时写入 `node_health` 表(复用 `SaveDetectionResults`,target_name 为 `bandwidth`)与内存节点字段,carry-forward 时一并保留。

## 后果

### 正面

- **检测状态跨刷新保留**:真实检测确认过的节点状态不会因刷新回退,`checkHealth` 降级逻辑对老节点自然生效。
- **消失节点可追踪**:机场下架的节点保留在列表,用户可查看历史、确认后再清理,避免误删仍可用节点。
- **订阅顺序稳定**:upsert 语义保持 `position` 字段,订阅生成顺序不因去重改变而乱序。
- **用户编辑持久**:机场节点改名/改地区的编辑跨刷新保留,不被下轮 fetch 冲掉。
- **单一事实源**:NodeKey 成为全系统节点引用的唯一标识,消除 ID 体系分裂。

### 负面

- **旧库迁移复杂度**:旧库可能有重复 NodeKey 历史行,需要去重逻辑与降级处理。
- **池膨胀风险**:stale 节点一直累积会使内存池与订阅页面变大,需要用户手动清理(自动 GC 留待后续)。
- **carry-forward 字段维护**:新增需要跨刷新保留的字段时,必须同步更新 `MergePool` 逻辑。

### 不做的事

- **覆盖层不支持改连接参数**:改 server/port/协议等于换节点,NodeKey 会变、覆盖层失配。只允许改展示层字段(`display_name` / `region`)。
- **不自动过期 stale 节点**:定时 GC 留待后续,避免误删用户仍想保留的节点。
- **多地域测速/解锁结果不 carry-forward**:这些属于深度体检(`ExamReport`),有独立历史表(`exam_history`),不混入节点快照。

## 参考

- [ADR 0009](0009-node-management-filter-whitelist.md):节点池模型与刷新流程
- [ADR 0013](0013-node-management-pagination.md):节点管理分页与筛选
- [design-node-exam](../design-node-exam.md):深度体检模型,不与节点快照混淆
