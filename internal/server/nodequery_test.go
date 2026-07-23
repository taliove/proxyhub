package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

// searchPoolNodes 返回一组用于 keyword 搜索测试的节点:
// 名称大小写混合、覆盖 JP/US/HK 地区,便于验证名称片段/地区码/地区中文名三种命中路径。
func searchPoolNodes() []*subscription.Node {
	return []*subscription.Node{
		mkNode("JP-TYO-01 Premium", "vmess", "JP", "极速", 50, true),
		mkNode("us-west relay", "vless", "US", "极速", 120, true),
		mkNode("HK-BGP-03", "trojan", "HK", "花云", 80, true),
		mkNode("Singapore Edge", "vmess", "SG", "花云", 60, true),
	}
}

// TestListNodes_FilterKeyword covers the HTTP wiring the unit tests above cannot:
// query param parsing (parseNodeQuery) -> QueryNodes -> JSON response.
func TestListNodes_FilterKeyword(t *testing.T) {
	srv, _ := newTestServer(t, searchPoolNodes())
	h := srv.Handler()
	cookie := authCookie(t, h)

	req := httptest.NewRequest("GET", "/api/nodes?keyword=%E6%97%A5%E6%9C%AC&page=1&page_size=10", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}

	var resp struct {
		Nodes []nodeViewJSON `json:"nodes"`
		Total int            `json:"total"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// keyword=日本 matches only the JP node via region Chinese name mapping
	if resp.Total != 1 || len(resp.Nodes) != 1 || resp.Nodes[0].Name != "JP-TYO-01 Premium" {
		t.Fatalf("want only JP node, got total=%d nodes=%+v", resp.Total, resp.Nodes)
	}
}

func TestQueryNodes_FilterKeywordNameFragment(t *testing.T) {
	// 名称片段,大小写不敏感
	res := QueryNodes(searchPoolNodes(), nil, NodeQuery{Keyword: "premium", Page: 1, PageSize: 10})
	if res.Total != 1 || res.Nodes[0].Name != "JP-TYO-01 Premium" {
		t.Fatalf("want only JP-TYO-01 Premium, got total=%d %+v", res.Total, res.Nodes)
	}
}

func TestQueryNodes_FilterKeywordRegionCode(t *testing.T) {
	// 地区码精确匹配(不区分大小写);"jp" 不是任何节点名称片段,命中只能来自地区
	res := QueryNodes(searchPoolNodes(), nil, NodeQuery{Keyword: "jp", Page: 1, PageSize: 10})
	if res.Total != 1 || res.Nodes[0].Region != "JP" {
		t.Fatalf("want only JP node, got total=%d %+v", res.Total, res.Nodes)
	}
}

func TestQueryNodes_FilterKeywordRegionChineseName(t *testing.T) {
	// 地区中文名命中:节点名称不含"日本",命中只能来自 Region=JP 的中文名映射
	res := QueryNodes(searchPoolNodes(), nil, NodeQuery{Keyword: "日本", Page: 1, PageSize: 10})
	if res.Total != 1 || res.Nodes[0].Region != "JP" {
		t.Fatalf("want only JP node via Chinese name, got total=%d %+v", res.Total, res.Nodes)
	}
}

func TestQueryNodes_FilterKeywordDisplayName(t *testing.T) {
	// 仅 DisplayName 命中:名称与地区都不含关键词,匹配只能来自标准化展示名
	n := mkNode("relay-x9", "vmess", "US", "极速", 50, true)
	n.DisplayName = "专属通道-01"
	res := QueryNodes([]*subscription.Node{n}, nil, NodeQuery{Keyword: "专属", Page: 1, PageSize: 10})
	if res.Total != 1 {
		t.Fatalf("want hit via DisplayName only, got total=%d %+v", res.Total, res.Nodes)
	}
}

func TestQueryNodes_FilterKeywordNoMatch(t *testing.T) {
	// 既非名称片段也不像地区:空结果合法
	res := QueryNodes(searchPoolNodes(), nil, NodeQuery{Keyword: "不存在的东西zzz", Page: 1, PageSize: 10})
	if res.Total != 0 || len(res.Nodes) != 0 {
		t.Fatalf("want empty result, got total=%d %+v", res.Total, res.Nodes)
	}
}

func TestQueryNodes_FilterKeywordCombinedWithSource(t *testing.T) {
	// keyword 与 source 组合:US 节点属"极速",但 keyword=us 按名称也能命中其他机场节点时仍被 source 收敛
	res := QueryNodes(searchPoolNodes(), nil, NodeQuery{Keyword: "us", Source: "花云", Page: 1, PageSize: 10})
	if res.Total != 0 {
		t.Fatalf("want no US node under 花云, got total=%d %+v", res.Total, res.Nodes)
	}
	res2 := QueryNodes(searchPoolNodes(), nil, NodeQuery{Keyword: "HK", Source: "花云", Page: 1, PageSize: 10})
	if res2.Total != 1 || res2.Nodes[0].Name != "HK-BGP-03" {
		t.Fatalf("want only HK-BGP-03, got total=%d %+v", res2.Total, res2.Nodes)
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
