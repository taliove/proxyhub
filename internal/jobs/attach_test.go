package jobs

import (
	"encoding/json"
	"testing"
	"time"
)

// TestManager_AttachUnknownKeyErrors Attach 不存在的任务报错,不幻影启动。
func TestManager_AttachUnknownKeyErrors(t *testing.T) {
	k := newScriptedKind("exam")
	m := NewManager(nil)
	m.Register(k)

	if _, err := m.Attach("exam", "missing"); err == nil {
		t.Fatal("Attach on non-existent job should error")
	}
	if k.calls.Load() != 0 {
		t.Fatalf("Attach started a phantom job (calls=%d), want 0", k.calls.Load())
	}
}

// TestManager_AttachUnknownKindErrors Attach 未注册 kind 报错。
func TestManager_AttachUnknownKindErrors(t *testing.T) {
	m := NewManager(nil)
	if _, err := m.Attach("nope", "k"); err == nil {
		t.Fatal("Attach on unregistered kind should error")
	}
}

// TestManager_AttachRunningJob Atttach 进行中的任务:可收直播事件,不重复启动。
func TestManager_AttachRunningJob(t *testing.T) {
	k := newScriptedKind("exam")
	m := NewManager(nil)
	m.Register(k)

	s1 := mustOpen(t, m, "exam", "n")
	defer s1.Close()
	<-k.started

	s2, err := m.Attach("exam", "n")
	if err != nil {
		t.Fatalf("Attach running job: %v", err)
	}
	defer s2.Close()

	if k.calls.Load() != 1 {
		t.Fatalf("Attach re-started the job (calls=%d), want 1", k.calls.Load())
	}
}

// TestManager_AttachFinishedJobWithinTTL Attach TTL 内已收口任务:回放可得,不重跑。
func TestManager_AttachFinishedJobWithinTTL(t *testing.T) {
	k := newScriptedKind("exam")
	fc := &fakeClock{cur: time.Unix(1000, 0)}
	m := NewManager(nil, WithClock(fc.now), WithTTL(5*time.Minute))
	m.Register(k)

	s1 := mustOpen(t, m, "exam", "n")
	<-k.started
	k.events <- json.RawMessage(`{"phase":"done"}`)
	close(k.events)
	drain(s1)

	s2, err := m.Attach("exam", "n")
	if err != nil {
		t.Fatalf("Attach finished job within TTL: %v", err)
	}
	defer s2.Close()

	if len(s2.Replay) == 0 {
		t.Error("Attach replay empty, want buffered events of finished job")
	}
	if k.calls.Load() != 1 {
		t.Fatalf("Attach re-ran finished job (calls=%d), want 1", k.calls.Load())
	}
}

// TestManager_AttachExpiredJobErrors TTL 过期后任务已清扫,Attach 报错。
func TestManager_AttachExpiredJobErrors(t *testing.T) {
	k := newScriptedKind("exam")
	fc := &fakeClock{cur: time.Unix(1000, 0)}
	m := NewManager(nil, WithClock(fc.now), WithTTL(5*time.Minute))
	m.Register(k)

	s1 := mustOpen(t, m, "exam", "n")
	<-k.started
	close(k.events)
	drain(s1)

	fc.cur = fc.cur.Add(10 * time.Minute)
	if _, err := m.Attach("exam", "n"); err == nil {
		t.Fatal("Attach on swept job should error")
	}
	if k.calls.Load() != 1 {
		t.Fatalf("Attach re-started swept job (calls=%d), want 1", k.calls.Load())
	}
}
