package jobs_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/taliove/proxyhub/internal/jobs"
	"github.com/taliove/proxyhub/internal/store"
)

// testKind 可配置的假 kind:可注入发射的事件、进度游标、是否可续跑、是否阻塞。
type testKind struct {
	name      string
	resumable bool
	emit      json.RawMessage
	cursor    string        // 非空则在 Run 中调用 progress(cursor)
	gotCursor chan string   // Run 收到的续跑 cursor(缓冲 1)
	block     chan struct{} // 非 nil 则 Run 等待其关闭(或 ctx 取消)后返回
}

func newTestKind(name string, resumable bool) *testKind {
	return &testKind{name: name, resumable: resumable, gotCursor: make(chan string, 1)}
}

func (k *testKind) Name() string    { return k.name }
func (k *testKind) Resumable() bool { return k.resumable }

func (k *testKind) Run(ctx context.Context, _ json.RawMessage, cursor string, emit func(json.RawMessage), progress func(string)) error {
	if k.emit != nil {
		emit(k.emit)
	}
	if k.cursor != "" {
		progress(k.cursor)
	}
	select {
	case k.gotCursor <- cursor:
	default:
	}
	if k.block != nil {
		select {
		case <-k.block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.OpenForTesting(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// waitStatus 轮询等待某 job 记录到达期望状态。
func waitStatus(t *testing.T, js *jobs.Store, id int64, want jobs.Status) *jobs.Record {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		rec, err := js.Get(id)
		if err != nil {
			t.Fatalf("Get(%d): %v", id, err)
		}
		if rec != nil && rec.Status == want {
			return rec
		}
		if time.Now().After(deadline) {
			got := "<nil>"
			if rec != nil {
				got = string(rec.Status)
			}
			t.Fatalf("job %d status = %s, want %s (timeout)", id, got, want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestPersist_Lifecycle Open 落 running -> progress 更新 cursor -> 自然完成落 done。
func TestPersist_Lifecycle(t *testing.T) {
	st := openStore(t)
	js := st.Jobs()

	k := newTestKind("batch", true)
	k.emit = json.RawMessage(`{"phase":"item"}`)
	k.cursor = "1"
	k.block = make(chan struct{})

	m := jobs.NewManager(js)
	m.Register(k)

	sub, err := m.Open("batch", "job1", json.RawMessage(`{"total":3}`))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// 读到发射的事件即保证 Run 已推进(progress 已落 cursor)。
	<-sub.Live

	// 找到该行 id(唯一一条)。
	running, err := js.LoadRunning()
	if err != nil {
		t.Fatalf("LoadRunning: %v", err)
	}
	if len(running) != 1 {
		t.Fatalf("running rows = %d, want 1", len(running))
	}
	id := running[0].ID
	if running[0].Status != jobs.StatusRunning {
		t.Fatalf("status = %s, want running", running[0].Status)
	}
	if string(running[0].Params) != `{"total":3}` {
		t.Errorf("params = %s, want {\"total\":3}", running[0].Params)
	}

	close(k.block) // 放行 Run 自然完成
	for range sub.Live {
	}
	sub.Close()

	rec := waitStatus(t, js, id, jobs.StatusDone)
	if rec.Cursor != "1" {
		t.Errorf("cursor = %q, want 1", rec.Cursor)
	}
}

// TestPersist_FailedStatus Run 返回非取消错误 -> 落 failed。
func TestPersist_FailedStatus(t *testing.T) {
	st := openStore(t)
	js := st.Jobs()

	k := &failKind{name: "failer"}
	m := jobs.NewManager(js)
	m.Register(k)

	sub, err := m.Open("failer", "j", nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	for range sub.Live {
	}
	sub.Close()

	// 落终态在 finalize 关闭订阅之后异步发生,须轮询等待(id=1 为首条)。
	waitStatus(t, js, 1, jobs.StatusFailed)
}

var errBoom = errors.New("boom")

type failKind struct{ name string }

func (k *failKind) Name() string    { return k.name }
func (k *failKind) Resumable() bool { return false }
func (k *failKind) Run(_ context.Context, _ json.RawMessage, _ string, _ func(json.RawMessage), _ func(string)) error {
	return errBoom
}

// TestRecover_ResumableResumesFromCursor 重启:running 的可续跑任务从游标续跑并完成。
func TestRecover_ResumableResumesFromCursor(t *testing.T) {
	st := openStore(t)
	js := st.Jobs()

	// 直接种入一条"崩溃遗留"的 running 记录 + 游标(模拟上次进程中断)。
	id, err := js.Insert("batch", "job1", json.RawMessage(`{"total":10}`))
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if err := js.UpdateCursor(id, "3"); err != nil {
		t.Fatalf("UpdateCursor: %v", err)
	}

	k := newTestKind("batch", true) // 可续跑
	m := jobs.NewManager(js)
	m.Register(k)

	if err := m.Recover(); err != nil {
		t.Fatalf("Recover: %v", err)
	}

	select {
	case got := <-k.gotCursor:
		if got != "3" {
			t.Fatalf("resumed cursor = %q, want 3", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("resumable job was not resumed")
	}

	waitStatus(t, js, id, jobs.StatusDone)
}

// TestRecover_NonResumableMarksInterrupted 重启:running 的单发任务标记 interrupted,不续跑。
func TestRecover_NonResumableMarksInterrupted(t *testing.T) {
	st := openStore(t)
	js := st.Jobs()

	id, err := js.Insert("exam", "node1", nil)
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	k := newTestKind("exam", false) // 不可续跑
	m := jobs.NewManager(js)
	m.Register(k)

	if err := m.Recover(); err != nil {
		t.Fatalf("Recover: %v", err)
	}

	rec, err := js.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if rec.Status != jobs.StatusInterrupted {
		t.Errorf("status = %s, want interrupted", rec.Status)
	}
	select {
	case <-k.gotCursor:
		t.Fatal("non-resumable job must not be run on recover")
	case <-time.After(100 * time.Millisecond):
	}
}

// TestRecover_UnregisteredKindMarksInterrupted 重启:kind 未注册的 running 任务标记 interrupted。
func TestRecover_UnregisteredKindMarksInterrupted(t *testing.T) {
	st := openStore(t)
	js := st.Jobs()

	id, err := js.Insert("ghost", "k", nil)
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	m := jobs.NewManager(js) // 未注册任何 kind
	if err := m.Recover(); err != nil {
		t.Fatalf("Recover: %v", err)
	}

	rec, _ := js.Get(id)
	if rec.Status != jobs.StatusInterrupted {
		t.Errorf("status = %s, want interrupted", rec.Status)
	}
}
