package detection

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/taliove/proxyhub/internal/jobs"
	"github.com/taliove/proxyhub/internal/subscription"
)

// 体检任务化:把深度体检从"绑死在 SSE 请求"改为服务端后台任务。
// 本文件是通用任务运行时(internal/jobs)之上的薄适配:机制(单实例、环形缓冲、
// 附加回放、取消、TTL、自带 ctx、终态持久化)全部下沉到 jobs.Manager,此处只
// 保留体检特有的对外形态(ExamFrame 帧协议、ExamRunner 注入、落 exam_history)。
const (
	// examPhaseCancelled 取消任务时补发的终止帧 phase(区别于自然完成的 done)。
	examPhaseCancelled = "cancelled"
	// examLiveBuffer 附加 SSE 直播通道缓冲(与 jobs 默认环形缓冲同量级)。
	examLiveBuffer = 512
)

// ExamFrame 一帧体检事件:内嵌 ExamEvent(字段保持顶层不变),外加 Seq 供前端去重。
// 帧协议对前端零改动:JSON 形如 {"seq":N, ...ExamEvent 字段}。
type ExamFrame struct {
	Seq int `json:"seq"`
	ExamEvent
}

// ExamRunner 运行一场完整体检:emit 逐事件推送,ctx 取消或跑完即返回报告。
// *Detector.ExamStream 的签名与此一致,可直接作为生产实现注入。
type ExamRunner func(ctx context.Context, node *subscription.Node, emit func(ExamEvent)) ExamReport

// examParams 体检任务的持久化参数。只存 node_key —— 节点会话凭证(UUID/密码)
// 绝不进 jobs 表(安全红线:凭证不入库);活节点经内存旁路传递(见 examKind.nodes)。
type examParams struct {
	NodeKey string `json:"node_key"`
	// UserID 任务属主(多租户):旁路按 (userID, nodeKey) 索引,回调按属主写池。
	// 与 jobs 行的 user_id 同值,0 = 未归属(旧调用路径)。
	UserID int64 `json:"user_id,omitempty"`
}

// examNodeRef 活节点旁路的索引:属主 + node_key(多租户下同 key 分属不同用户)。
type examNodeRef struct {
	userID  int64
	nodeKey string
}

// examKind 把体检实现为一个 jobs.Kind。单发任务(不可续跑):进程重启时中断标记 interrupted。
// 同一 kind 实例被多节点并发复用,故活节点存于按 key 索引的 sync.Map,而非实例字段。
type examKind struct {
	name       string // kind 名(默认 examKindName;出网+稳定性单节点检查等变体经 WithExamKindName 区分)
	run        ExamRunner
	onComplete func(userID int64, nodeKey string, report ExamReport)
	onErr      func(error)
	nodes      sync.Map // examNodeRef -> *subscription.Node(供 Run 消费的活节点,含凭证,仅内存)
	pending    sync.Map // examNodeRef -> *subscription.Node(open 暂存,OnStartFor 原子晋升进 nodes)
}

// examKindName 默认 kind 名(完整体检单发任务)。
const examKindName = "exam"

func (k *examKind) Name() string    { return k.name }
func (k *examKind) Resumable() bool { return false }

// CancelEvent 取消时补发的终止帧载荷({"phase":"cancelled"}),由 jobs 运行时在
// finalize 中原子补发,保持 SSE cancelled 语义不变。
func (k *examKind) CancelEvent() (json.RawMessage, bool) {
	b, err := json.Marshal(ExamEvent{Phase: examPhaseCancelled})
	if err != nil {
		if k.onErr != nil {
			k.onErr(fmt.Errorf("exam: marshal cancel event: %w", err))
		}
		return nil, false
	}
	return b, true
}

// OnStartFor 新任务创建后、Run goroutine 启动前(jobs 运行时在 manager 锁内同步调用):
// 把 open 暂存于 pending 的活节点原子晋升进 nodes,建立"节点就位 happens-before Run"。
// 仅创建路径触发;附加到既有任务不触发(其 pending 暂存由 open 收尾清理)。
// 属主感知(jobs.ownerStarter):同 nodeKey 不同用户的任务旁路互不覆盖。
func (k *examKind) OnStartFor(userID int64, nodeKey string) {
	ref := examNodeRef{userID: userID, nodeKey: nodeKey}
	if v, ok := k.pending.LoadAndDelete(ref); ok {
		k.nodes.Store(ref, v)
	}
}

// examResult 包装体检结果,承载 report(OnComplete 要用)经 Run 返回值传递给 hook。
type examResult struct {
	userID  int64
	nodeKey string
	report  ExamReport
}

func (e examResult) Error() string {
	return fmt.Sprintf("exam(%s): report ready", e.nodeKey)
}

