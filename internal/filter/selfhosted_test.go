package filter

import (
	"testing"

	"github.com/taliove/proxyhub/internal/subscription"
)

// 自建节点是「FailBack 常驻安全网」：即使不可用、高延迟、超出每地区上限，
// 也必须始终保留在订阅里（见计划 sleepy-tickling-brook）。以下用例逐条锁死这一豁免。

func selfHosted(name string) *subscription.Node {
	return &subscription.Node{Name: name, Region: "SELF", Source: subscription.SourceSelfHosted}
}

func containsNode(nodes []*subscription.Node, name string) bool {
	for _, n := range nodes {
		if n.Name == name {
			return true
		}
	}
	return false
}

func TestFilterAvailable_ExemptsSelfHosted(t *testing.T) {
	down := selfHosted("self-down")
	down.Available = false // 检测为不可用
	nodes := []*subscription.Node{
		{Name: "airport-up", Available: true},
		{Name: "airport-down", Available: false, DetectionKind: subscription.DetectionKindHealth}, // 已确认死亡
		down,
	}

	result := FilterAvailable(nodes)

	if containsNode(result, "airport-down") {
		t.Error("unavailable airport node should be filtered")
	}
	if !containsNode(result, "self-down") {
		t.Error("unavailable self-hosted node must be retained (failback safety net)")
	}
}

func TestFilterByLatencyThreshold_ExemptsSelfHosted(t *testing.T) {
	slow := selfHosted("self-slow")
	slow.Latency = 5000 // 远超阈值
	nodes := []*subscription.Node{
		{Name: "airport-fast", Latency: 50},
		{Name: "airport-slow", Latency: 5000},
		slow,
	}

	result := FilterByLatencyThreshold(nodes, 500)

	if containsNode(result, "airport-slow") {
		t.Error("over-threshold airport node should be filtered")
	}
	if !containsNode(result, "self-slow") {
		t.Error("over-threshold self-hosted node must be retained")
	}
}

func TestSelectBestByRegion_ExemptsSelfHosted(t *testing.T) {
	// nodesPerRegion=1，"SELF" 组有多个自建节点，全部保留（不受上限约束）。
	f := NewFilter(1, false)
	nodes := []*subscription.Node{
		{Name: "hk-1", Region: "HK", Latency: 10},
		{Name: "hk-2", Region: "HK", Latency: 20},
		selfHosted("self-a"),
		selfHosted("self-b"),
		selfHosted("self-c"),
	}

	result := f.selectBestByRegion(nodes)

	// HK 只留延迟最低的 1 个
	if !containsNode(result, "hk-1") || containsNode(result, "hk-2") {
		t.Errorf("HK region should keep only best 1, got %d nodes", len(result))
	}
	// 三个自建节点全部保留
	for _, name := range []string{"self-a", "self-b", "self-c"} {
		if !containsNode(result, name) {
			t.Errorf("self-hosted %s must survive per-region cap", name)
		}
	}
}

func TestDeduplicateNodes_ExemptsSelfHosted(t *testing.T) {
	// 自建节点与机场节点 server:port 相同（NodeKey 碰撞），自建节点不能被并掉。
	f := NewFilter(0, true)
	self := selfHosted("self-collide")
	self.Server, self.Port = "1.2.3.4", 443
	nodes := []*subscription.Node{
		{Name: "airport-collide", Server: "1.2.3.4", Port: 443, Latency: 10},
		self,
	}

	result := f.deduplicateNodes(nodes)

	if !containsNode(result, "self-collide") {
		t.Error("self-hosted node must survive dedup even on NodeKey collision")
	}
	if !containsNode(result, "airport-collide") {
		t.Error("airport node should remain")
	}
}
