package jobs

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeClock 虚拟时钟:now 返回当前时刻,推进由测试显式控制。
type fakeClock struct{ cur time.Time }

func (c *fakeClock) now() time.Time { return c.cur }

// scriptedKind 可脚本化的假任务 kind:测试通过 events 通道驱动 emit,
// close(events) 表示自然完成(返回 runErr,默认 nil=done);ctx 取消返回 ctx.Err()。
type scriptedKind struct {
	name      string
	resumable bool
	calls     atomic.Int32
	started   chan struct{}
	events    chan json.RawMessage
	runErr    error
	// resumeCursors 记录每次 Run 收到的 cursor(断点续跑断言)。
	mu            sync.Mutex
	resumeCursors []string
}

func newScriptedKind(name string) *scriptedKind {
	return &scriptedKind{
		name:    name,
		started: make(chan struct{}, 1),
		events:  make(chan json.RawMessage),
	}
}

func (k *scriptedKind) Name() string    { return k.name }
func (k *scriptedKind) Resumable() bool { return k.resumable }

func (k *scriptedKind) Run(ctx context.Context, _ json.RawMessage, cursor string, emit func(json.RawMessage), progress func(string)) error {
	k.calls.Add(1)
	k.mu.Lock()
	k.resumeCursors = append(k.resumeCursors, cursor)
	k.mu.Unlock()
	select {
	case k.started <- struct{}{}:
	default:
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case e, ok := <-k.events:
			if !ok {
				return k.runErr
			}
			emit(e)
		}
	}
}

func (k *scriptedKind) CancelEvent() (json.RawMessage, bool) {
	return json.RawMessage(`{"phase":"cancelled"}`), true
}

// drain 订阅并读到通道关闭为止,用于等待任务 finalize 完成。
func drain(sub *Subscription) {
	for range sub.Live {
	}
	sub.Close()
}

func mustOpen(t *testing.T, m *Manager, kind, key string) *Subscription {
	t.Helper()
	sub, err := m.Open(kind, key, nil)
	if err != nil {
		t.Fatalf("Open(%s,%s): %v", kind, key, err)
	}
	return sub
}

func TestManager_UnknownKindError(t *testing.T) {
	m := NewManager(nil)
	if _, err := m.Open("nope", "k", nil); err == nil {
		t.Fatal("Open on unregistered kind should error")
	}
}

func TestManager_DuplicateRegisterPanics(t *testing.T) {
	m := NewManager(nil)
	m.Register(newScriptedKind("exam"))
	defer func() {
		if recover() == nil {
			t.Fatal("duplicate Register should panic")
		}
	}()
	m.Register(newScriptedKind("exam"))
}

func TestManager_SingleInstancePerKindKey(t *testing.T) {
	k := newScriptedKind("exam")
	m := NewManager(nil)
	m.Register(k)

	s1 := mustOpen(t, m, "exam", "node1")
	<-k.started
	s2 := mustOpen(t, m, "exam", "node1")

	close(k.events)
	drain(s1)
	s2.Close()

	if got := k.calls.Load(); got != 1 {
		t.Fatalf("runner invoked %d times, want 1 (single instance per kind+key)", got)
	}
}

func TestManager_DifferentKeysConcurrent(t *testing.T) {
	k := newScriptedKind("exam")
	m := NewManager(nil)
	m.Register(k)

	s1 := mustOpen(t, m, "exam", "node1")
	<-k.started
	s2 := mustOpen(t, m, "exam", "node2")
	<-k.started

	close(k.events)
	drain(s1)
	drain(s2)

	if got := k.calls.Load(); got != 2 {
		t.Fatalf("runner invoked %d times, want 2 (one per key)", got)
	}
}

func TestManager_AttachReplaysThenLive(t *testing.T) {
	k := newScriptedKind("exam")
	m := NewManager(nil)
	m.Register(k)

	s1 := mustOpen(t, m, "exam", "n")
	<-k.started

	k.events <- json.RawMessage(`{"phase":"sample"}`)
	f1 := <-s1.Live

	// 迟到订阅者:replay 含 f1,不重复经 live 再收。
	s2 := mustOpen(t, m, "exam", "n")
	if len(s2.Replay) != 1 {
		t.Fatalf("replay len = %d, want 1", len(s2.Replay))
	}
	if s2.Replay[0].Seq != f1.Seq {
		t.Fatalf("replay seq = %d, want %d", s2.Replay[0].Seq, f1.Seq)
	}

	k.events <- json.RawMessage(`{"phase":"sample"}`)
	f2a := <-s1.Live
	f2b := <-s2.Live
	if f2a.Seq != f2b.Seq {
		t.Fatalf("live seq mismatch: %d vs %d", f2a.Seq, f2b.Seq)
	}
	if f2b.Seq != f1.Seq+1 {
		t.Fatalf("seq not monotonic: f1=%d f2=%d", f1.Seq, f2b.Seq)
	}

	close(k.events)
	drain(s1)
	s2.Close()
}

