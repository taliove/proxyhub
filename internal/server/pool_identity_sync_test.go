package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/taliove/proxyhub/internal/detection"
	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

type nodeViewFull struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Region      string `json:"region"`
	Server      string `json:"server"`
	Port        int    `json:"port"`
}

// listNodeViews 拉取 /nodes 全量并按 server:port 建索引,便于断言单节点的 name/region。
func listNodeViews(t *testing.T, h http.Handler, cookie *http.Cookie) map[string]nodeViewFull {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/nodes?page_size=100", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list nodes status = %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Nodes []nodeViewFull `json:"nodes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	out := make(map[string]nodeViewFull, len(resp.Nodes))
	for _, n := range resp.Nodes {
		out[n.Server+":"+strconv.Itoa(n.Port)] = n
	}
	return out
}

// TestRefreshNames_SelfHosted_PoolSyncsImmediately 复现票据 47:
// 自建节点 refresh-names 把 region/name 写进 DB 后,/nodes 列表必须立即反映新值,
// 而不是等下一轮聚合刷新。修复前:pool 里仍是旧 name/region。
func TestRefreshNames_SelfHosted_PoolSyncsImmediately(t *testing.T) {
	// 池中的自建节点带旧身份(Unknown 地区 + 旧名),模拟聚合上一轮的快照。
	stalePoolNode := &subscription.Node{
		Name:   "boy SELF-02",
		Server: "192.0.2.1",
		Port:   443,
		Type:   "vmess",
		UUID:   "00000000-0000-0000-0000-000000000000",
		Region: "Unknown",
		Source: subscription.SourceSelfHosted,
	}
	srv, st := newTestServer(t, []*subscription.Node{stalePoolNode})
	h := srv.Handler()
	cookie := authCookie(t, h)

	// DB 里的自建节点(权威),refresh 会据此重算 region/name。
	dbNode := &store.SelfHostedNode{
		Name:       "boy SELF-02",
		Protocol:   "vmess",
		Server:     "192.0.2.1",
		Port:       443,
		UUID:       "00000000-0000-0000-0000-000000000000",
		RegionCode: "Unknown",
		Enabled:    true,
	}
	if err := st.CreateSelfHostedNode(dbNode); err != nil {
		t.Fatalf("create self node: %v", err)
	}

	// GeoIP 解析 192.0.2.1 -> HK,驱动 refresh 把名字改成 自建香港、region 改成 HK。
	srv.countryLookup = func(ip string) (string, error) { return "HK", nil }

	// 触发 refresh-names
	body := bytes.NewReader([]byte(`{"node_keys":["192.0.2.1:443"]}`))
	req := httptest.NewRequest(http.MethodPost, "/api/nodes/refresh-names", body)
	rec := httptest.NewRecorder()
	srv.handleRefreshNames(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh-names status = %d: %s", rec.Code, rec.Body.String())
	}

	// 立即查 /nodes:pool 里的自建节点 name 必须是新名,region 必须是 HK。
	got, ok := listNodeViews(t, h, cookie)["192.0.2.1:443"]
	if !ok {
		t.Fatal("self-hosted node missing from /nodes")
	}
	if got.Name != "自建香港" {
		t.Errorf("pool name = %q, want 自建香港 (rename should sync to pool immediately)", got.Name)
	}
	if got.Region != "HK" {
		t.Errorf("pool region = %q, want HK (region writeback should sync to pool immediately)", got.Region)
	}
}

// TestExamWriteback_SelfHosted_PoolRegionSyncsImmediately 票据 47:
// 体检回写 egress region 到 DB 后,/nodes 里自建节点的 region 立即为新值。
func TestExamWriteback_SelfHosted_PoolRegionSyncsImmediately(t *testing.T) {
	stalePoolNode := &subscription.Node{
		Name:   "自建节点",
		Server: "192.0.2.1",
		Port:   443,
		Type:   "vmess",
		UUID:   "00000000-0000-0000-0000-000000000000",
		Region: "Unknown",
		Source: subscription.SourceSelfHosted,
	}
	srv, st := newTestServer(t, []*subscription.Node{stalePoolNode})
	h := srv.Handler()
	cookie := authCookie(t, h)

	dbNode := &store.SelfHostedNode{
		Name:       "自建节点",
		Protocol:   "vmess",
		Server:     "192.0.2.1",
		Port:       443,
		UUID:       "00000000-0000-0000-0000-000000000000",
		RegionCode: "Unknown",
		Enabled:    true,
	}
	if err := st.CreateSelfHostedNode(dbNode); err != nil {
		t.Fatalf("create self node: %v", err)
	}

	report := detection.ExamReport{
		Egress: &detection.EgressMetrics{
			IPv4: &detection.EgressIPv4{CountryCode: "JP"},
		},
	}
	srv.onExamComplete("192.0.2.1:443", report)

	got := listNodeViews(t, h, cookie)["192.0.2.1:443"]
	if got.Region != "JP" {
		t.Errorf("pool region = %q, want JP (exam writeback should sync to pool immediately)", got.Region)
	}
}

