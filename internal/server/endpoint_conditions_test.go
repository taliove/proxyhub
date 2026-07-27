package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/taliove/proxyhub/internal/subfilter"
	"github.com/taliove/proxyhub/internal/subscription"
)

// condPool 一组带地区/机场/标签维度的可用机场节点(fixture: example.com + 全零 UUID)。
func condPool() []*subscription.Node {
	return []*subscription.Node{
		{Name: "香港A", Type: "ss", Server: "a.example.com", Port: 8388, Cipher: "aes-256-gcm", Password: "p", Available: true, Region: "HK", Source: "机场甲"},
		{Name: "日本B", Type: "ss", Server: "b.example.com", Port: 8388, Cipher: "aes-256-gcm", Password: "p", Available: true, Region: "JP", Source: "机场甲"},
		{Name: "美国C", Type: "ss", Server: "c.example.com", Port: 8388, Cipher: "aes-256-gcm", Password: "p", Available: true, Region: "US", Source: "机场乙"},
	}
}

// setConditions 通过 store 直接写入端点条件(绕过 API,测试订阅链)。
func setConditions(t *testing.T, srv *Server, id int64, raw string) {
	t.Helper()
	if err := srv.st.UpdateEndpointConditions(id, raw); err != nil {
		t.Fatalf("UpdateEndpointConditions: %v", err)
	}
}

// TestSubscription_EmptyConditionsZeroRegression 无条件端点下发全部可用节点(零回归)。
func TestSubscription_EmptyConditionsZeroRegression(t *testing.T) {
	srv, st := newTestServer(t, condPool())
	h := srv.Handler()
	ep, _ := st.CreateEndpointForUser(1, "all")

	out := fetchSub(t, h, ep)
	for _, want := range []string{"香港A", "日本B", "美国C"} {
		if !strings.Contains(out, want) {
			t.Errorf("empty conditions should include %q", want)
		}
	}
}

// TestSubscription_ConditionsFilterByRegion 地区条件只下发命中地区的节点。
func TestSubscription_ConditionsFilterByRegion(t *testing.T) {
	srv, st := newTestServer(t, condPool())
	h := srv.Handler()
	ep, _ := st.CreateEndpointForUser(1, "hk-only")
	setConditions(t, srv, ep.ID, `{"regions":["HK"]}`)

	out := fetchSub(t, h, ep)
	if !strings.Contains(out, "香港A") {
		t.Error("HK node 香港A must be present")
	}
	if strings.Contains(out, "日本B") || strings.Contains(out, "美国C") {
		t.Error("non-HK nodes must be filtered out by region condition")
	}
}

// TestSubscription_ConditionsFilterByTag 标签条件只下发带该自动标签的节点。
func TestSubscription_ConditionsFilterByTag(t *testing.T) {
	srv, st := newTestServer(t, condPool())
	h := srv.Handler()
	// 给香港A / 美国C 打 nf-full 标签(node_key = server:port)
	if err := st.ReplaceNodeTags("a.example.com:8388", []string{"nf-full"}); err != nil {
		t.Fatalf("ReplaceNodeTags: %v", err)
	}
	if err := st.ReplaceNodeTags("c.example.com:8388", []string{"nf-full"}); err != nil {
		t.Fatalf("ReplaceNodeTags: %v", err)
	}
	ep, _ := st.CreateEndpointForUser(1, "nf")
	setConditions(t, srv, ep.ID, `{"tags":["nf-full"]}`)

	out := fetchSub(t, h, ep)
	if !strings.Contains(out, "香港A") || !strings.Contains(out, "美国C") {
		t.Error("nf-full tagged nodes must be present")
	}
	if strings.Contains(out, "日本B") {
		t.Error("untagged 日本B must be filtered out by tag condition")
	}
}

