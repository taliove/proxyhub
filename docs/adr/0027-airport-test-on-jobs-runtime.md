# ADR 0027: 机场测试迁入 jobs 运行时 + 跨 kind 互斥

## 状态
**已采纳** — 2026-07-22

## 背景

ADR 0019 确立"系统只有一种异步风格",ADR 0026 把刷新迁入后,机场测试是唯一剩下的体系外异步链路:POST /api/airports/{id}/test 同步建行 + 裸 goroutine 跑编排,无单实例保护(连点=并发 run 都写池)、无取消(ctx 是 Background)、重启残留永久悬挂、任务中心不可见。spec 见 `.scratch/spec/spec-airport-test-jobs-runtime.md`(过程稿)。本 ADR 落地 triage issue 0025,收口 ADR 0019 的最后一块。

## 决策

### 1. 机场测试成为 jobs kind(kind=airport_test,key=airport-<id>)

- kind 包装既有 Orchestrator(抽样/检活写回/评分算法不动,ADR 0024/0025),`Resumable()=false`:重启即 interrupted,对齐 refresh;run 行残留由 `FailRunningAirportTestRuns` 收口为 failed(0024 已铺)。
- 诊断拉取与建行移入任务执行:旧handler同步建行在单实例语义下会让"连点附加"产生永不推进的孤儿 run,且 Background ctx 不可取消。fetch 走新的 `subscription.Fetcher.FetchContext`,ctx 取消即中断。
- run 行建行即回填 `job_id`(写入侧反查,ADR 0026 样板);/api/jobs/{id}/result 按 job_id 反查 run 报告(0022 机制衔接)。
- **取消=ctx 中断不回滚**:已完成的抽样检活写回保留;取消诱导的失败结果(Error 为 context.Canceled)不是真实测量,不回写池、不计进度,避免把"被取消"误诊为节点不可用。run 行标 cancelled 并保留已产出诊断数据。
- **run 行 cancelled 取独立枚举值**(非 failed+error_message 标注),对齐 jobs.StatusCancelled 与 refresh_runs 的 cancelled 口径,任务中心状态展示一致;编排内业务失败(池空+URL不通)run 行 failed,jobs 行同步 failed。
- **进度双源主从:jobs cursor 为主**(`{"phase","checked","total"}`,progress 回调即时持久化,/api/jobs/{id} 轮询消费),run 行 sample_params 为镜像照旧写——顺带修复了 store.UpdateAirportTestRun 不含 sample_params 导致镜像被静默丢弃的旧缺陷。

### 2. 跨 kind 互斥:同机场刷新↔测试 409

刷新与测试分属两个 Manager(kind+key 单实例不天然跨 kind),机场级互斥显式协调:**两个方向的"冲突检查+发起"共用 aggregator 的 refreshStartMu 临界区**——

- 测试发起:server → `Aggregator.StartAirportTestExclusive(airportID, startFn)`,锁内查刷新 RunningKeys(全量或同机场在跑即 `ErrAirportTestConflict` → 409),无冲突则锁内调 startFn 完成 OpenIDForce。
- 刷新发起:startRefresh 锁内追加查测试侧回调 `airportTestConflict(airportID)`(server 装配期经 `SetAirportTestConflictChecker` 注入,读测试 Manager 的 RunningKeys;全量刷新视角下任一机场测试在跑即冲突,归并到 ErrRefreshConflict → 409)。
- 互斥判断一律以 Manager 内存态为准(ADR 0026 既有原则:持久化是尽力而为)。不同机场不互斥;同机场连点测试按 kind+key 单实例附加。

### 3. 接口薄封装

POST /api/airports/{id}/test 保留,改发任务返回 `{jobId, kind, key, started}`(与 /airports/{id}/refresh 同构);通用取消 POST /api/jobs/{kind}/{key}/cancel 加 airport_test 分支;旧裸 goroutine 路径删除。

## 被拒绝的替代方案

- **测试 kind 注册进刷新的 Manager**:Orchestrator 及其检活/池缝装配在 server 侧,注册进 aggregator 的 Manager 会造成 server↔aggregator 循环依赖;双 Manager + 共享临界区锁达到同样互斥强度,爆炸半径更小。
- **run 行 cancelled 复用 failed+标注**:任务中心已有 cancelled 状态口径(jobs.StatusCancelled、refresh_runs cancelled),详情表再发明一套标注口径只会让前端映射多一个特例。
- **同步边界保留(handler 同步拉取诊断建行)**:与单实例语义冲突(孤儿 run),且诊断在 Background ctx 下不可取消;移入任务后取消覆盖全流水线。

## 影响

- `internal/airporttest`:job.go(新 kind)、RunTest 取消语义与进度上报、TestRun.JobID、StatusCancelled。
- `internal/server`:handleAirportTest 改发任务、handlers_jobs cancel 分支、handlers_job_result airport_test 分支、server.go 运行时装配与跨 kind 注入。
- `internal/aggregator`:StartAirportTestExclusive / SetAirportTestConflictChecker / startRefresh 测试冲突检查(refreshStartMu 语义泛化为"机场级任务发起临界区")。
- `internal/store`:GetAirportTestRunByJobID、UpdateAirportTestRun 持久化 sample_params、AirportTestStatusCancelled。
- `internal/subscription`:Fetcher.FetchContext。
- 口径勘误:ADR 0026 §2 的 `singleUpsertMu` 描述已过期——池写串行 0024 起挪进 poolops 包级 upsertMu(ADR 不可变,记于此)。
- ADR 0019 收口:系统内不再有体系外异步链路;机场自动测试(定时/刷新联动)由此解锁,归 backlog。
