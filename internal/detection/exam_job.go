package detection

import (
	"context"
	"sync"
	"time"

	"github.com/taliove/proxyhub/internal/subscription"
)

// 体检任务化:把深度体检从"绑死在 SSE 请求"改为服务端后台任务。
// 任务持有自己的 context(不从 r.Context() 派生),连接断开不杀任务;
// SSE 端点变为"无任务则启动、有任务则附加",附加先回放缓冲事件再转直播。
const (
	// examBufferCap 每个任务事件环形缓冲上限(>=500):附加时回放最近这么多帧。
	// 单次体检事件量(~50)远小于此,实际不会丢;溢出时丢最旧帧。
	examBufferCap = 512
	// examResultTTL 任务结束后结果保留时长,供迟到附加回放,过期清理。
	examResultTTL = 5 * time.Minute
	// examPhaseCancelled 取消任务时推送的终止帧 phase(区别于自然完成的 done)。
	examPhaseCancelled = "cancelled"
)

// ExamFrame 一帧体检事件:内嵌现状 ExamEvent(字段保持顶层不变),外加 Seq 供前端去重。
type ExamFrame struct {
	Seq int `json:"seq"`
	ExamEvent
}

// ExamRunner 运行一场完整体检:emit 逐事件推送,ctx 取消或跑完即返回报告。
// *Detector.ExamStream 的签名与此一致,可直接作为生产实现注入。
type ExamRunner func(ctx context.Context, node *subscription.Node, emit func(ExamEvent)) ExamReport

// examJob 单节点体检任务:自带 context、环形事件缓冲、订阅者列表。
type examJob struct {
	nodeKey string
	node    *subscription.Node

	ctx    context.Context
	cancel context.CancelFunc

	mu          sync.Mutex
	buffer      []ExamFrame          // 环形事件缓冲(容量 examBufferCap)
	nextSeq     int                  // 单调递增序号
	subscribers map[int]chan ExamFrame // 订阅者:id -> 直播通道
	nextSubID   int
	cancelled   bool      // 已请求取消(取消后抑制 runner 后续事件)
	done        bool      // 任务已收口(finalize 后为真)
	finishedAt  time.Time // 收口时刻,用于 TTL
}

// pushLocked 分配序号、写入缓冲并广播给当前订阅者(调用方持有 j.mu)。
// 在锁内做非阻塞发送:通道有缓冲(examBufferCap),正常读者不会满;满则丢弃该帧,
// 仅当丢弃帧仍在环形缓冲(最近 examBufferCap 帧)内时可由重连回放找回,超出即永久丢失
// —— 深度体检事件量(~50)远小于缓冲容量,实际不会触发。
// 持锁发送与订阅/退订/收口互斥,杜绝向已关闭通道发送。
func (j *examJob) pushLocked(e ExamEvent) {
	frame := ExamFrame{Seq: j.nextSeq, ExamEvent: e}
	j.nextSeq++
	j.appendBuffer(frame)
	for _, ch := range j.subscribers {
		select {
		case ch <- frame:
		default:
		}
	}
}

// emit 供 runner 使用:取消后抑制一切事件(含编排器在取消路径上仍会推的 done),
// 检查与推送在同一把锁下完成,避免抑制窗口漏帧;cancelled 终止帧由 runJob 单独补发。
func (j *examJob) emit(e ExamEvent) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.cancelled {
		return
	}
	j.pushLocked(e)
}

// appendBuffer 追加一帧,超过容量则丢弃最旧帧(调用方持锁)。
func (j *examJob) appendBuffer(f ExamFrame) {
	if len(j.buffer) >= examBufferCap {
		copy(j.buffer, j.buffer[1:])
		j.buffer = j.buffer[:len(j.buffer)-1]
	}
	j.buffer = append(j.buffer, f)
}

// subscribe 附加一个订阅者:原子地快照当前缓冲(replay)并注册直播通道(live)。
// 二者在同一把锁下取得,保证任一帧要么在 replay、要么在 live,不重不漏。
// 任务已收口时返回全量 replay 与一个已关闭的通道(读者回放完即结束),unsub 为空操作。
func (j *examJob) subscribe() (replay []ExamFrame, live <-chan ExamFrame, unsub func()) {
	j.mu.Lock()
	defer j.mu.Unlock()

	snapshot := make([]ExamFrame, len(j.buffer))
	copy(snapshot, j.buffer)

	if j.done {
		ch := make(chan ExamFrame)
		close(ch)
		return snapshot, ch, func() {}
	}

	ch := make(chan ExamFrame, examBufferCap)
	id := j.nextSubID
	j.nextSubID++
	j.subscribers[id] = ch

	var once sync.Once
	return snapshot, ch, func() {
		once.Do(func() {
			j.mu.Lock()
			defer j.mu.Unlock()
			if c, ok := j.subscribers[id]; ok {
				delete(j.subscribers, id)
				close(c)
			}
		})
	}
}

// isDone 报告任务是否已收口(供 manager 在 m.mu 下安全查询)。
func (j *examJob) isDone() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.done
}

// requestCancel 请求取消:任务已收口返回 false(无可取消);否则置位并取消 ctx。
func (j *examJob) requestCancel() bool {
	j.mu.Lock()
	if j.done {
		j.mu.Unlock()
		return false
	}
	already := j.cancelled
	j.cancelled = true
	j.mu.Unlock()
	if !already {
		j.cancel()
	}
	return true
}

