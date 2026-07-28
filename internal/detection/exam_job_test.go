package detection

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/taliove/proxyhub/internal/subscription"
)

// scriptedExam 可脚本化的假体检 runner:测试通过 events 通道驱动 emit,
// close(events) 表示自然完成(返回 report),ctx 取消则返回 cancelReport。
type scriptedExam struct {
	calls        atomic.Int32
	started      chan struct{}
	events       chan ExamEvent
	report       ExamReport
	cancelReport ExamReport
}

func newScriptedExam() *scriptedExam {
	return &scriptedExam{
		started: make(chan struct{}, 1),
		events:  make(chan ExamEvent),
	}
}

func (s *scriptedExam) run(ctx context.Context, _ *subscription.Node, emit func(ExamEvent)) ExamReport {
	s.calls.Add(1)
	select {
	case s.started <- struct{}{}:
	default:
	}
	for {
		select {
		case <-ctx.Done():
			return s.cancelReport
		case e, ok := <-s.events:
			if !ok {
				return s.report
			}
			emit(e)
		}
	}
}

func examTestNode() *subscription.Node {
	return &subscription.Node{Name: "exam-node", Server: "example.com", Port: 443, Type: "vmess", Source: "airport"}
}

// drainExam 订阅并读到通道关闭为止。
func drainExam(sub *ExamSubscription) {
	for range sub.Live {
	}
	sub.Close()
}

func TestExamJobManager_SingleInstancePerNode(t *testing.T) {
	se := newScriptedExam()
	m := NewExamJobManager(se.run, nil)
	node := examTestNode()

	s1 := m.Open(node.NodeKey(), node)
	<-se.started

	// 让任务推送至少一帧,确保后续 Open 附加时 Replay 非空(标识已在跑)。
	se.events <- ExamEvent{Phase: "sample"}
	<-s1.Live

	s2 := m.Open(node.NodeKey(), node)
	if len(s2.Replay) == 0 {
		t.Fatal("second Open should attach (non-empty Replay)")
	}

	close(se.events)
	drainExam(s1)
	s2.Close()

	if got := se.calls.Load(); got != 1 {
		t.Fatalf("runner invoked %d times, want 1 (single instance per node)", got)
	}
}

func TestExamJobManager_AttachReplaysThenLive(t *testing.T) {
	se := newScriptedExam()
	m := NewExamJobManager(se.run, nil)
	node := examTestNode()

	s1 := m.Open(node.NodeKey(), node)
	<-se.started

	se.events <- ExamEvent{Phase: "sample", Section: "stability"}
	f1 := <-s1.Live

	s2 := m.Open(node.NodeKey(), node)
	if len(s2.Replay) != 1 {
		t.Fatalf("replay len = %d, want 1", len(s2.Replay))
	}
	if s2.Replay[0].Seq != f1.Seq {
		t.Fatalf("replay seq = %d, want %d", s2.Replay[0].Seq, f1.Seq)
	}

	se.events <- ExamEvent{Phase: "sample", Section: "stability"}
	f2a := <-s1.Live
	f2b := <-s2.Live
	if f2a.Seq != f2b.Seq {
		t.Fatalf("live seq mismatch: %d vs %d", f2a.Seq, f2b.Seq)
	}
	if f2b.Seq != f1.Seq+1 {
		t.Fatalf("seq not monotonic: f1=%d f2=%d", f1.Seq, f2b.Seq)
	}

	close(se.events)
	drainExam(s1)
	s2.Close()
}

func TestExamJobManager_CancelEmitsCancelledNoHistory(t *testing.T) {
	var mu sync.Mutex
	var saved []string
	onComplete := func(userID int64, key string, _ ExamReport) {
		mu.Lock()
		saved = append(saved, key)
		mu.Unlock()
	}

	se := newScriptedExam()
	// 即便取消时报告已含稳定性指标,取消也绝不落历史。
	se.cancelReport = ExamReport{Stability: &StabilityMetrics{Succeeded: 2}}
	m := NewExamJobManager(se.run, onComplete)
	node := examTestNode()

	s := m.Open(node.NodeKey(), node)
	<-se.started

	se.events <- ExamEvent{Phase: "sample", Section: "stability"}
	<-s.Live

	if !m.Cancel(node.NodeKey()) {
		t.Fatal("Cancel returned false for a running job")
	}

	var phases []string
	for f := range s.Live {
		phases = append(phases, f.Phase)
	}
	if len(phases) == 0 || phases[len(phases)-1] != examPhaseCancelled {
		t.Fatalf("phases = %v, want last = cancelled", phases)
	}
	s.Close()

	mu.Lock()
	defer mu.Unlock()
	if len(saved) != 0 {
		t.Fatalf("history saved on cancel: %v", saved)
	}
}

