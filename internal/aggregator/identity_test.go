package aggregator

import (
	"testing"

	"github.com/taliove/proxyhub/internal/subscription"
)

// TestUpdateNodeIdentity_UpdatesNameAndRegion 确认按 NodeKey 命中后,
// 内存池节点的 Name/Region 立即更新为新值,不必等下一轮聚合刷新。
func TestUpdateNodeIdentity_UpdatesNameAndRegion(t *testing.T) {
	agg, _ := newTestAggregator(t)
	agg.nodes = []*subscription.Node{
		{Name: "boy SELF-02", Server: "192.0.2.1", Port: 443, Region: "Unknown", Source: subscription.SourceSelfHosted},
		{Name: "HK-01", Server: "hk.example.com", Port: 443, Region: "HK", Source: "TestAirport"},
	}

	ok := agg.UpdateNodeIdentity("192.0.2.1:443", "自建香港", "HK")
	if !ok {
		t.Fatal("UpdateNodeIdentity() = false, want true (node in pool)")
	}

	got := agg.Nodes()
	if got[0].Name != "自建香港" {
		t.Errorf("name = %q, want 自建香港", got[0].Name)
	}
	if got[0].Region != "HK" {
		t.Errorf("region = %q, want HK", got[0].Region)
	}
	// 其它节点不受影响
	if got[1].Name != "HK-01" || got[1].Region != "HK" {
		t.Errorf("sibling node mutated: %q/%q", got[1].Name, got[1].Region)
	}
}

// TestUpdateNodeIdentity_NotFound 池中无此 NodeKey 时返回 false,不改动任何节点。
func TestUpdateNodeIdentity_NotFound(t *testing.T) {
	agg, _ := newTestAggregator(t)
	agg.nodes = []*subscription.Node{
		{Name: "HK-01", Server: "hk.example.com", Port: 443, Region: "HK", Source: "TestAirport"},
	}

	if agg.UpdateNodeIdentity("no.such.host:443", "x", "JP") {
		t.Fatal("UpdateNodeIdentity() = true for absent node, want false")
	}
	if agg.Nodes()[0].Name != "HK-01" {
		t.Error("absent-key update mutated existing node")
	}
}

// TestUpdateNodeIdentity_EmptyValuesSkipped region/name 为空时保留原值,
// 避免误抹已有身份字段(region 回写只带 region,rename 只带 name)。
func TestUpdateNodeIdentity_EmptyValuesSkipped(t *testing.T) {
	agg, _ := newTestAggregator(t)
	agg.nodes = []*subscription.Node{
		{Name: "orig", Server: "192.0.2.1", Port: 443, Region: "HK", Source: subscription.SourceSelfHosted},
	}

	// 只回写 region,name 传空:name 应保留
	agg.UpdateNodeIdentity("192.0.2.1:443", "", "JP")
	if got := agg.Nodes()[0]; got.Name != "orig" || got.Region != "JP" {
		t.Errorf("region-only update: got %q/%q, want orig/JP", got.Name, got.Region)
	}

	// 只改名,region 传空:region 应保留
	agg.UpdateNodeIdentity("192.0.2.1:443", "renamed", "")
	if got := agg.Nodes()[0]; got.Name != "renamed" || got.Region != "JP" {
		t.Errorf("name-only update: got %q/%q, want renamed/JP", got.Name, got.Region)
	}
}

// TestUpdateNodeIdentity_ReplacesObject 不可变语义:更新替换切片中的节点对象,
// 调用前抓取的旧指针不被原地改写(与 Nodes() 返回引用的调用方隔离)。
func TestUpdateNodeIdentity_ReplacesObject(t *testing.T) {
	agg, _ := newTestAggregator(t)
	original := &subscription.Node{Name: "orig", Server: "192.0.2.1", Port: 443, Region: "HK", Source: subscription.SourceSelfHosted}
	agg.nodes = []*subscription.Node{original}

	agg.UpdateNodeIdentity("192.0.2.1:443", "renamed", "JP")

	if original.Name != "orig" || original.Region != "HK" {
		t.Errorf("original object mutated in place: %q/%q, want orig/HK", original.Name, original.Region)
	}
	if agg.nodes[0] == original {
		t.Error("pool still holds the old pointer; expected replacement with a new object")
	}
}
