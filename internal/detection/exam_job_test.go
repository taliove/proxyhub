package detection

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/taliove/proxyhub/internal/subscription"
)

// scriptedExam 是一个可脚本化的假体检 runner:测试通过 events 通道驱动 emit,
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

// drainJob 订阅并读到通道关闭为止,用于等待任务 finalize 完成。
func drainJob(j *examJob) {
	_, live, unsub := j.subscribe()
	defer unsub()
	for range live {
	}
}

func TestExamJobManager_SingleInstancePerNode(t *testing.T) {
	se := newScriptedExam()
	m := NewExamJobManager(se.run, nil)
	node := examTestNode()

	j1 := m.startOrAttach(node.NodeKey(), node, false)
	<-se.started
	j2 := m.startOrAttach(node.NodeKey(), node, false)
	if j1 != j2 {
		t.Fatal("StartOrAttach on same node should attach to the same job")
	}

	close(se.events) // 自然完成
	drainJob(j1)

	if got := se.calls.Load(); got != 1 {
		t.Fatalf("runner invoked %d times, want 1 (single instance per node)", got)
	}
}

func TestExamJob_AttachReplaysThenLive(t *testing.T) {
	se := newScriptedExam()
	m := NewExamJobManager(se.run, nil)
	node := examTestNode()

	j := m.startOrAttach(node.NodeKey(), node, false)
	<-se.started

	_, live1, unsub1 := j.subscribe()
	defer unsub1()

	se.events <- ExamEvent{Phase: "sample", Section: "stability"}
	f1 := <-live1 // 读到即保证 emit 已入缓冲

	// 迟到订阅者:replay 必须包含 f1,且不重复通过 live 再收一遍。
	replay2, live2, unsub2 := j.subscribe()
	defer unsub2()
	if len(replay2) != 1 {
		t.Fatalf("replay len = %d, want 1", len(replay2))
	}
	if replay2[0].Seq != f1.Seq {
		t.Fatalf("replay seq = %d, want %d", replay2[0].Seq, f1.Seq)
	}

	se.events <- ExamEvent{Phase: "section_done", Section: "stability"}
	f2a := <-live1
	f2b := <-live2
	if f2a.Seq != f2b.Seq {
		t.Fatalf("live seq mismatch across subscribers: %d vs %d", f2a.Seq, f2b.Seq)
	}
	if f2b.Seq != f1.Seq+1 {
		t.Fatalf("seq not monotonic: f1=%d f2=%d", f1.Seq, f2b.Seq)
	}

	close(se.events)
	drainJob(j)
}

func TestExamJob_RingBufferCap(t *testing.T) {
	se := newScriptedExam()
	m := NewExamJobManager(se.run, nil)
	node := examTestNode()

	j := m.startOrAttach(node.NodeKey(), node, false)
	<-se.started

	_, live, unsub := j.subscribe()
	defer unsub()

	total := examBufferCap + 50
	for i := 0; i < total; i++ {
		se.events <- ExamEvent{Phase: "sample", Section: "stability"}
		<-live // 逐帧读走,确保每次 emit 已处理
	}

	replay, _, unsub2 := j.subscribe()
	defer unsub2()
	if len(replay) != examBufferCap {
		t.Fatalf("replay len = %d, want cap %d", len(replay), examBufferCap)
	}
	if replay[0].Seq != total-examBufferCap {
		t.Fatalf("oldest retained seq = %d, want %d", replay[0].Seq, total-examBufferCap)
	}
	if replay[len(replay)-1].Seq != total-1 {
		t.Fatalf("newest retained seq = %d, want %d", replay[len(replay)-1].Seq, total-1)
	}

	close(se.events)
	drainJob(j)
}

