package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

func countSelfHosted(nodes []*subscription.Node) int {
	n := 0
	for _, node := range nodes {
		if node.Source == subscription.SourceSelfHosted {
			n++
		}
	}
	return n
}

// 刚新增的自建节点(尚未进入聚合池)应在订阅生成时即刻被合并进来,
// 无需等下一轮刷新——坐实需求②:订阅地址里要有自建节点。
func TestFilteredNodes_MergesFreshSelfHosted(t *testing.T) {
	// 节点池里没有任何自建节点(模拟刷新后新增的空档)
	srv, st := newTestServer(t, nil)
	if err := st.CreateSelfHostedNode(&store.SelfHostedNode{
		Name: "自建DMIT香港", Protocol: "vmess", Server: "hysteria.taliove.com", Port: 443, Enabled: true,
	}); err != nil {
		t.Fatalf("CreateSelfHostedNode: %v", err)
	}

	result := srv.filteredNodes(srv.nodes.Nodes(), 0)

	if countSelfHosted(result) != 1 {
		t.Fatalf("self-hosted count = %d, want 1 (should be merged at serve time)", countSelfHosted(result))
	}
}

// 禁用的自建节点不应被合并(ListSelfHostedNodes 只返回启用的)。
func TestMergeSelfHosted_SkipsDisabled(t *testing.T) {
	srv, st := newTestServer(t, nil)
	if err := st.CreateSelfHostedNode(&store.SelfHostedNode{
		Name: "已禁用", Protocol: "vmess", Server: "1.2.3.4", Port: 443, Enabled: false,
	}); err != nil {
		t.Fatalf("CreateSelfHostedNode: %v", err)
	}

	if got := countSelfHosted(srv.mergeSelfHosted(nil, 0)); got != 0 {
		t.Errorf("merged disabled self-hosted count = %d, want 0", got)
	}
}

// 池中已存在的自建节点不应被重复合并(按 NodeKey 去重),且不修改入参底层数组。
func TestMergeSelfHosted_DedupsExisting(t *testing.T) {
	srv, st := newTestServer(t, nil)
	if err := st.CreateSelfHostedNode(&store.SelfHostedNode{
		Name: "自建A", Protocol: "vmess", Server: "1.2.3.4", Port: 443, Enabled: true,
	}); err != nil {
		t.Fatalf("CreateSelfHostedNode: %v", err)
	}

	// 池中已有同一自建节点(相同 server:port → 相同 NodeKey)
	pool := []*subscription.Node{
		{Name: "自建A", Server: "1.2.3.4", Port: 443, Source: subscription.SourceSelfHosted, Available: true, Latency: 42},
	}
	result := srv.mergeSelfHosted(pool, 0)

	if countSelfHosted(result) != 1 {
		t.Errorf("self-hosted count = %d, want 1 (no duplicate)", countSelfHosted(result))
	}
	// 池中带真实健康状态的版本应被保留
	if result[0].Latency != 42 {
		t.Errorf("existing pool node (latency 42) should be preserved, got latency %d", result[0].Latency)
	}
}