// TestPreview_MatchesConditions 预览走同一条件链(所见即所得)。
func TestPreview_MatchesConditions(t *testing.T) {
	srv, st := newTestServer(t, condPool())
	h := srv.Handler()
	cookie := authCookie(t, h)
	ep, _ := st.CreateEndpointForUser(1, "jp")
	setConditions(t, srv, ep.ID, `{"regions":["JP"]}`)

	req := httptest.NewRequest("GET", "/api/endpoints/"+strconv.FormatInt(ep.ID, 10)+"/preview?format=clash", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("preview status = %d (body %s)", w.Code, w.Body.String())
	}
	var resp struct {
		Count int `json:"count"`
		Nodes []struct {
			Name string `json:"name"`
		} `json:"nodes"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Count != 1 || len(resp.Nodes) != 1 || resp.Nodes[0].Name != "日本B" {
		t.Errorf("preview under JP condition = %+v, want only 日本B", resp)
	}
}

// TestUpdateEndpointConditionsAPI PUT 条件后 List 回显;非法 JSON 体 -> 400。
func TestUpdateEndpointConditionsAPI(t *testing.T) {
	srv, st := newTestServer(t, condPool())
	h := srv.Handler()
	cookie := authCookie(t, h)
	ep, _ := st.CreateEndpointForUser(1, "cfg")

	body, _ := json.Marshal(map[string]any{"regions": []string{"HK"}, "tags": []string{"nf-full"}})
	req := httptest.NewRequest("PUT", "/api/endpoints/"+strconv.FormatInt(ep.ID, 10)+"/conditions", bytes.NewReader(body))
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update conditions status = %d (body %s)", w.Code, w.Body.String())
	}

	got, _ := st.GetEndpointByID(ep.ID)
	if !strings.Contains(got.Conditions, "HK") || !strings.Contains(got.Conditions, "nf-full") {
		t.Errorf("stored conditions = %q, want HK + nf-full", got.Conditions)
	}

	// 非法 JSON 体 -> 400
	req = httptest.NewRequest("PUT", "/api/endpoints/"+strconv.FormatInt(ep.ID, 10)+"/conditions", bytes.NewReader([]byte("{bad")))
	req.AddCookie(cookie)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("invalid body status = %d, want 400", w.Code)
	}
}

// TestApplyConditions_TagLoadFailureDegrades 标签数据读不出时,丢弃 tag 维度、保留其余维度,
// 而非把订阅打空(降级=宁可多给节点)。关闭 store 强制 ListNodeTags 报错。
func TestApplyConditions_TagLoadFailureDegrades(t *testing.T) {
	srv, st := newTestServer(t, condPool())
	st.Close() // 关库:后续 ListNodeTags 必然报错

	// 仅 tag 维度:降级丢弃后条件变空 -> 返回全池
	got := srv.applyConditionsResolved(condPool(), subfilter.Conditions{Tags: []string{"nf-full"}})
	if len(got) != 3 {
		t.Errorf("tag-load failure should degrade to full pool, got %d nodes", len(got))
	}

	// tag + region:降级丢弃 tag 后仍按 region 收窄(不是全池,也不是空)
	got = srv.applyConditionsResolved(condPool(), subfilter.Conditions{
		Regions: []string{"HK"}, Tags: []string{"nf-full"},
	})
	if len(got) != 1 || got[0].Name != "香港A" {
		t.Errorf("expected region dimension retained after tag drop, got %v", names(got))
	}
}

// names 抽取节点名切片(测试断言辅助)。
func names(nodes []*subscription.Node) []string {
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, n.Name)
	}
	return out
}

// TestApplyConditions_TagChunkBoundary 超过单块(500)节点时,collectNodeTags 分块拉取标签
// 并正确合并:跨块打标签的节点都应命中。
func TestApplyConditions_TagChunkBoundary(t *testing.T) {
	nodes := make([]*subscription.Node, 0, 600)
	for i := 0; i < 600; i++ {
		nodes = append(nodes, &subscription.Node{
			Name: fmt.Sprintf("N%d", i), Type: "ss", Cipher: "aes-256-gcm", Password: "p",
			Server: fmt.Sprintf("n%d.example.com", i), Port: 443, Available: true, Region: "HK",
		})
	}
	srv, st := newTestServer(t, nodes)
	// 一个在首块(#10),一个在第二块(#550,越过 500 边界)
	if err := st.ReplaceNodeTags("n10.example.com:443", []string{"nf-full"}); err != nil {
		t.Fatalf("ReplaceNodeTags: %v", err)
	}
	if err := st.ReplaceNodeTags("n550.example.com:443", []string{"nf-full"}); err != nil {
		t.Fatalf("ReplaceNodeTags: %v", err)
	}

	got := srv.applyConditionsResolved(nodes, subfilter.Conditions{Tags: []string{"nf-full"}})
	if len(got) != 2 {
		t.Fatalf("expected 2 tagged nodes across chunk boundary, got %d", len(got))
	}
	gotNames := map[string]bool{got[0].Name: true, got[1].Name: true}
	if !gotNames["N10"] || !gotNames["N550"] {
		t.Errorf("chunk merge dropped a node: got %v, want N10 + N550", names(got))
	}
}

// TestPreviewConditionsCount POST 预览条件命中数(编辑时实时预览,无需先保存)。
func TestPreviewConditionsCount(t *testing.T) {
	srv, st := newTestServer(t, condPool())
	h := srv.Handler()
	cookie := authCookie(t, h)
	_ = st

	body, _ := json.Marshal(map[string]any{"regions": []string{"HK", "US"}})
	req := httptest.NewRequest("POST", "/api/endpoints/preview-conditions", bytes.NewReader(body))
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("preview-conditions status = %d (body %s)", w.Code, w.Body.String())
	}
	var resp struct {
		Count int `json:"count"`
		Total int `json:"total"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Total != 3 {
		t.Errorf("total = %d, want 3", resp.Total)
	}
	if resp.Count != 2 {
		t.Errorf("count (HK+US) = %d, want 2", resp.Count)
	}
}

// TestPreviewConditions_NodeDetails 预览返回节点明细(前 N 个:名称/地区/延迟/带宽/标签),不只是计数。
func TestPreviewConditions_NodeDetails(t *testing.T) {
	// 使用 condPool 确保节点通过 filteredNodes
	srv, st := newTestServer(t, condPool())
	h := srv.Handler()
	cookie := authCookie(t, h)
	_ = st

	// 无条件:全匹配
	body, _ := json.Marshal(map[string]any{"regions": []string{}})
	req := httptest.NewRequest("POST", "/api/endpoints/preview-conditions", bytes.NewReader(body))
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}

	var resp struct {
		Count int `json:"count"`
		Total int `json:"total"`
		Nodes []struct {
			Name              string   `json:"name"`
			Region            string   `json:"region"`
			Latency           int      `json:"latency"`
			Source            string   `json:"source"`
			BandwidthDownMbps *float64 `json:"bandwidth_down_mbps,omitempty"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp.Count != 3 || resp.Total != 3 {
		t.Errorf("count=%d, total=%d; want 3,3", resp.Count, resp.Total)
	}
	if len(resp.Nodes) != 3 {
		t.Fatalf("len(nodes) = %d, want 3", len(resp.Nodes))
	}

	// 检查第一个节点有必需字段
	n0 := resp.Nodes[0]
	if n0.Name == "" {
		t.Error("node[0].name is empty")
	}
	if n0.Region == "" {
		t.Error("node[0].region is empty")
	}
	if n0.Source == "" {
		t.Error("node[0].source is empty")
	}
	// latency/bandwidth 检查存在即可(condPool 无 bandwidth)
}



// TestPreviewConditions_TruncateAt20 节点明细数组截断在 20 个(count 保持真实值)。
// 使用 condPool 基础+复制来避开复杂过滤逻辑。
func TestPreviewConditions_TruncateAt20(t *testing.T) {
	// 基于 condPool 结构复制 25 个可用节点
	base := condPool()
	nodes := make([]*subscription.Node, 0, 25)
	for i := 0; i < 25; i++ {
		// 轮询复制 condPool 的三个节点模板,确保每个节点唯一
		template := base[i%3]
		n := &subscription.Node{
			Name: fmt.Sprintf("%s-%d", template.Name, i),
			Type: template.Type, Cipher: template.Cipher, Password: template.Password,
			Server: fmt.Sprintf("n%d.example.com", i), Port: 8388 + i,
			Region: template.Region, Source: template.Source, Available: template.Available,
		}
		nodes = append(nodes, n)
	}

	srv, st := newTestServer(t, nodes)
	h := srv.Handler()
	cookie := authCookie(t, h)
	_ = st

	body, _ := json.Marshal(map[string]any{"regions": []string{}})
	req := httptest.NewRequest("POST", "/api/endpoints/preview-conditions", bytes.NewReader(body))
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}

	var resp struct {
		Count int                    `json:"count"`
		Nodes []map[string]any `json:"nodes"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)

	// 验证真实命中数大于 20 且明细被截断
	if resp.Count < 20 {
		t.Errorf("count = %d, want >= 20 (至少部分节点通过过滤)", resp.Count)
	}
	if len(resp.Nodes) > 20 {
		t.Errorf("len(nodes) = %d, want <= 20 (截断)", len(resp.Nodes))
	}
	// 最重要的断言:nodes 少于 count(证明截断生效)
	if len(resp.Nodes) >= resp.Count {
		t.Error("nodes array should be truncated, but len(nodes) >= count")
	}
}




// TestPreviewConditions_ZeroMatch 零命中时 nodes 返回空数组(不是 null)。
func TestPreviewConditions_ZeroMatch(t *testing.T) {
	nodes := []*subscription.Node{
		{Name: "HK-01", Type: "ss", Server: "hk1.example.com", Port: 8388, Cipher: "aes-256-gcm", Password: "p", Region: "HK", Source: "机场A", Available: true},
	}

	srv, st := newTestServer(t, nodes)
	h := srv.Handler()
	cookie := authCookie(t, h)
	_ = st

	// 筛选不存在的地区
	body, _ := json.Marshal(map[string]any{"regions": []string{"US"}})
	req := httptest.NewRequest("POST", "/api/endpoints/preview-conditions", bytes.NewReader(body))
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp struct {
		Count int                    `json:"count"`
		Nodes []map[string]any `json:"nodes"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp.Count != 0 {
		t.Errorf("count = %d, want 0", resp.Count)
	}
	if resp.Nodes == nil {
		t.Error("nodes should be [] not null")
	}
	if len(resp.Nodes) != 0 {
		t.Errorf("len(nodes) = %d, want 0", len(resp.Nodes))
	}
}



