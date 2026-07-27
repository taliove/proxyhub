package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

func stdNodes() []*subscription.Node {
	return []*subscription.Node{
		{Name: "HK-old-1", Type: "vmess", Region: "HK", Source: "极速机场", Server: "1.1.1.1", Port: 443, Available: true, Latency: 50},
		{Name: "HK-old-2", Type: "vmess", Region: "HK", Source: "极速机场", Server: "2.2.2.2", Port: 443, Available: true, Latency: 60},
	}
}

// 开启标准化后,/api/nodes 的 display_name 应为标准格式;关闭时为空。
func TestListNodes_Standardization(t *testing.T) {
	srv, st := newTestServer(t, stdNodes())
	h := srv.Handler()
	cookie := authCookie(t, h)

	// 建一个机场,给出简称 JS
	if _, err := st.CreateAirport("极速机场", "http://example.com"); err != nil {
		t.Fatalf("create airport: %v", err)
	}
	airports, _ := st.ListAirports()
	if err := st.UpdateAirport(airports[0].ID, "极速机场", "http://example.com", "JS"); err != nil {
		t.Fatalf("set abbr: %v", err)
	}

	// 默认关闭:display_name 应为空
	nodes := listNodes(t, h, cookie)
	for _, n := range nodes {
		if n.DisplayName != "" {
			t.Errorf("standardization off, but display_name=%q", n.DisplayName)
		}
	}

	// 开启标准化
	if err := st.SaveSystemSettings(map[string]string{"standardize_names": "true"}); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	nodes = listNodes(t, h, cookie)
	got := map[string]string{}
	for _, n := range nodes {
		got[n.Name] = n.DisplayName
	}
	// NodeKey 排序:1.1.1.1 → 01, 2.2.2.2 → 02
	if got["HK-old-1"] != "🇭🇰 香港 JS-01" {
		t.Errorf("HK-old-1 display_name = %q, want 🇭🇰 香港 JS-01", got["HK-old-1"])
	}
	if got["HK-old-2"] != "🇭🇰 香港 JS-02" {
		t.Errorf("HK-old-2 display_name = %q, want 🇭🇰 香港 JS-02", got["HK-old-2"])
	}
}

func TestListNodes_Pagination(t *testing.T) {
	srv, _ := newTestServer(t, stdNodes())
	h := srv.Handler()
	cookie := authCookie(t, h)

	req := httptest.NewRequest("GET", "/api/nodes?page=1&page_size=1", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}

	var resp struct {
		Nodes      []nodeViewJSON `json:"nodes"`
		Total      int            `json:"total"`
		TotalPages int            `json:"total_pages"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total != 2 {
		t.Errorf("total = %d, want 2", resp.Total)
	}
	if resp.TotalPages != 2 {
		t.Errorf("total_pages = %d, want 2", resp.TotalPages)
	}
	if len(resp.Nodes) != 1 {
		t.Errorf("page size = %d, want 1", len(resp.Nodes))
	}
}

// 端点覆盖:全局关但端点强制开;端点自定义模板覆盖全局模板(见 ADR 0012)。
func TestResolveNameConfig_EndpointOverride(t *testing.T) {
	srv, st := newTestServer(t, nil)
	if err := st.SaveSystemSettings(map[string]string{
		"standardize_names": "false",
		"name_template":     "{region} {source_abbr}-{index}",
	}); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	// nil 端点 → 取全局(关)
	if std, _ := srv.resolveNameConfig(0, nil); std {
		t.Error("global off, nil endpoint should be off")
	}

	// 端点强制开 + 自定义模板 → 覆盖全局
	ep := &store.Endpoint{NameMode: store.NameModeOn, NameTemplate: "{emoji}{index}"}
	std, tmpl := srv.resolveNameConfig(0, ep)
	if !std {
		t.Error("endpoint NameModeOn should force standardize on")
	}
	if tmpl != "{emoji}{index}" {
		t.Errorf("template = %q, want endpoint override {emoji}{index}", tmpl)
	}

	// 端点强制关,即便全局开也关
	if err := st.SaveSystemSettings(map[string]string{"standardize_names": "true"}); err != nil {
		t.Fatalf("save settings: %v", err)
	}
	off := &store.Endpoint{NameMode: store.NameModeOff}
	if std, _ := srv.resolveNameConfig(0, off); std {
		t.Error("endpoint NameModeOff should force standardize off despite global on")
	}

	// 端点 inherit + 空模板 → 回退全局(开 + 全局模板)
	inherit := &store.Endpoint{NameMode: store.NameModeInherit}
	std, tmpl = srv.resolveNameConfig(0, inherit)
	if !std {
		t.Error("inherit should follow global on")
	}
	if tmpl != "{region} {source_abbr}-{index}" {
		t.Errorf("template = %q, want global template", tmpl)
	}
}

type nodeViewJSON struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
}

func listNodes(t *testing.T, h http.Handler, cookie *http.Cookie) []nodeViewJSON {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/nodes?page_size=100", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list nodes status = %d", w.Code)
	}
	var resp struct {
		Nodes []nodeViewJSON `json:"nodes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return resp.Nodes
}

func TestApplyStandardization_SelfHostedGetsSelfAbbr(t *testing.T) {
	srv, st := newTestServer(t, nil)
	// 开启标准化
	if err := st.SaveSystemSettings(map[string]string{"standardize_names": "true"}); err != nil {
		t.Fatalf("save settings: %v", err)
	}
	nodes := []*subscription.Node{
		{Name: "我的东京机", Region: "JP", Source: subscription.SourceSelfHosted, Server: "1.2.3.4", Port: 443},
	}
	std, tmpl := srv.resolveNameConfig(0, nil)
	out := srv.applyStandardization(nodes, std, tmpl)
	if out[0].DisplayName == "" || !strings.Contains(out[0].DisplayName, "SELF") {
		t.Fatalf("self-hosted DisplayName = %q, want to contain SELF", out[0].DisplayName)
	}
}
