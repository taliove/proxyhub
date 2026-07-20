# Spec: 安全审计 + 访问统计

> 已 grill 钻透。两块独立但本次一并实现（共用一轮提交）。

## Problem Statement

**安全审计缺口**：登录成功/失败/蜜罐命中/封禁，目前只写 slog 日志，没有落库成审计事件。`banned_ips` 表只存当前封禁状态（计数器），没有历史流水、没有 List 方法、没有解封能力。运维无法回溯"谁在什么时候尝试登录/被封"。

**访问统计缺口**：`pull_logs` 完整记录每次拉取，`EndpointStats(id)` 已能按 IP 聚合（次数/最后拉取/地理信息），`GET /api/endpoints/{id}/stats` 已挂路由——数据和 API 都齐了，**但前端 Endpoints.vue 完全没用这个 stats**。用户看不到哪些 IP 在拉订阅、拉了多少次。

## Solution

### 安全审计（新建审计流水 + 封禁管理）

**审计事件表 `audit_logs`**：
```sql
CREATE TABLE audit_logs (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    event_type  TEXT NOT NULL,        -- login_success | login_failure | honeypot_ban | threshold_ban
    ip          TEXT NOT NULL,
    username    TEXT NOT NULL,        -- 尝试的用户名（失败/蜜罐时有意义）
    detail      TEXT,                 -- 补充信息（如封禁时长、失败计数）
    created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_audit_logs_created_at ON audit_logs(created_at DESC);
```
- 保留策略：90 天（与 `node_health` 同套 Prune 机制），定期清理 `created_at < 90天前` 的记录

**Store 补充**（internal/store/security.go）：
- `RecordAuditEvent(event_type, ip, username, detail)` — 插入审计流水
- `ListAuditEvents(filters, limit, offset) (events, total)` — 按事件类型/IP/时间范围过滤，分页返回，倒序
- `PruneAuditLogs(olderThan time.Time)` — 删除超过保留期的审计记录
- `ListBannedIPs() ([]*BannedIP, error)` — 从 `banned_ips` 读当前封禁状态列表（ip, fail_count, banned_until）
- `UnbanIP(ip string) error` — 手动解封（`UPDATE banned_ips SET banned_until = NULL, fail_count = 0 WHERE ip = ?`）

**埋点**（internal/server/server.go `handleLogin`）：
- 登录成功 → `RecordAuditEvent("login_success", ip, username, "")`
- 登录失败 → `RecordAuditEvent("login_failure", ip, attemptedUsername, "")`
- 蜜罐命中 → `RecordAuditEvent("honeypot_ban", ip, honeypotUsername, fmt.Sprintf("banned until %s", bannedUntil))`
- 达到阈值封禁 → `RecordAuditEvent("threshold_ban", ip, attemptedUsername, fmt.Sprintf("fail_count=%d, banned until %s", threshold, bannedUntil))`

**API**：
- `GET /api/audit/events?event_type=...&ip=...&time_range=24h|7d|30d|all&limit=50&offset=0` → 审计流水（分页）
- `GET /api/audit/banned` → 当前封禁 IP 列表
- `POST /api/audit/unban` body `{ip}` → 手动解封

**前端「安全审计」页**（web/src/views/Audit.vue）：
- 上半区：**审计事件流水表**
  - 列：时间 / 事件类型（标签着色：成功=绿、失败=黄、封禁=红）/ IP / 用户名 / 详情
  - 过滤：事件类型多选下拉、IP 搜索、时间范围（24h/7d/30d/全部）
  - 分页：倒序（最新在前），每页 50 条
- 下半区：**当前封禁 IP 列表**（独立卡片）
  - 列：IP / 失败次数 / 封禁截止时间 / 操作（「解封」按钮）
  - 点击解封 → 调 `/api/audit/unban`，刷新列表
- 路由 `/audit` + 导航入口（Layout.vue 加菜单项「安全审计」，图标用 `<Warning />`）

---

### 访问统计（两处入口 + 全局汇总 + 趋势图）

**两处入口**：
1. **Endpoints 页行内展开**（就近速览）：订阅地址列表每行加「统计▼」按钮，点击展开该地址的 IP 聚合表（el-table 嵌套展开行）
2. **独立「访问统计」页**（全局视角 + 趋势）：顶部全局汇总卡片 + 趋势图 + 订阅地址下拉 + IP 表

**Store 补充**（internal/store/stats.go）：
- `GlobalStats() (totalPulls int, uniqueIPs int, activeEndpoints int)` — 全局汇总：总拉取数、独立 IP 数、活跃订阅数（最近 24h 有拉取的地址数）
- `PullTrend(days int) ([]TrendPoint, error)` — 返回最近 N 天每天每个订阅地址的拉取次数，用于趋势图
  ```go
  type TrendPoint struct {
      Date       string // YYYY-MM-DD
      EndpointID int64
      Alias      string // 订阅地址别名
      Count      int
  }
  ```
  查询示例：`SELECT date(pulled_at) AS date, endpoint_id, COUNT(*) FROM pull_logs WHERE pulled_at > ? GROUP BY date, endpoint_id ORDER BY date`

