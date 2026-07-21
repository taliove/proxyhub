# ADR 0019: 通用异步任务运行时

状态: accepted
日期: 2026-07-21

## 上下文

深度体检最初绑死在 SSE 请求上:请求断开则任务消失,无法后台续跑、无法多客户端附加同一场体检、进程重启后遗留状态无人收口。随后又出现批量解锁检测、批量体检、批量打标、夜间定时重算等一批"长耗时、需进度反馈、可能被取消"的任务。

若每种任务各写一套生命周期(启动/取消/缓冲/订阅/持久化),会分裂出多种异步风格,机制重复且各自的竞态(取消到达时刻、迟到附加、重启恢复)各修一遍。

## 决策

抽出通用异步任务运行时 `internal/jobs`,系统从此只有一种异步风格。核心契约:

- **按 `kind + key` 单实例调度**:同一 key 重复启动 = 附加到现有任务而非另起(`Manager.Open`);`OpenForce` 丢弃已收口旧任务重开。
- **事件缓冲(回放 + 直播)**:每任务一个环形缓冲(默认 512 帧),订阅者先收 `Replay` 再转 `Live`;`Event.Seq` 单调递增供前端去重。运行时不解释 `Data`(kind 自定义 JSON 载荷)。
- **取消与终态**:`Cancel` 走任务自带 ctx;状态机 `running -> done/failed/cancelled`,进程重启时仍 running 的记录按 `Resumable()` 分流。
- **续跑 vs 中断**:`Resumable()=true` 的批量任务重启时从持久化 `cursor` 续跑(`progress(cursor)` 每推进一项落一次);`Resumable()=false` 的单发任务(体检)重启标记 `interrupted`。
- **完成 hook**:`OnComplete` 在 finalize 之后、读到权威 `cancelled` 状态之后调用,消除"取消到达时刻"竞态——落副作用(体检落 `exam_history`、批量任务汇总)在此,不在 Run 内。
- **TTL 清扫**:任务结束后结果保留一段时长(默认 5min)供迟到附加回放,过期清理。

具体 kind 只实现 `Kind` 接口(`Name/Run/Resumable`)与可选 hook(`OnComplete`/`CancelEvent`),把领域形态(体检的 `ExamFrame` 帧协议、批量的进度事件)留在自己包里,机制全部下沉运行时。凭证绝不进 `jobs` 表(安全红线):活节点经内存旁路(按 key 索引的 `sync.Map`)传递,`params_json` 只存 `node_key`。

## 后果

### 正面
- 单发体检(`exam`)、批量体检(`batch_exam`)、批量打标(`retag_all`)、定时调度(`scheduler`)全部长在同一运行时,机制一处维护、竞态一处修。
- 后台任务与 SSE 解耦:请求断开任务不死,多客户端可附加同一任务,进程重启有明确恢复语义。
- 取消/落历史的时序竞态被 `OnComplete` 的 finalize-after 语义根除。

### 负面
- 多了一层抽象:新任务要理解 kind 接口与 params/cursor/emit/progress 的职责边界,而非直接写个 goroutine。
- 内存旁路传活节点(凭证)要求 kind 自行管理生命周期(体检用 `LoadAndDelete`),疏忽会留存凭证对象。

### 被放弃的备选
- **每种任务各自实现生命周期**:机制重复、异步风格分裂,取消/恢复/附加竞态各修一遍,否决。
- **凭证入 params_json 持久化**:违反安全红线(凭证不入库),否决;改内存旁路。

## 参考
- [design-node-exam](../design-node-exam.md):体检/批量体检如何长在本运行时上。