func TestExamJobManager_CompleteSavesHistory(t *testing.T) {
	var mu sync.Mutex
	var savedKey string
	var savedReport ExamReport
	completed := make(chan struct{}, 1)
	onComplete := func(userID int64, k string, r ExamReport) {
		mu.Lock()
		savedKey = k
		savedReport = r
		mu.Unlock()
		completed <- struct{}{}
	}

	se := newScriptedExam()
	se.report = ExamReport{Stability: &StabilityMetrics{Succeeded: 3}}
	m := NewExamJobManager(se.run, onComplete)
	node := examTestNode()

	s := m.Open(node.NodeKey(), node)
	<-se.started

	se.events <- ExamEvent{Phase: "done"}
	close(se.events)
	drainExam(s)

	// onComplete 由 jobs 管理器在 finalize 后经独立 goroutine 触发,与 SSE drain
	// 无先后保证;必须等回调到达再断言(旧实现直接断言,CI 高负载下 flake)。
	select {
	case <-completed:
	case <-time.After(5 * time.Second):
		t.Fatal("onComplete not called within 5s after job completion")
	}

	mu.Lock()
	defer mu.Unlock()
	if savedKey != node.NodeKey() {
		t.Fatalf("saved key = %q, want %q", savedKey, node.NodeKey())
	}
	if savedReport.Stability == nil || savedReport.Stability.Succeeded != 3 {
		t.Fatalf("saved report = %+v, want stability succeeded=3", savedReport)
	}
}

func TestExamJobManager_FailedNoHistory(t *testing.T) {
	var saved atomic.Int32
	onComplete := func(userID int64, _ string, _ ExamReport) { saved.Add(1) }

	se := newScriptedExam()
	se.report = ExamReport{} // Stability 为 nil,视为失败(建会话失败等)
	m := NewExamJobManager(se.run, onComplete)
	node := examTestNode()

	s := m.Open(node.NodeKey(), node)
	<-se.started

	se.events <- ExamEvent{Phase: "error", Error: "create proxy session: boom"}
	close(se.events)
	drainExam(s)

	if got := saved.Load(); got != 0 {
		t.Fatalf("history saved %d times on failure, want 0", got)
	}
}

// TestExamJobManager_OpenStoresNodeBeforeRun 钉死根因回归:活节点必须在 Run 之前就位。
// 历史缺陷:open 在 mgr.Open(已启动 Run goroutine)之后才 Store 节点,Run 的
// LoadAndDelete 偶发抢先命中空缺 -> 返回 "no live node" -> OnComplete 收非 examResult ->
// 不落历史 -> 上层 waitExamHistory 超时。修复后 OnStart 在锁内、Run 启动前原子晋升节点,
// 连开多轮不应再丢节点(onComplete 每轮必达,onErr 从不因缺节点触发)。
func TestExamJobManager_OpenStoresNodeBeforeRun(t *testing.T) {
	var completed atomic.Int32
	var errs atomic.Int32
	onComplete := func(userID int64, _ string, _ ExamReport) { completed.Add(1) }
	onErr := func(error) { errs.Add(1) }

	// runner 立即自然完成(带稳定性段 -> 应落历史),放大 open/Run 的时序竞争窗口。
	run := func(_ context.Context, _ *subscription.Node, emit func(ExamEvent)) ExamReport {
		emit(ExamEvent{Phase: "done"})
		return ExamReport{Stability: &StabilityMetrics{Succeeded: 1}}
	}

	const rounds = 300
	for i := 0; i < rounds; i++ {
		m := NewExamJobManager(run, onComplete, WithExamErrorHandler(onErr))
		node := examTestNode()
		s := m.Open(node.NodeKey(), node)
		drainExam(s)
	}

	if got := errs.Load(); got != 0 {
		t.Fatalf("onErr fired %d times (live node lost to Run/Store race)", got)
	}
	if got := completed.Load(); got != rounds {
		t.Fatalf("onComplete fired %d times, want %d (every run must persist)", got, rounds)
	}
}

func TestExamJobManager_OpenForceRestartsFinished(t *testing.T) {
	se := newScriptedExam()
	m := NewExamJobManager(se.run, nil)
	node := examTestNode()

	// 进行中:force 不打断,附加同实例。
	s1 := m.OpenForce(node.NodeKey(), node)
	<-se.started
	se.events <- ExamEvent{Phase: "sample"}
	<-s1.Live
	s1b := m.OpenForce(node.NodeKey(), node)
	if len(s1b.Replay) == 0 {
		t.Fatal("force on running job should attach (non-empty Replay)")
	}
	s1b.Close()
	close(se.events)
	drainExam(s1)

	// 已收口:force 丢弃旧任务起新任务。
	s2 := m.OpenForce(node.NodeKey(), node)
	drainExam(s2)
	if got := se.calls.Load(); got != 2 {
		t.Fatalf("runner invoked %d times, want 2 (force restart finished)", got)
	}
}