**API**：
- `GET /api/stats/global` → `{total_pulls, unique_ips, active_endpoints}`
- `GET /api/stats/trend?days=7` → `[{date, endpoint_id, alias, count}, ...]`
- `GET /api/endpoints/{id}/stats` — 已存在，复用

**前端 Endpoints 页**（web/src/views/Endpoints.vue）：
- 表格行操作列增加「统计」按钮
- 使用 el-table `expand` 展开行机制：点击后在展开区调 `/api/endpoints/{id}/stats`，渲染 IP 表（IP / 次数 / 最后拉取 / 地区 / ISP）

**前端「访问统计」页**（web/src/views/Stats.vue）：
- 顶部卡片：全局汇总（总拉取、独立 IP、活跃订阅）
- 趋势图（ECharts 折线图）：横轴日期（最近 7 天 / 30 天可切换），纵轴拉取次数，按订阅地址分线（不同颜色）
- 下方：订阅地址下拉选择器 + 该地址的 IP 表（复用 Endpoints 展开行的同一份 IP 表组件）
- 路由 `/stats` + 导航入口（Layout.vue 加菜单项「访问统计」，图标用 `<TrendCharts />`）

---

## User Stories

**审计**：
1. 作为管理员，我想看到所有登录尝试的历史记录，这样我能发现爆破攻击。
2. 作为管理员，我想过滤出所有失败的登录尝试，这样我能定位可疑 IP。
3. 作为管理员，我想看到当前被封禁的 IP 列表，这样我知道谁被拦住了。
4. 作为管理员，我想手动解封某个 IP（误封或测试时），这样不用等超时。
5. 作为管理员，我想审计记录只保留 90 天，这样不会无限增长撑爆数据库。

**统计**：
6. 作为管理员，我想在订阅地址列表里快速查看某个地址的拉取统计，这样不用跳页。
7. 作为管理员，我想看到全局的总拉取次数和独立 IP 数，这样了解整体使用情况。
8. 作为管理员，我想看到最近 7 天的拉取趋势图，这样知道流量变化。
9. 作为管理员，我想按订阅地址分线查看趋势，这样知道哪个地址最活跃。
10. 作为管理员，我想看到每个 IP 的拉取次数和地理位置，这样知道用户分布。

## Implementation Decisions

**审计埋点在 handleLogin 一处集中落**，避免散在多处 security.go 方法（`RecordLoginFailure`/`BanIP` 只负责状态更新，不写审计流水）。审计流水与封禁状态分表存储（`audit_logs` 是历史流水，`banned_ips` 是当前状态），彼此独立查询。

**统计复用现有 `pull_logs` + `EndpointStats`**，不新增表。趋势图查询按天聚合（`date(pulled_at)`），SQLite 原生支持。ECharts 折线图配置：x 轴 category（日期），y 轴 value（次数），series 数组（每个订阅地址一条线）。

**前端组件复用**：Endpoints 行内展开与独立统计页的 IP 表抽成共享组件 `IPStatsTable.vue`（props: `endpointId`，内部调 `/api/endpoints/{id}/stats` 并渲染）。

**定期清理**：在现有 `aggregator.pruneHealth` 里增加 `st.PruneAuditLogs(time.Now().AddDate(0, 0, -90))`，复用同一套定期任务。

## Testing Decisions

沿用现有 TDD seam。

**Store 单元测试**（internal/store/security_test.go, stats_test.go）：
- `RecordAuditEvent` 插入后 `ListAuditEvents` 能读回；过滤条件正确（event_type/ip/时间范围）；分页准确
- `PruneAuditLogs` 只删老于阈值的，保留新的
- `ListBannedIPs` 返回当前封禁 + 已过期的（`banned_until > now` 为封禁中）；`UnbanIP` 清空 banned_until
- `GlobalStats` 聚合准确（总拉取 = pull_logs 行数，独立 IP = DISTINCT ip，活跃订阅 = 最近 24h 有拉取的 endpoint_id 去重数）
- `PullTrend` 按天聚合准确；跨天边界正确

**Server HTTP 集成测试**（internal/server/audit_test.go, stats_test.go）：
- 登录成功/失败/蜜罐命中后 `GET /api/audit/events` 能看到对应事件（event_type 正确）
- `GET /api/audit/banned` 返回当前封禁 IP；`POST /api/audit/unban` 解封后该 IP 从列表消失
- `GET /api/stats/global` 聚合准确（复用 store 单元测试埋的 pull_logs 数据）
- `GET /api/stats/trend?days=7` 返回按天分组的数据；订阅地址别名正确关联

## Out of Scope

- 后台操作审计（谁改设置/删节点/改模板）— 另一个量级，本次不做
- 实时告警（新 IP 拉取/异常流量尖刺）— 留作后续
- 审计导出（CSV/Excel）— 可视化够用，导出留作后续
- 订阅地址的"下载量排行榜"— 趋势图已能对比，榜单是冗余视图
- 地理分布地图 — ECharts 地图组件配置复杂，IP 表已有地理文本，够用
