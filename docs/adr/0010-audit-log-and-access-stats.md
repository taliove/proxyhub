# ADR 0010: 安全审计流水 + 访问统计

## 状态

已接受

## 上下文

两个可观测性缺口:

1. **审计**:登录成功/失败/蜜罐命中/封禁,原本只写 slog 应用日志,没有落库成可查询的审计事件。`banned_ips` 表只是当前封禁状态计数器(ip/fail_count/banned_until),既无历史流水,也无"列出全部封禁""手动解封"的能力。运维无法回溯谁在何时尝试登录/被封。

2. **统计**:`pull_logs` 已完整记录每次拉取,`EndpointStats(id)` 已能按 IP 聚合,`GET /api/endpoints/{id}/stats` 已挂路由——但前端完全没展示。

## 决策

### 审计
- 新建 `audit_logs` 表(event_type/ip/username/detail/created_at),记 4 类事件:`login_success` / `login_failure` / `honeypot_ban` / `threshold_ban`。保留 90 天。
- 埋点集中在 `handleLogin` 一处(成功/失败/蜜罐/阈值封禁四个分支),写审计失败只记日志不阻断登录。
- `banned_ips` 补 `ListBannedIPs` / `UnbanIP`,支持后台查看当前封禁 + 手动解封。
- 前端「安全审计」页:审计流水表(过滤 事件类型/IP/时间范围,分页倒序)+ 当前封禁 IP 列表(可解封)。

### 统计
- 复用 `pull_logs`,新增 `GlobalStats`(总拉取/独立 IP/活跃订阅) 和 `PullTrend(days)`(按天分订阅地址聚合)。不新增表。
- 两处入口:Endpoints 页行内展开(就近速览,复用 `IPStatsTable` 组件)+ 独立「访问统计」页(全局汇总卡片 + ECharts 趋势折线图 + 地址下拉 + IP 表)。

### 定期清理
- main.go 新增 `runMaintenance` 后台 goroutine:启动跑一次、之后每 24h 一次,清理审计日志(90 天)+ 健康历史(30 天)。此前 `PruneHealth`/`PruneAuditLogs` 定义了但从未被调度。

## 理由

- 审计范围只锁定"登录+封禁安全事件",不含后台操作审计(改设置/删节点/改模板谁做的)——那是另一个量级,本次不做。
- 审计流水与封禁状态分表:前者是 append-only 历史(`audit_logs`),后者是可变当前态(`banned_ips`),职责不同。
- 统计不新增表:`pull_logs` 已够,按天聚合用 SQLite `date()` 原生支持。

## 后果

- **SQLite 时间坑(重要)**:modernc.org/sqlite 驱动把 `CURRENT_TIMESTAMP` 写成 RFC3339 的 `"...T...Z"` 形式,而 SQLite 内建 `datetime()` 函数不认这个格式,导致按时间过滤/比较失效。解法:
  1. `RecordAuditEvent` 显式写 UTC `"2006-01-02 15:04:05"` 格式的 created_at(不依赖 CURRENT_TIMESTAMP);
  2. 时间过滤查询用 `datetime(created_at) >= datetime(?)` 包裹两侧强制按时间语义比较;
  3. `parseSQLiteTime` 补 RFC3339Nano 布局;可空列(如 detail、banned_until)用 `sql.NullString` 扫描避免 NULL→string 报错。
- Endpoints 页「统计」按钮从占位 TODO 改为 el-table expand 行,复用 `IPStatsTable`。
- 前端新增 echarts + vue-echarts 依赖(仅「访问统计」页懒加载,~487KB gzip 165KB)。

## 相关决策

- ADR 0003: SQLite 单文件存储
- 蜜罐/IP2Ban 机制见 internal/server/security.go 与 handleLogin
