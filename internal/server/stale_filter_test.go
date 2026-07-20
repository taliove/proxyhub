package server

import (
	"testing"

	"github.com/taliove/proxyhub/internal/subscription"
)

// TestFilterStaleNodes 验证 stale 节点被排除，非 stale 节点保留
func TestFilterStaleNodes(t *testing.T) {
	nodes := []*subscription.Node{
		{Name: "在架1", Server: "1.1.1.1", Port: 1, Source: "机场A", Stale: false},
		{Name: "已下架", Server: "2.2.2.2", Port: 2, Source: "机场A", Stale: true},
		{Name: "在架2", Server: "3.3.3.3", Port: 3, Source: "机场A", Stale: false},
		{Name: "自建", Server: "4.4.4.4", Port: 4, Source: subscription.SourceSelfHosted, Stale: false},
	}

	got := filterStaleNodes(nodes)

	if len(got) != 3 {
		t.Fatalf("len = %d, want 3 (排除 1 个 stale)", len(got))
	}
	for _, n := range got {
		if n.Stale {
			t.Errorf("stale 节点 %s 不应保留", n.Name)
		}
	}
}

// TestFilterStaleNodes_Empty 验证空输入不 panic
func TestFilterStaleNodes_Empty(t *testing.T) {
	got := filterStaleNodes(nil)
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

// TestFilterStaleNodes_AllStale 验证全 stale 时返回空
func TestFilterStaleNodes_AllStale(t *testing.T) {
	nodes := []*subscription.Node{
		{Name: "a", Server: "1.1.1.1", Port: 1, Stale: true},
		{Name: "b", Server: "2.2.2.2", Port: 2, Stale: true},
	}
	got := filterStaleNodes(nodes)
	if len(got) != 0 {
		t.Errorf("len = %d, want 0 (全部 stale)", len(got))
	}
}