// Run 跑一场体检:解析出活节点 -> 运行 runner(逐事件转 JSON emit)-> 返回包装的 report。
// 落历史决策在 OnComplete hook,读 finalize 后的权威 cancelled 状态,消除取消到达时刻竞态。
func (k *examKind) Run(ctx context.Context, params json.RawMessage, _ string, emit func(json.RawMessage), _ func(string)) error {
	var p examParams
	if err := json.Unmarshal(params, &p); err != nil {
		return fmt.Errorf("exam: bad params: %w", err)
	}
	v, ok := k.nodes.LoadAndDelete(examNodeRef{userID: p.UserID, nodeKey: p.NodeKey})
	if !ok {
		return fmt.Errorf("exam: no live node for key %q", p.NodeKey)
	}
	node := v.(*subscription.Node)

	report := k.run(ctx, node, func(e ExamEvent) {
		b, err := json.Marshal(e)
		if err != nil {
			if k.onErr != nil {
				k.onErr(fmt.Errorf("exam: marshal event: %w", err))
			}
			return
		}
		emit(b)
	})

	// 返回包装的 report(自然完成标记):OnComplete 取出并落历史。
	// 建会话失败等(report.Stability=nil)也到此,OnComplete 据此不落历史。
	return examResult{userID: p.UserID, nodeKey: node.NodeKey(), report: report}
}

// OnComplete 自然完成 hook(finalize 之后,已读权威 cancelled):取出 report,有稳定性段才落历史。
// 取消到达时刻无歧义:cancelled=true 时 runJob 不调此方法,与旧 finalize 原子决策语义等价。
func (k *examKind) OnComplete(key string, runErr error) {
	res, ok := runErr.(examResult)
	if !ok {
		// Run 返回的不是 examResult(编程错误,理论不达)。
		if k.onErr != nil {
			k.onErr(fmt.Errorf("exam: OnComplete called with non-examResult error: %T", runErr))
		}
		return
	}
	if res.report.Stability == nil {
		return
	}
	if k.onComplete != nil {
		k.onComplete(res.userID, res.nodeKey, res.report)
	}
}

// ExamSubscription 一次 SSE 附加:先回放 Replay(带序号),再从 Live 转直播。
// 调用方结束时必须 Close 退订。任务已收口时 Live 为已关闭通道(回放完即结束)。
type ExamSubscription struct {
	Replay []ExamFrame
	Live   <-chan ExamFrame

	closeOnce sync.Once
	done      chan struct{}
	inner     *jobs.Subscription
}

// Close 退订本次附加(幂等)。
func (s *ExamSubscription) Close() {
	s.closeOnce.Do(func() {
		close(s.done)
		s.inner.Close()
	})
}

// newExamSubscription 把通用订阅(jobs.Event)适配为体检订阅(ExamFrame):
// Replay 直接转换;Live 起一个转换 goroutine,读通用直播、转帧、转发,并在
// Close 或上游关闭时收尾,避免读者停读导致的阻塞。
func newExamSubscription(inner *jobs.Subscription) *ExamSubscription {
	replay := make([]ExamFrame, 0, len(inner.Replay))
	for _, ev := range inner.Replay {
		replay = append(replay, toExamFrame(ev))
	}

	out := make(chan ExamFrame, examLiveBuffer)
	done := make(chan struct{})
	go func() {
		defer close(out)
		for {
			select {
			case ev, ok := <-inner.Live:
				if !ok {
					return
				}
				select {
				case out <- toExamFrame(ev):
				case <-done:
					return
				}
			case <-done:
				return
			}
		}
	}()

	return &ExamSubscription{Replay: replay, Live: out, done: done, inner: inner}
}

// toExamFrame 通用事件 -> 体检帧:Data 是 marshal 后的 ExamEvent,反解并附上 Seq。
func toExamFrame(ev jobs.Event) ExamFrame {
	var e ExamEvent
	// Data 由 examKind.Run emit 刚 marshal,结构必定匹配,反解不会错;万一错了记日志,返回零帧。
	_ = json.Unmarshal(ev.Data, &e)
	return ExamFrame{Seq: ev.Seq, ExamEvent: e}
}

// ExamJobManager 按节点单实例管理体检任务:重复启动附加到现有任务而非另起。
// 内部是注册了 exam kind 的 jobs.Manager。
type ExamJobManager struct {
	mgr  *jobs.Manager
	kind *examKind
}

// ExamJobOption 配置体检任务管理器。
type ExamJobOption func(*examJobConfig)

type examJobConfig struct {
	kindName string
	store    *jobs.Store
	onErr    func(error)
}

// WithExamKindName 覆盖 kind 名:同进程多套单发任务管理器并存时区分
// (如出网+稳定性单节点检查用 "exam_stability",避免与完整体检的 "exam" 记录互相附加)。
func WithExamKindName(name string) ExamJobOption {
	return func(c *examJobConfig) { c.kindName = name }
}

// WithExamJobStore 注入 jobs 表存储:体检任务生命周期(running/终态/重启 interrupted)持久化。
func WithExamJobStore(st *jobs.Store) ExamJobOption {
	return func(c *examJobConfig) { c.store = st }
}

// WithExamErrorHandler 注入持久化错误回调(接到日志,避免静默吞错)。
func WithExamErrorHandler(h func(error)) ExamJobOption {
	return func(c *examJobConfig) { c.onErr = h }
}

