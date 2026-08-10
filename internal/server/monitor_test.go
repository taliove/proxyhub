package server

import (
	"testing"

	"github.com/taliove/proxyhub/internal/subscription"
)

// TestMonitorTargets 监控集合口径(issue #99):
// 可用性被排除(宕机节点仍在监控中),stale/屏蔽/禁用地址不在集合内
func TestMonitorTargets(t *testing.T) {
	up := &subscription.Node{
		Name: "好节点", Server: "up.example.com", Port: 443, Source: "机场A", Available: true,
	}
	down := &subscription.Node{
		Name: "宕节点", Server: "down.example.com", Port: 443, Source: "机场A", Available: false,
	}
	stale := &subscription.Node{
		Name: "消失节点", Server: "gone.example.com", Port: 443, Source: "机场A", Stale: true,
	}
	srv, st := newTestServer(t, []*subscription.Node{up, down, stale})

	// 属主 0(未归属桶):一个启用地址 + 一个禁用地址
	if _, err := st.CreateEndpointForUser(0, "主订阅"); err != nil {
		t.Fatal(err)
	}
	ep2, err := st.CreateEndpointForUser(0, "停用订阅")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetEndpointEnabled(ep2.ID, false); err != nil {
		t.Fatal(err)
	}

	targets := srv.MonitorTargets()
	keys := map[string]bool{}
	for _, tg := range targets {
		keys[tg.NodeKey] = true
	}
	if !keys[up.NodeKey()] {
		t.Error("available node should be monitored")
	}
	if !keys[down.NodeKey()] {
		t.Error("unavailable node must stay monitored (否则等不到恢复探测)")
	}
	if keys[stale.NodeKey()] {
		t.Error("stale node should not be monitored")
	}

	// 屏蔽后不监控
	if err := st.BlockNodeForUser(0, up.NodeKey()); err != nil {
		t.Fatal(err)
	}
	targets = srv.MonitorTargets()
	for _, tg := range targets {
		if tg.NodeKey == up.NodeKey() {
			t.Error("blocked node should not be monitored")
		}
	}
}

// TestMonitorImmuneDelivery 宕机免疫(issue #101):监控开启时,监控集合内
// 确认死亡的节点仍下发;集合外(关键词黑名单)的死节点照旧过滤;监控关闭零回归
func TestMonitorImmuneDelivery(t *testing.T) {
	dead := &subscription.Node{
		Name: "好名但宕了", Server: "dead.example.com", Port: 443, Source: "机场A",
		Available: false, DetectionKind: subscription.DetectionKindHealth, Latency: 500,
	}
	excluded := &subscription.Node{
		Name: "垃圾节点", Server: "junk.example.com", Port: 443, Source: "机场A",
		Available: false, DetectionKind: subscription.DetectionKindHealth,
	}
	srv, st := newTestServer(t, []*subscription.Node{dead, excluded})
	ep, err := st.CreateEndpointForUser(0, "主订阅")
	if err != nil {
		t.Fatal(err)
	}
	// 关键词黑名单把 excluded 挤出下发/监控集合
	if err := st.SaveSystemSettings(map[string]string{"filter_keywords": "垃圾"}); err != nil {
		t.Fatal(err)
	}

	contains := func(nodes []*subscription.Node, key string) bool {
		for _, n := range nodes {
			if n.NodeKey() == key {
				return true
			}
		}
		return false
	}

	// 监控关闭(默认):死节点不下发(零回归)
	if got := srv.endpointDeliverableNodes(ep); contains(got, dead.NodeKey()) {
		t.Error("monitor off: dead node should be filtered (zero regression)")
	}

	// 监控开启:集合内死节点仍下发;集合外死节点仍过滤
	if err := st.SaveSystemSettings(map[string]string{
		"filter_keywords": "垃圾", "subscription_monitor_enabled": "true",
	}); err != nil {
		t.Fatal(err)
	}
	got := srv.endpointDeliverableNodes(ep)
	if !contains(got, dead.NodeKey()) {
		t.Error("monitor on: immune dead node should still be delivered")
	}
	if contains(got, excluded.NodeKey()) {
		t.Error("monitor on: non-immune dead node should stay filtered")
	}
}

// TestMonitorTargetsRespectPicks 精选地址只监控精选节点
func TestMonitorTargetsRespectPicks(t *testing.T) {
	a := &subscription.Node{Name: "A", Server: "a.example.com", Port: 443, Source: "机场A", Available: true}
	b := &subscription.Node{Name: "B", Server: "b.example.com", Port: 443, Source: "机场A", Available: true}
	srv, st := newTestServer(t, []*subscription.Node{a, b})

	ep, err := st.CreateEndpointForUser(0, "精选订阅")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateEndpointNodePicksForUser(0, ep.ID, `[{"key":"`+a.NodeKey()+`"}]`); err != nil {
		t.Fatal(err)
	}

	targets := srv.MonitorTargets()
	if len(targets) != 1 || targets[0].NodeKey != a.NodeKey() {
		t.Errorf("targets = %v, want only picked node", targets)
	}
}
