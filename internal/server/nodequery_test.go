package server

import (
	"testing"

	"github.com/taliove/proxyhub/internal/subscription"
)

func mkNode(name, typ, region, source string, latency int, available bool) *subscription.Node {
	return &subscription.Node{
		Name: name, Type: typ, Region: region, Source: source,
		Latency: latency, Available: available,
		Server: name, Port: 443, // NodeKey 唯一即可
	}
}

func sampleNodes() []*subscription.Node {
	return []*subscription.Node{
		mkNode("a", "vmess", "HK", "极速", 50, true),
		mkNode("b", "vless", "HK", "极速", 120, true),
		mkNode("c", "vmess", "SG", "花云", 80, false),
		mkNode("d", "trojan", "US", "花云", 200, true),
		mkNode("e", "vmess", "US", "极速", 30, true),
	}
}

func boolPtr(b bool) *bool { return &b }

func TestQueryNodes_FilterRegion(t *testing.T) {
	res := QueryNodes(sampleNodes(), nil, NodeQuery{Region: "HK", Page: 1, PageSize: 10})
	if res.Total != 2 {
		t.Fatalf("Total = %d, want 2", res.Total)
	}
	for _, n := range res.Nodes {
		if n.Region != "HK" {
			t.Errorf("got region %q, want HK", n.Region)
		}
	}
}

func TestQueryNodes_FilterType(t *testing.T) {
	res := QueryNodes(sampleNodes(), nil, NodeQuery{Type: "vmess", Page: 1, PageSize: 10})
	if res.Total != 3 {
		t.Fatalf("Total = %d, want 3", res.Total)
	}
}

func TestQueryNodes_FilterAvailable(t *testing.T) {
	res := QueryNodes(sampleNodes(), nil, NodeQuery{Available: boolPtr(false), Page: 1, PageSize: 10})
	if res.Total != 1 || res.Nodes[0].Name != "c" {
		t.Fatalf("want only node c unavailable, got %+v", res.Nodes)
	}
}

func TestQueryNodes_FilterSourceFuzzy(t *testing.T) {
	res := QueryNodes(sampleNodes(), nil, NodeQuery{Source: "极", Page: 1, PageSize: 10})
	if res.Total != 3 {
		t.Fatalf("Total = %d, want 3", res.Total)
	}
}

func TestQueryNodes_FilterBlocked(t *testing.T) {
	blocked := map[string]bool{"a:443": true} // NodeKey of node "a"
	res := QueryNodes(sampleNodes(), blocked, NodeQuery{Blocked: boolPtr(true), Page: 1, PageSize: 10})
	if res.Total != 1 || res.Nodes[0].Name != "a" {
		t.Fatalf("want only blocked node a, got %+v", res.Nodes)
	}
	// blocked=false 排除已屏蔽
	res2 := QueryNodes(sampleNodes(), blocked, NodeQuery{Blocked: boolPtr(false), Page: 1, PageSize: 10})
	if res2.Total != 4 {
		t.Fatalf("Total = %d, want 4", res2.Total)
	}
}

func TestQueryNodes_SortLatencyAsc(t *testing.T) {
	res := QueryNodes(sampleNodes(), nil, NodeQuery{SortBy: "latency", SortOrder: "asc", Page: 1, PageSize: 10})
	want := []int{30, 50, 80, 120, 200}
	for i, n := range res.Nodes {
		if n.Latency != want[i] {
			t.Errorf("pos %d latency = %d, want %d", i, n.Latency, want[i])
		}
	}
}

func TestQueryNodes_SortLatencyDesc(t *testing.T) {
	res := QueryNodes(sampleNodes(), nil, NodeQuery{SortBy: "latency", SortOrder: "desc", Page: 1, PageSize: 10})
	if res.Nodes[0].Latency != 200 {
		t.Errorf("first latency = %d, want 200", res.Nodes[0].Latency)
	}
}

func TestQueryNodes_Pagination(t *testing.T) {
	res := QueryNodes(sampleNodes(), nil, NodeQuery{SortBy: "latency", SortOrder: "asc", Page: 2, PageSize: 2})
	if res.Total != 5 {
		t.Fatalf("Total = %d, want 5", res.Total)
	}
	if res.TotalPages != 3 {
		t.Fatalf("TotalPages = %d, want 3", res.TotalPages)
	}
	if len(res.Nodes) != 2 {
		t.Fatalf("page size = %d, want 2", len(res.Nodes))
	}
	// 第2页(latency asc): 80, 120
	if res.Nodes[0].Latency != 80 || res.Nodes[1].Latency != 120 {
		t.Errorf("page 2 = [%d,%d], want [80,120]", res.Nodes[0].Latency, res.Nodes[1].Latency)
	}
}

func TestQueryNodes_PageOutOfRange(t *testing.T) {
	res := QueryNodes(sampleNodes(), nil, NodeQuery{Page: 99, PageSize: 2})
	if len(res.Nodes) != 0 {
		t.Errorf("out-of-range page should be empty, got %d", len(res.Nodes))
	}
	if res.Total != 5 {
		t.Errorf("Total = %d, want 5", res.Total)
	}
}

func TestQueryNodes_CombinedFilters(t *testing.T) {
	// HK + vmess + available → 只有 node a
	res := QueryNodes(sampleNodes(), nil, NodeQuery{
		Region: "HK", Type: "vmess", Available: boolPtr(true),
		Page: 1, PageSize: 10,
	})
	if res.Total != 1 || res.Nodes[0].Name != "a" {
		t.Fatalf("want only node a, got %+v", res.Nodes)
	}
}