// 编辑自建节点配置(如协议 vmess→vless)后,即使池中还留着旧版,
// serve-time 合并也应以库为准输出新配置,同时保留池中的真实健康状态。坐实 #1。
func TestMergeSelfHosted_DBConfigAuthoritative(t *testing.T) {
	srv, st := newTestServer(t, nil)
	if err := st.CreateSelfHostedNode(&store.SelfHostedNode{
		Name: "自建A", Protocol: "vless", Server: "1.2.3.4", Port: 443, Enabled: true,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	// 池中是旧的 vmess 版本(相同 server:port → 相同 NodeKey),带真实健康状态
	pool := []*subscription.Node{
		{Name: "自建A", Type: "vmess", Server: "1.2.3.4", Port: 443,
			Source: subscription.SourceSelfHosted, Available: true, Latency: 42},
	}
	out := srv.mergeSelfHosted(pool, 0)

	if countSelfHosted(out) != 1 {
		t.Fatalf("self-hosted count = %d, want 1", countSelfHosted(out))
	}
	if out[0].Type != "vless" {
		t.Errorf("Type = %q, want vless (DB config must win)", out[0].Type)
	}
	if out[0].Latency != 42 {
		t.Errorf("Latency = %d, want 42 (pool health must be preserved)", out[0].Latency)
	}
}

// 机场节点必须原样保留,不被合并逻辑丢弃。
func TestMergeSelfHosted_KeepsAirportNodes(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	pool := []*subscription.Node{
		{Name: "机场节点", Type: "vless", Server: "9.9.9.9", Port: 8443, Source: "机场X"},
	}
	out := srv.mergeSelfHosted(pool, 0)
	if len(out) != 1 || out[0].Source != "机场X" {
		t.Fatalf("airport node not preserved: %+v", out)
	}
}

// listNodesSelfHostedCount 调 GET /api/nodes 统计自建来源行数(大 page_size 一次拉全)。
func listNodesSelfHostedCount(t *testing.T, h http.Handler, cookie *http.Cookie) int {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/nodes?page_size=100000", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list nodes status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	var resp struct {
		Nodes []struct {
			Source string `json:"source"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	n := 0
	for _, node := range resp.Nodes {
		if node.Source == subscription.SourceSelfHosted {
			n++
		}
	}
	return n
}

// 自建节点不经聚合也应在节点管理列表立即可见(handleListNodes serve-time 合并兜底):
// 池中没有任何节点(模拟聚合失败/未刷新)时,创建即出现在 /api/nodes;
// 禁用、删除即时消失——坐实生产线「创建后搜不到」的修复。
func TestHandleListNodes_MergesSelfHostedWithoutAggregation(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	h := srv.Handler()
	cookie := authCookie(t, h)

	// 经 API 创建,保证属主与列表查询的有效用户一致
	body, _ := json.Marshal(map[string]any{
		"name": "自建HK", "protocol": "vless", "server": "1.2.3.4", "port": 443, "uuid": "00000000-0000-0000-0000-000000000000",
	})
	req := httptest.NewRequest("POST", "/api/self-nodes", bytes.NewReader(body))
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("create status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}

	if got := listNodesSelfHostedCount(t, h, cookie); got != 1 {
		t.Fatalf("after create: self-hosted rows = %d, want 1 (serve-time merge, no aggregation)", got)
	}

	// 禁用即从列表消失(管理界面经 /self-nodes 另补禁用行,池视图只含启用)
	id := listSelfNodesFirstID(t, h, cookie, 1)
	tg, _ := json.Marshal(map[string]bool{"enabled": false})
	req = httptest.NewRequest("POST", "/api/self-nodes/"+strconv.FormatInt(id, 10)+"/toggle", bytes.NewReader(tg))
	req.AddCookie(cookie)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("toggle status = %d, want 200", w.Code)
	}
	if got := listNodesSelfHostedCount(t, h, cookie); got != 0 {
		t.Fatalf("after disable: self-hosted rows = %d, want 0", got)
	}

	// 重新启用恢复可见;删除后消失
	tg, _ = json.Marshal(map[string]bool{"enabled": true})
	req = httptest.NewRequest("POST", "/api/self-nodes/"+strconv.FormatInt(id, 10)+"/toggle", bytes.NewReader(tg))
	req.AddCookie(cookie)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("re-enable status = %d, want 200", w.Code)
	}
	if got := listNodesSelfHostedCount(t, h, cookie); got != 1 {
		t.Fatalf("after re-enable: self-hosted rows = %d, want 1", got)
	}

	req = httptest.NewRequest("DELETE", "/api/self-nodes/"+strconv.FormatInt(id, 10), nil)
	req.AddCookie(cookie)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete status = %d, want 200", w.Code)
	}
	if got := listNodesSelfHostedCount(t, h, cookie); got != 0 {
		t.Fatalf("after delete: self-hosted rows = %d, want 0", got)
	}
}
