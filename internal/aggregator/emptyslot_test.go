package aggregator

import (
	"strings"
	"testing"

	"github.com/taliove/proxyhub/internal/subscription"
)

type fakeNotifier struct{ alerts []string }

func (f *fakeNotifier) AlertAirportDown(string, int) error  { return nil }
func (f *fakeNotifier) AlertLowAvailability(int, int) error { return nil }
func (f *fakeNotifier) Alert(title, content string) error {
	f.alerts = append(f.alerts, title+": "+content)
	return nil
}

// TestAlertEmptySlots 空槽告警(issue #100):槽位节点消失/stale → 告警一次;
// 回归 → 恢复通知;预建空槽不告警
func TestAlertEmptySlots(t *testing.T) {
	agg, st := newTestAggregator(t)
	nt := &fakeNotifier{}
	agg.alerter = nt

	gone := &subscription.Node{Name: "旧节点", Server: "gone.example.com", Port: 443, Source: "机场A"}
	if err := st.CreateNameSlotForUser(0, "🇭🇰 香港01", gone.NodeKey(), false); err != nil {
		t.Fatal(err)
	}
	// 预建空槽:不告警
	if err := st.CreateNameSlotForUser(0, "预建名", "", false); err != nil {
		t.Fatal(err)
	}

	// 节点不在池中 → 告警一次
	agg.alertEmptySlots(0, nil)
	if len(nt.alerts) != 1 || !strings.Contains(nt.alerts[0], "🇭🇰 香港01") {
		t.Fatalf("alerts = %v, want 1 empty-slot alert", nt.alerts)
	}
	// 重复刷新:不重复告警
	agg.alertEmptySlots(0, nil)
	if len(nt.alerts) != 1 {
		t.Fatal("should not re-alert while still empty")
	}

	// 节点回归(同 key,非 stale)→ 恢复通知,冷却复位
	back := &subscription.Node{Name: "旧节点", Server: "gone.example.com", Port: 443, Source: "机场A"}
	agg.alertEmptySlots(0, []*subscription.Node{back})
	if len(nt.alerts) != 2 || !strings.Contains(nt.alerts[1], "已恢复") {
		t.Fatalf("alerts = %v, want recovery notice", nt.alerts)
	}
	// 再次消失:重新告警(冷却已复位)
	agg.alertEmptySlots(0, nil)
	if len(nt.alerts) != 3 {
		t.Fatal("re-gone after recovery should alert again")
	}
}

// TestAlertEmptySlotsStale 池中存在但被标 stale 同样算消失
func TestAlertEmptySlotsStale(t *testing.T) {
	agg, st := newTestAggregator(t)
	nt := &fakeNotifier{}
	agg.alerter = nt

	n := &subscription.Node{Name: "节点", Server: "x.example.com", Port: 443, Source: "机场A", Stale: true}
	if err := st.CreateNameSlotForUser(0, "名", n.NodeKey(), false); err != nil {
		t.Fatal(err)
	}
	agg.alertEmptySlots(0, []*subscription.Node{n})
	if len(nt.alerts) != 1 {
		t.Fatalf("stale node should trigger empty-slot alert, alerts = %v", nt.alerts)
	}
}
