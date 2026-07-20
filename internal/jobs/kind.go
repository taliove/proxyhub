// Package jobs 是通用异步任务运行时:注册任务 kind、按 kind+key 单实例调度、
// 事件缓冲/订阅(回放+直播)、取消、TTL 清扫,以及可选的持久化续跑。
//
// 提炼自体检任务(internal/detection 的旧 ExamJobManager):机制已验证,
// 此处泛化为不依赖具体事件类型的运行时。系统从此只有一种异步风格,
// 后续批量检测/批量体检/批量打标/定时调度都长在这个运行时上。
package jobs

import (
	"context"
	"encoding/json"
)

// Status 任务生命周期状态,也是 jobs 表持久化的 status 列取值。
type Status string

const (
	// StatusRunning 任务开始即落此态。
	StatusRunning Status = "running"
	// StatusDone 自然完成(Run 返回 nil 且未被取消)。
	StatusDone Status = "done"
	// StatusFailed Run 返回非取消错误。
	StatusFailed Status = "failed"
	// StatusCancelled 被显式取消(Cancel 触发)。
	StatusCancelled Status = "cancelled"
	// StatusInterrupted 进程重启时仍处于 running 且 kind 不可续跑,标记中断。
	StatusInterrupted Status = "interrupted"
)

// Event 一帧任务事件:Seq 单调递增供前端去重,Data 是 kind 自定义的 JSON 载荷。
// 运行时不解释 Data,只负责分配 Seq、环形缓冲与广播。
type Event struct {
	Seq  int             `json:"seq"`
	Data json.RawMessage `json:"data"`
}

// Kind 一种可注册的任务类型。实现应是无状态的:每次运行的状态经 params/cursor/emit 流转,
// 同一 kind 的多个 key 会并发复用同一 Kind 实例。
type Kind interface {
	// Name 稳定的 kind 标识,持久化进 jobs 表的 kind 列。
	Name() string
	// Run 执行一次任务。
	//   params  启动时传入的 JSON 参数(持久化进 jobs 表 params_json)。
	//   cursor  续跑游标:全新启动为空串,重启续跑时为上次持久化的游标。
	//   emit    推送一帧事件(运行时分配 Seq、入缓冲、广播给订阅者)。
	//   progress 记录续跑游标(立即持久化);批量任务每推进一项即调用。
	// 返回 nil=自然完成;返回 ctx.Err()=被取消;返回其它错误=失败。
	// 返回值可携带任务收口所需的数据(体检报告、批量任务结果等)。
	Run(ctx context.Context, params json.RawMessage, cursor string, emit func(json.RawMessage), progress func(cursor string)) error
	// Resumable 报告重启时仍 running 的任务应从游标续跑(批量任务)还是标记中断(单发任务)。
	Resumable() bool
}

// completionHooker 可选接口:kind 实现它则在任务自然完成(未取消且无错)时接到通知,
// 供落副作用(体检落 exam_history、批量任务汇总日志)。Hook 在 runJob finalize 之后、
// 读到权威 cancelled 状态之后调用,消除"取消到达时刻"的竞态。
type completionHooker interface {
	OnComplete(key string, runErr error)
}

// cancelEventer 可选接口:kind 实现它则在任务被取消时补发一帧终止事件
// (体检据此补发 {"phase":"cancelled"} 终止帧,保持 SSE 契约不变)。
type cancelEventer interface {
	CancelEvent() (json.RawMessage, bool)
}