func TestExamJobManager_CancelEmitsCancelledNoHistory(t *testing.T) {
	var mu sync.Mutex
	var saved []string
	onComplete := func(key string, _ ExamReport) {
		mu.Lock()
		saved = append(saved, key)
		mu.Unlock()
	}

	se := newScriptedExam()
	// 即便取消时报告已含稳定性指标,取消也绝不落历史。
	se.cancelReport = ExamReport{Stability: &StabilityMetrics{Succeeded: 2}}
	m := NewExamJobManager(se.run, onComplete)
	node := examTestNode()

	j := m.startOrAttach(node.NodeKey(), node, false)
	<-se.started

	_, live, unsub := j.subscribe()
	defer unsub()

	se.events <- ExamEvent{Phase: "sample", Section: "stability"}
	<-live

	if !m.Cancel(node.NodeKey()) {
		t.Fatal("Cancel returned false for a running job")
	}

	var phases []string
	for f := range live {
		phases = append(phases, f.Phase)
	}
	if len(phases) == 0 || phases[len(phases)-1] != examPhaseCancelled {
		t.Fatalf("phases = %v, want last = %q", phases, examPhaseCancelled)
	}

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
	onComplete := func(k string, r ExamReport) {
		mu.Lock()
		savedKey = k
		savedReport = r
		mu.Unlock()
	}

	se := newScriptedExam()
	se.report = ExamReport{Stability: &StabilityMetrics{Succeeded: 3}}
	m := NewExamJobManager(se.run, onComplete)
	node := examTestNode()

	j := m.startOrAttach(node.NodeKey(), node, false)
	<-se.started

	se.events <- ExamEvent{Phase: "done"}
	close(se.events)
	drainJob(j)

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
	onComplete := func(string, ExamReport) { saved.Add(1) }

	se := newScriptedExam()
	se.report = ExamReport{} // Stability 为 nil,视为失败(建会话失败等)
	m := NewExamJobManager(se.run, onComplete)
	node := examTestNode()

	j := m.startOrAttach(node.NodeKey(), node, false)
	<-se.started

	se.events <- ExamEvent{Phase: "error", Error: "create proxy session: boom"}
	close(se.events)
	drainJob(j)

	if got := saved.Load(); got != 0 {
		t.Fatalf("history saved %d times on failure, want 0", got)
	}
}

func TestExamJobManager_TTLExpiry(t *testing.T) {
	se := newScriptedExam()
	m := NewExamJobManager(se.run, nil)
	fc := &fakeClock{cur: time.Unix(1000, 0)}
	m.now = fc.now

	node := examTestNode()
	j1 := m.startOrAttach(node.NodeKey(), node, false)
	<-se.started
	close(se.events) // 完成后返回(events 已关,后续 run 也会立即返回)
	drainJob(j1)

	// TTL 内:附加到已完成的任务(同实例,可回放)。
	j1b := m.startOrAttach(node.NodeKey(), node, false)
	if j1b != j1 {
		t.Fatal("within TTL StartOrAttach should attach to the finished job")
	}

	// 推进超过 TTL 后清理,再启动应是新任务。
	fc.cur = fc.cur.Add(examResultTTL + time.Second)
	m.SweepExpired()

	j2 := m.startOrAttach(node.NodeKey(), node, false)
	if j2 == j1 {
		t.Fatal("after TTL StartOrAttach should start a fresh job")
	}
	drainJob(j2)
	if got := se.calls.Load(); got != 2 {
		t.Fatalf("runner invoked %d times, want 2 (fresh job after TTL)", got)
	}
}

// TestExamJobManager_OpenForce force=重新体检:已收口的旧任务丢弃重开(即使 TTL 未过);
// 进行中的任务不受 force 影响,仍附加到原任务。
func TestExamJobManager_OpenForce(t *testing.T) {
	se := newScriptedExam()
	m := NewExamJobManager(se.run, nil)
	node := examTestNode()

	// 进行中的任务:force 不打断,附加到同一实例。
	j1 := m.startOrAttach(node.NodeKey(), node, true)
	<-se.started
	j1b := m.startOrAttach(node.NodeKey(), node, true)
	if j1b != j1 {
		t.Fatal("force on a running job should attach, not restart")
	}

	close(se.events) // 自然完成
	drainJob(j1)

	// 已收口:force 丢弃旧任务起新任务(对比:非 force 在 TTL 内附加旧任务,见 TTLExpiry)。
	j2 := m.startOrAttach(node.NodeKey(), node, true)
	if j2 == j1 {
		t.Fatal("force on a finished job should start a fresh job")
	}
	drainJob(j2)
	if got := se.calls.Load(); got != 2 {
		t.Fatalf("runner invoked %d times, want 2 (force restarted finished job)", got)
	}
}

func TestExamJob_ConcurrentSubscribersRace(t *testing.T) {
	se := newScriptedExam()
	m := NewExamJobManager(se.run, nil)
	node := examTestNode()

	j := m.startOrAttach(node.NodeKey(), node, false)
	<-se.started

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 300; i++ {
			se.events <- ExamEvent{Phase: "sample", Section: "stability"}
		}
		close(se.events)
	}()

	for k := 0; k < 8; k++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for r := 0; r < 25; r++ {
				_, live, unsub := j.subscribe()
				done := make(chan struct{})
				go func() {
					for range live {
					}
					close(done)
				}()
				time.Sleep(time.Millisecond)
				unsub()
				<-done
			}
		}()
	}

	wg.Wait()
	drainJob(j)
}