// NewExamJobManager 构造体检任务管理器。onComplete 可为 nil(不落历史);
// 回调带任务属主 userID(多租户:写回按属主选池),0 = 未归属。
func NewExamJobManager(run ExamRunner, onComplete func(userID int64, nodeKey string, report ExamReport), opts ...ExamJobOption) *ExamJobManager {
	cfg := examJobConfig{kindName: examKindName}
	for _, o := range opts {
		o(&cfg)
	}
	k := &examKind{name: cfg.kindName, run: run, onComplete: onComplete, onErr: cfg.onErr}

	mopts := []jobs.Option{jobs.WithBufferCap(examLiveBuffer), jobs.WithTTL(5 * time.Minute)}
	if cfg.onErr != nil {
		mopts = append(mopts, jobs.WithErrorHandler(cfg.onErr))
	}
	mgr := jobs.NewManager(cfg.store, mopts...)
	mgr.Register(k)
	return &ExamJobManager{mgr: mgr, kind: k}
}

// Open 启动或附加该节点的体检任务,并返回一次订阅(回放 + 直播)。供 SSE handler 使用。
// 等价于 OpenFor(0, ...)(未归属分片,旧语义)。
func (m *ExamJobManager) Open(nodeKey string, node *subscription.Node) *ExamSubscription {
	return m.open(0, nodeKey, node, false)
}

// OpenForce 强制开始一场新体检:已收口的旧任务直接丢弃重开(用于"重新体检",
// 避免 TTL 窗口内附加到上次结果);仍在运行的任务不打断,按 Open 语义附加。
func (m *ExamJobManager) OpenForce(nodeKey string, node *subscription.Node) *ExamSubscription {
	return m.open(0, nodeKey, node, true)
}

// OpenFor 与 Open 同语义,但按属主分片(多租户):同 nodeKey 不同用户各自单实例。
func (m *ExamJobManager) OpenFor(userID int64, nodeKey string, node *subscription.Node) *ExamSubscription {
	return m.open(userID, nodeKey, node, false)
}

// OpenForceFor 与 OpenForce 同语义,但按属主分片(多租户)。
func (m *ExamJobManager) OpenForceFor(userID int64, nodeKey string, node *subscription.Node) *ExamSubscription {
	return m.open(userID, nodeKey, node, true)
}

func (m *ExamJobManager) open(userID int64, nodeKey string, node *subscription.Node, force bool) *ExamSubscription {
	// 活节点存内存旁路(凭证不进 params_json)。先暂存进 pending:若 mgr 判定为新任务,
	// 会在锁内、Run goroutine 启动前调 OnStartFor 把它原子晋升进 nodes(被 Run LoadAndDelete 消费),
	// 消除"Run 抢先于旁路写入"的竞态。附加到既有任务不触发 OnStartFor,暂存由收尾 LoadAndDelete 清掉,
	// 避免凭证对象留存超出任务生命周期(M2 修复)。
	params, err := json.Marshal(examParams{NodeKey: nodeKey, UserID: userID})
	if err != nil && m.kind.onErr != nil {
		m.kind.onErr(fmt.Errorf("exam: marshal params: %w", err))
	}

	ref := examNodeRef{userID: userID, nodeKey: nodeKey}
	m.kind.pending.Store(ref, node)

	var inner *jobs.Subscription
	if force {
		inner, err = m.mgr.OpenForceFor(userID, m.kind.Name(), nodeKey, params)
	} else {
		inner, err = m.mgr.OpenFor(userID, m.kind.Name(), nodeKey, params)
	}
	// 收尾清理暂存:创建路径 OnStartFor 已 LoadAndDelete(此处 no-op);附加路径 OnStartFor 未触发,
	// 此处删掉暂存,避免活节点(含凭证)留存超出任务生命周期。
	m.kind.pending.LoadAndDelete(ref)
	if err != nil {
		// 仅当 kind 未注册才可能到此(编程错误);此处 kind 恒已注册,理论不达。
		closed := make(chan ExamFrame)
		close(closed)
		return &ExamSubscription{Live: closed, done: make(chan struct{}), inner: &jobs.Subscription{}}
	}

	return newExamSubscription(inner)
}

// Cancel 取消该节点任务:无任务或已收口返回 false。
// 等价于 CancelFor(0, ...)(未归属分片,旧语义)。
func (m *ExamJobManager) Cancel(nodeKey string) bool {
	return m.mgr.Cancel(m.kind.Name(), nodeKey)
}

// CancelFor 取消指定属主的该节点任务(多租户)。
func (m *ExamJobManager) CancelFor(userID int64, nodeKey string) bool {
	return m.mgr.CancelFor(userID, m.kind.Name(), nodeKey)
}

// SweepExpired 清理超过 TTL 的已完成任务(供后台/测试触发)。
func (m *ExamJobManager) SweepExpired() {
	m.mgr.SweepExpired()
}

// RecoverInterrupted 重启恢复:体检为单发任务,重启时仍 running 的记录一律标记 interrupted。
// 由 main 在服务启动前调用(须在管理器构造之后,即 kind 已注册)。
func (m *ExamJobManager) RecoverInterrupted() error {
	return m.mgr.RecoverOwn()
}