// finalize 收口任务(单锁原子):读取最终取消状态,取消则补发 cancelled 终止帧,
// 再记录完成时刻、置 done,关闭并清空仍在册的订阅通道(读者回放/直播后结束),返回是否取消。
// 把"取消判定 + cancelled 帧 + done"放进同一把锁,消除取消与完成之间的落历史竞态:
// 落历史是否发生完全由本方法返回值决定,与 Cancel 的到达时刻无歧义。
func (j *examJob) finalize(now time.Time) (cancelled bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	cancelled = j.cancelled
	if cancelled {
		j.pushLocked(ExamEvent{Phase: examPhaseCancelled})
	}
	j.done = true
	j.finishedAt = now
	for id, ch := range j.subscribers {
		close(ch)
		delete(j.subscribers, id)
	}
	return cancelled
}

// ExamJobManager 按节点单实例管理体检任务:重复启动附加到现有任务而非另起。
type ExamJobManager struct {
	run        ExamRunner
	onComplete func(nodeKey string, report ExamReport) // 自然完成回调(落历史);失败/取消不调用
	ttl        time.Duration
	now        func() time.Time // 可注入时钟(TTL 虚拟时钟测试)

	mu   sync.Mutex
	jobs map[string]*examJob
}

// NewExamJobManager 构造任务管理器。onComplete 可为 nil(不落历史)。
func NewExamJobManager(run ExamRunner, onComplete func(nodeKey string, report ExamReport)) *ExamJobManager {
	return &ExamJobManager{
		run:        run,
		onComplete: onComplete,
		ttl:        examResultTTL,
		now:        time.Now,
		jobs:       make(map[string]*examJob),
	}
}

// ExamSubscription 一次 SSE 附加:先回放 Replay(带序号),再从 Live 转直播。
// 调用方结束时必须 Close 退订。任务已收口时 Live 为已关闭通道(回放完即结束)。
type ExamSubscription struct {
	Replay []ExamFrame
	Live   <-chan ExamFrame
	unsub  func()
}

// Close 退订本次附加(幂等)。
func (s *ExamSubscription) Close() { s.unsub() }

// Open 启动或附加该节点的体检任务,并返回一次订阅(回放 + 直播)。供 SSE handler 使用。
func (m *ExamJobManager) Open(nodeKey string, node *subscription.Node) *ExamSubscription {
	return m.open(nodeKey, node, false)
}

// OpenForce 强制开始一场新体检:已收口的旧任务直接丢弃重开(用于"重新体检",
// 避免 TTL 窗口内附加到上次结果);仍在运行的任务不打断,按 Open 语义附加。
func (m *ExamJobManager) OpenForce(nodeKey string, node *subscription.Node) *ExamSubscription {
	return m.open(nodeKey, node, true)
}

func (m *ExamJobManager) open(nodeKey string, node *subscription.Node, force bool) *ExamSubscription {
	j := m.startOrAttach(nodeKey, node, force)
	replay, live, unsub := j.subscribe()
	return &ExamSubscription{Replay: replay, Live: live, unsub: unsub}
}

// startOrAttach 该节点无任务(或已过期)则启动新任务,否则返回现有任务(启动或附加)。
// force 仅对"已收口"的旧任务生效:丢弃并重开;进行中的任务不受 force 影响。
func (m *ExamJobManager) startOrAttach(nodeKey string, node *subscription.Node, force bool) *examJob {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.sweepLocked(m.now())

	if j, ok := m.jobs[nodeKey]; ok {
		if force && j.isDone() {
			delete(m.jobs, nodeKey) // 已收口的旧任务,强制重开
		} else {
			return j
		}
	}

	// 任务自带 context.Background() 派生的 ctx —— 绝不从请求 ctx 派生,连接断开不杀任务。
	ctx, cancel := context.WithCancel(context.Background())
	j := &examJob{
		nodeKey:     nodeKey,
		node:        node,
		ctx:         ctx,
		cancel:      cancel,
		subscribers: make(map[int]chan ExamFrame),
	}
	m.jobs[nodeKey] = j
	go m.runJob(j)
	return j
}

// runJob 任务生命周期:跑体检 -> 收口(取消则补 cancelled 帧)-> 自然完成才落历史。
func (m *ExamJobManager) runJob(j *examJob) {
	report := m.run(j.ctx, j.node, j.emit)

	// finalize 原子返回最终取消状态:落历史与否只看它,取消到达时刻不再有歧义。
	// 落历史语义与既有 handler 一致:自然完成(未取消且跑完稳定性段)才落;失败/取消不落。
	cancelled := j.finalize(m.now())
	if !cancelled && report.Stability != nil && m.onComplete != nil {
		m.onComplete(j.nodeKey, report)
	}
}

// Cancel 取消该节点任务:无任务或已收口返回 false。
func (m *ExamJobManager) Cancel(nodeKey string) bool {
	m.mu.Lock()
	j, ok := m.jobs[nodeKey]
	m.mu.Unlock()
	if !ok {
		return false
	}
	return j.requestCancel()
}

// SweepExpired 清理超过 TTL 的已完成任务(供后台/测试触发)。
func (m *ExamJobManager) SweepExpired() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sweepLocked(m.now())
}

// sweepLocked 移除已完成且超过 TTL 的任务(调用方持有 m.mu)。
func (m *ExamJobManager) sweepLocked(now time.Time) {
	for key, j := range m.jobs {
		j.mu.Lock()
		expired := j.done && now.Sub(j.finishedAt) > m.ttl
		j.mu.Unlock()
		if expired {
			delete(m.jobs, key)
		}
	}
}
