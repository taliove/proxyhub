package nodemon

import (
	"strings"
	"sync"
	"testing"
)

type fakeSlots struct{ m map[int64]map[string]string }

func (f fakeSlots) SlotNameByNodeKeyForUser(userID int64) (map[string]string, error) {
	return f.m[userID], nil
}

type fakeWriter struct {
	mu     sync.Mutex
	writes []writeRec
}

type writeRec struct {
	userID    int64
	nodeKey   string
	available bool
	latency   int
}

func (f *fakeWriter) UpdateNodeTestResultForUser(userID int64, nodeKey, mode string, available bool, latency int, downMbps, upMbps float64, failReason, failDetail string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes = append(f.writes, writeRec{userID, nodeKey, available, latency})
	return true
}

func result(userID int64, key string, ok bool) (Target, Sample) {
	return Target{UserID: userID, NodeKey: key}, Sample{NodeKey: key, OK: ok, LatencyMs: 66}
}

func TestStateMachineDownThenRecover(t *testing.T) {
	slots := fakeSlots{m: map[int64]map[string]string{1: {"hk.example.com:443": "🇭🇰 香港01"}}}
	w := &fakeWriter{}
	al := &fakeAlerter{}
	sm := NewStateMachine(slots, w, al, testLogger())

	// 抖动(1~2 次失败):无告警无写回
	sm.OnProbeResult(result(1, "hk.example.com:443", false))
	sm.OnProbeResult(result(1, "hk.example.com:443", false))
	sm.OnProbeResult(result(1, "hk.example.com:443", true))
	if len(al.alerts) != 0 || len(w.writes) != 0 {
		t.Fatal("flap should not alert or write back")
	}

	// 连续 3 败判宕:一次告警 + 写回 false,槽位名进文案
	for i := 0; i < 3; i++ {
		sm.OnProbeResult(result(1, "hk.example.com:443", false))
	}
	if len(al.alerts) != 1 || !strings.Contains(al.alerts[0], "🇭🇰 香港01") {
		t.Fatalf("down alerts = %v, want 1 with slot name", al.alerts)
	}
	if len(w.writes) != 1 || w.writes[0].available {
		t.Fatalf("writes = %+v, want 1 writeback available=false", w.writes)
	}

	// 宕中继续失败:不重复告警
	sm.OnProbeResult(result(1, "hk.example.com:443", false))
	if len(al.alerts) != 1 {
		t.Fatal("repeat failure while down should not re-alert")
	}

	// 宕后 1 次成功:不恢复(阈值 2)
	sm.OnProbeResult(result(1, "hk.example.com:443", true))
	if len(al.alerts) != 1 {
		t.Fatal("single success should not recover")
	}
	// 第 2 次成功:恢复通知 + 写回 true(带延迟)
	sm.OnProbeResult(result(1, "hk.example.com:443", true))
	if len(al.alerts) != 2 || !strings.Contains(al.alerts[1], "恢复") {
		t.Fatalf("alerts = %v, want recovery notice", al.alerts)
	}
	last := w.writes[len(w.writes)-1]
	if !last.available || last.latency != 66 {
		t.Errorf("recovery writeback = %+v, want available=true latency=66", last)
	}

	// 恢复后再宕:重新计数 3 次才告警
	for i := 0; i < 2; i++ {
		sm.OnProbeResult(result(1, "hk.example.com:443", false))
	}
	if len(al.alerts) != 2 {
		t.Fatal("re-fail below threshold should not alert")
	}
	sm.OnProbeResult(result(1, "hk.example.com:443", false))
	if len(al.alerts) != 3 {
		t.Fatal("re-fail at threshold should alert again")
	}
}

func TestStateMachinePerUserIsolation(t *testing.T) {
	w := &fakeWriter{}
	al := &fakeAlerter{}
	sm := NewStateMachine(fakeSlots{m: map[int64]map[string]string{}}, w, al, testLogger())

	// 同一物理节点,用户 1 判宕不影响用户 2 的计数
	for i := 0; i < 3; i++ {
		sm.OnProbeResult(result(1, "shared.example.com:443", false))
	}
	sm.OnProbeResult(result(2, "shared.example.com:443", false))
	if len(al.alerts) != 1 {
		t.Fatalf("alerts = %d, want 1 (user2 only 1 fail)", len(al.alerts))
	}
	for _, wr := range w.writes {
		if wr.userID != 1 {
			t.Errorf("writeback user = %d, want 1", wr.userID)
		}
	}
}