// TestExamWriteback_Airport_PoolRegionSyncsImmediately 票据 47:
// 机场节点 region 回写走 updateAirportNodeRegion(已直接改池),/nodes 立即反映。
// 这条守护既有行为不被回归。
func TestExamWriteback_Airport_PoolRegionSyncsImmediately(t *testing.T) {
	poolNode := &subscription.Node{
		Name:   "HK-01",
		Server: "hk.example.com",
		Port:   443,
		Type:   "vmess",
		UUID:   "00000000-0000-0000-0000-000000000001",
		Region: "Unknown",
		Source: "TestAirport",
	}
	srv, _ := newTestServer(t, []*subscription.Node{poolNode})
	h := srv.Handler()
	cookie := authCookie(t, h)

	report := detection.ExamReport{
		Egress: &detection.EgressMetrics{
			IPv4: &detection.EgressIPv4{CountryCode: "US"},
		},
	}
	srv.onExamComplete("hk.example.com:443", report)

	got := listNodeViews(t, h, cookie)["hk.example.com:443"]
	if got.Region != "US" {
		t.Errorf("airport pool region = %q, want US", got.Region)
	}
}

// TestUpdateSelfNode_Rename_PoolSyncsImmediately 票据 47:
// 用户通过 PUT /api/self-nodes/{id} 改名后,/nodes 立即显示新名(不等聚合刷新)。
func TestUpdateSelfNode_Rename_PoolSyncsImmediately(t *testing.T) {
	stalePoolNode := &subscription.Node{
		Name:   "old-name",
		Server: "192.0.2.1",
		Port:   443,
		Type:   "vmess",
		UUID:   "00000000-0000-0000-0000-000000000000",
		Region: "HK",
		Source: subscription.SourceSelfHosted,
	}
	srv, st := newTestServer(t, []*subscription.Node{stalePoolNode})
	h := srv.Handler()
	cookie := authCookie(t, h)

	dbNode := &store.SelfHostedNode{
		Name:       "old-name",
		Protocol:   "vmess",
		Server:     "192.0.2.1",
		Port:       443,
		UUID:       "00000000-0000-0000-0000-000000000000",
		RegionCode: "HK",
		Enabled:    true,
	}
	if err := st.CreateSelfHostedNode(dbNode); err != nil {
		t.Fatalf("create self node: %v", err)
	}
	// 取回带 ID 的节点
	all, err := st.ListAllSelfHostedNodes()
	if err != nil || len(all) != 1 {
		t.Fatalf("list self nodes: %v (n=%d)", err, len(all))
	}
	id := all[0].ID

	// region 已知(HK),名字留空 -> 落地为 自建香港;避免依赖 GeoIP。
	srv.countryLookup = func(ip string) (string, error) { return "HK", nil }
	payload, _ := json.Marshal(map[string]any{
		"name":     "",
		"protocol": "vmess",
		"server":   "192.0.2.1",
		"port":     443,
		"uuid":     "00000000-0000-0000-0000-000000000000",
	})
	req := httptest.NewRequest(http.MethodPut, "/api/self-nodes/"+strconv.FormatInt(id, 10), bytes.NewReader(payload))
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update self node status = %d: %s", w.Code, w.Body.String())
	}

	got := listNodeViews(t, h, cookie)["192.0.2.1:443"]
	if got.Name != "自建香港" {
		t.Errorf("pool name = %q, want 自建香港 (edit should sync to pool immediately)", got.Name)
	}
}
