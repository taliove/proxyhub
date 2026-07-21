# ADR 0026: 刷新迁入 jobs 运行时 + 单机场刷新

## 状态
**已采纳** — 2026-07-21

## 背景

ADR 0019 确立"系统只有一种异步风格"(jobs 运行时),但刷新一直是最明显的体系外双轨:自有 atomic 锁 + 409 互斥 + 独立的 refresh_runs 历史入口,任务中心看不到刷新。同时用户需求要求刷新支持单机场粒度、拉取并行、任务中心可见可取消。spec 见 `.scratch/spec/spec-refresh-tasks.md`(过程稿),术语已沉淀进 CONTEXT.md「刷新」词条。

## 决策

### 1. 刷新成为 jobs kind(key=all | airport-<id>)

- 手动/定时/启动三触发源都经 jobs 运行时发起,trigger 记入 params。同 key 重复触发按 kind+key 单实例附加(不再有 409);已收口任务用 `OpenIDForce` 重开新一轮。
- 废除 aggregator 的 atomic 锁。互斥细化到**机场级**:全量与任何进行中刷新互斥,不同机场的单机场刷新可并行;冲突检查 + 发起任务在同一临界区(`refreshStartMu`),且以 Manager 内存态(`RunningKeys`)为准而非 DB(jobs 持久化是尽力而为,纯内存任务对 DB 查询隐身)。
- **取消=中断不回滚**:拉取停止启动新机场,已拉取部分照常入池。关键正确性约束:取消时**只对成功拉取的机场做 MergePool**,未拉取的机场节点原样保留——直接走全量 MergePool 会把未拉取机场的节点全部标 stale(取消 ≠ 机场消失)。
- refresh_runs 保留为刷新的结果详情表(对齐 exam→exam_history 模式),加 `job_id` 关联列;前端独立"刷新日志"入口撤除,任务详情按 job_id 反查展示事件流与统计。进程启动时把残留的 running 刷新记录标 failed(死进程残留)。

### 2. 单机场刷新 = 只拉取入池

`POST /api/airports/{id}/refresh`:拉取→解析→地区识别→MergePool upsert(复用 ADR 0025 口径,已上移 `internal/poolops`),**不跑健康检查**。用于刚加机场/换订阅 token 后秒级可见;可用性交给后续检测或全量刷新。不同机场并行拉取允许,池写串行(`singleUpsertMu`,读-改-写全池的 upsert 并行会丢更新)。

### 3. 拉取并行度为系统设置

`fetch_concurrency`(默认 4,clamp 1-10),只作用于拉取阶段;健康检查并发是独立既有配置。并行拉取的结果按机场列表顺序归并,不随完成序漂移。

### 4. jobs.Manager 配套演进

- `OpenIDForce`:返回行 id + 是否新启动,已收口强制重开(刷新"再点一次就再跑一轮"语义)。
- `RecoverOwn`:**只处理本 Manager 注册的 kind**。多 Manager 共享一张 jobs 表(retag/batch_detection/exam/batch_exam/refresh),旧 `Recover` 会把其他运行时正在续跑的 running 记录误标 interrupted——全部调用点已迁 RecoverOwn,`Recover` 仅保留给单 Manager 场景与测试。
- `RunningKeys`:内存态进行中 key 列表,供跨 key 互斥判断。

## 被拒绝的替代方案

- **多任务模型**(全量拆成 N 个单机场任务行):任务中心刷屏,"全量"整体语义散掉;单任务模型内并行 + 事件流展示每机场进度已满足可见性。
- **事件流搬进 job events**:512 帧环形缓冲、重启即丢,不适合刷新这种长详情;refresh_runs 持久表保留。
- **取消时回滚已拉取部分**:与"单机场失败不阻断"的既有容错口径矛盾,且实现复杂;部分入池 + cancelled 状态标记足够。

## 影响

- `internal/aggregator`:refresh_job.go(新 kind)、execute 取消部分合并、fetchAirports 并行化、Run 定时/启动改发任务。
- `internal/jobs`:OpenIDForce / RecoverOwn / RunningKeys。
- `internal/store`:refresh_runs.job_id 幂等迁移、FailRunningRefreshRuns。
- `internal/poolops`(ticket 01 上移,airporttest 与刷新共用单机场 upsert 口径)。
- 前端:任务中心承接刷新(来源筛选/详情事件流),独立刷新日志页删除,机场行加单机场刷新按钮。
- 机场测试(airporttest.Orchestrator)仍自成一体,迁入 jobs 留作后续(待办)。