func TestManager_RingBufferCap(t *testing.T) {
	k := newScriptedKind("exam")
	m := NewManager(nil, WithBufferCap(8))
	m.Register(k)

	s := mustOpen(t, m, "exam", "n")
	<-k.started

	total := 8 + 5
	for i := 0; i < total; i++ {
		k.events <- json.RawMessage(`{"phase":"sample"}`)
		<-s.Live
	}

	s2 := mustOpen(t, m, "exam", "n")
	if len(s2.Replay) != 8 {
		t.Fatalf("replay len = %d, want cap 8", len(s2.Replay))
	}
	if s2.Replay[0].Seq != total-8 {
		t.Fatalf("oldest retained seq = %d, want %d", s2.Replay[0].Seq, total-8)
	}
	if s2.Replay[len(s2.Replay)-1].Seq != total-1 {
		t.Fatalf("newest retained seq = %d, want %d", s2.Replay[len(s2.Replay)-1].Seq, total-1)
	}

	close(k.events)
	drain(s)
	s2.Close()
}

func TestManager_CancelEmitsCancelledFrame(t *testing.T) {
	k := newScriptedKind("exam")
	m := NewManager(nil)
	m.Register(k)

	s := mustOpen(t, m, "exam", "n")
	<-k.started

	k.events <- json.RawMessage(`{"phase":"sample"}`)
	<-s.Live

	if !m.Cancel("exam", "n") {
		t.Fatal("Cancel returned false for a running job")
	}

	var phases []string
	for f := range s.Live {
		var e struct {
			Phase string `json:"phase"`
		}
		_ = json.Unmarshal(f.Data, &e)
		phases = append(phases, e.Phase)
	}
	if len(phases) == 0 || phases[len(phases)-1] != "cancelled" {
		t.Fatalf("phases = %v, want last = cancelled", phases)
	}
	s.Close()
}

func TestManager_CancelUnknownReturnsFalse(t *testing.T) {
	m := NewManager(nil)
	m.Register(newScriptedKind("exam"))
	if m.Cancel("exam", "missing") {
		t.Fatal("Cancel on unknown key should return false")
	}
}

func TestManager_TTLExpiry(t *testing.T) {
	k := newScriptedKind("exam")
	fc := &fakeClock{cur: time.Unix(1000, 0)}
	m := NewManager(nil, WithClock(fc.now), WithTTL(5*time.Minute))
	m.Register(k)

	s1 := mustOpen(t, m, "exam", "n")
	<-k.started
	close(k.events)
	drain(s1)

	// TTL 内:附加到已完成任务(同实例,可回放),runner 不再调用。
	s1b := mustOpen(t, m, "exam", "n")
	s1b.Close()
	if got := k.calls.Load(); got != 1 {
		t.Fatalf("within TTL calls = %d, want 1 (attach to finished)", got)
	}

	fc.cur = fc.cur.Add(5*time.Minute + time.Second)
	m.SweepExpired()

	s2 := mustOpen(t, m, "exam", "n")
	drain(s2)
	if got := k.calls.Load(); got != 2 {
		t.Fatalf("after TTL calls = %d, want 2 (fresh job)", got)
	}
}

func TestManager_OpenForceRestartsFinished(t *testing.T) {
	k := newScriptedKind("exam")
	m := NewManager(nil)
	m.Register(k)

	// 进行中:force 不打断,附加同实例。
	s1, err := m.OpenForce("exam", "n", nil)
	if err != nil {
		t.Fatal(err)
	}
	<-k.started
	s1b, _ := m.OpenForce("exam", "n", nil)
	s1b.Close()
	close(k.events)
	drain(s1)

	// 已收口:force 丢弃旧任务起新任务。
	s2, _ := m.OpenForce("exam", "n", nil)
	drain(s2)
	if got := k.calls.Load(); got != 2 {
		t.Fatalf("force calls = %d, want 2 (restart finished)", got)
	}
}

func TestManager_ConcurrentSubscribersRace(t *testing.T) {
	k := newScriptedKind("exam")
	m := NewManager(nil)
	m.Register(k)

	s := mustOpen(t, m, "exam", "n")
	<-k.started

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 300; i++ {
			k.events <- json.RawMessage(`{"phase":"sample"}`)
		}
		close(k.events)
	}()

	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for r := 0; r < 25; r++ {
				sub, err := m.Open("exam", "n", nil)
				if err != nil {
					return
				}
				done := make(chan struct{})
				go func() {
					for range sub.Live {
					}
					close(done)
				}()
				time.Sleep(time.Millisecond)
				sub.Close()
				<-done
			}
		}()
	}

	wg.Wait()
	drain(s)
}
