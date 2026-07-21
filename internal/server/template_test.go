package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/taliove/proxyhub/internal/generator"
	"github.com/taliove/proxyhub/internal/subscription"
)

// templateNodes 返回一组可用节点用于订阅渲染断言。
func templateNodes() []*subscription.Node {
	return []*subscription.Node{
		{Name: "香港优选", Type: "ss", Server: "1.1.1.1", Port: 8388,
			Cipher: "aes-256-gcm", Password: "p", Available: true, Source: "机场A"},
		{Name: "日本优选", Type: "trojan", Server: "2.2.2.2", Port: 443,
			Password: "p", TLS: true, Available: true, Source: "机场A"},
	}
}

func TestTemplateAPI_GetDefault(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	h := srv.Handler()
	cookie := authCookie(t, h)

	req := httptest.NewRequest("GET", "/api/settings/template", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	var resp struct {
		Template string `json:"template"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !strings.Contains(resp.Template, "{{nodes}}") {
		t.Error("default template should contain {{nodes}} placeholder")
	}
}

func TestTemplateAPI_RequiresAuth(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	h := srv.Handler()

	req := httptest.NewRequest("GET", "/api/settings/template", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 without session", w.Code)
	}
}

func TestTemplateAPI_SaveAndGet(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	h := srv.Handler()
	cookie := authCookie(t, h)

	custom := "mode: rule\nproxy-groups:\n  - name: 手动切换\n    type: select\n    proxies: [DIRECT, '{{nodes}}']\nrules:\n  - MATCH,手动切换\n"
	body, _ := json.Marshal(map[string]string{"template": custom})
	req := httptest.NewRequest("PUT", "/api/settings/template", bytes.NewReader(body))
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}

	// 再次读取应拿到刚保存的内容
	req2 := httptest.NewRequest("GET", "/api/settings/template", nil)
	req2.AddCookie(cookie)
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req2)
	var resp struct {
		Template string `json:"template"`
	}
	json.Unmarshal(w2.Body.Bytes(), &resp)
	if resp.Template != custom {
		t.Errorf("template = %q, want %q", resp.Template, custom)
	}
}

func TestTemplateAPI_RejectsInvalidYAML(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	cookie := authCookie(t, h)

	body, _ := json.Marshal(map[string]string{"template": "proxy-groups: [ bad : yaml : here"})
	req := httptest.NewRequest("PUT", "/api/settings/template", bytes.NewReader(body))
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for invalid YAML", w.Code)
	}
	// 无效模板不应落库：仍返回默认模板
	got, _ := st.GetClashTemplate()
	if !strings.Contains(got, "{{nodes}}") {
		t.Error("invalid template must not be persisted")
	}
}

func TestTemplateAPI_RejectsEmpty(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	h := srv.Handler()
	cookie := authCookie(t, h)

	body, _ := json.Marshal(map[string]string{"template": ""})
	req := httptest.NewRequest("PUT", "/api/settings/template", bytes.NewReader(body))
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for empty template", w.Code)
	}
}

func TestTemplateAPI_Reset(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	cookie := authCookie(t, h)

	// 先存一个自定义模板
	if err := st.SetClashTemplate("mode: rule\n"); err != nil {
		t.Fatalf("SetClashTemplate: %v", err)
	}

	req := httptest.NewRequest("POST", "/api/settings/template/reset", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("reset status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}

	got, _ := st.GetClashTemplate()
	if !strings.Contains(got, "{{nodes}}") {
		t.Error("after reset, template should be default (containing {{nodes}})")
	}
}

// TestSubscription_UsesTemplate 端到端验证 /sub 输出基于模板渲染:
// 首组与内嵌默认模板声明的首组一致(断言从模板推导),且节点已动态注入。
func TestSubscription_UsesTemplate(t *testing.T) {
	srv, st := newTestServer(t, templateNodes())
	h := srv.Handler()
	ep, _ := st.CreateEndpoint("dev")

	req := httptest.NewRequest("GET", "/sub/"+ep.Path+"?token="+ep.Token+"&format=clash", nil)
	req.RemoteAddr = "1.2.3.4:5678"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}

	var doc struct {
		DNS         map[string]any   `yaml:"dns"`
		Proxies     []map[string]any `yaml:"proxies"`
		ProxyGroups []struct {
			Name    string   `yaml:"name"`
			Proxies []string `yaml:"proxies"`
		} `yaml:"proxy-groups"`
		Rules []string `yaml:"rules"`
	}
	if err := yaml.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatalf("subscription output is not valid YAML: %v", err)
	}

	// 模板被应用的证据:dns 段存在,且首组与默认模板声明的首组同名
	if len(doc.DNS) == 0 {
		t.Error("subscription missing dns (template not applied)")
	}
	var tmplDoc struct {
		ProxyGroups []struct {
			Name string `yaml:"name"`
		} `yaml:"proxy-groups"`
	}
	if err := yaml.Unmarshal([]byte(generator.DefaultTemplate()), &tmplDoc); err != nil {
		t.Fatalf("default template is not valid YAML: %v", err)
	}
	if len(doc.ProxyGroups) == 0 {
		t.Fatal("subscription produced no proxy-groups")
	}
	if doc.ProxyGroups[0].Name != tmplDoc.ProxyGroups[0].Name {
		t.Errorf("first group = %q, want default template's first group %q",
			doc.ProxyGroups[0].Name, tmplDoc.ProxyGroups[0].Name)
	}

	// 节点注入:2 个测试节点出现在 proxies,且至少一个组引用节点名
	if len(doc.Proxies) != 2 {
		t.Errorf("subscription proxies = %d, want 2 (nodes not injected)", len(doc.Proxies))
	}
	found := false
	for _, g := range doc.ProxyGroups {
		for _, p := range g.Proxies {
			if p == "香港优选" {
				found = true
			}
		}
	}
	if !found {
		t.Error("injected node name 香港优选 not found in any proxy-group")
	}

	// 规则非空且有 MATCH 兜底
	if len(doc.Rules) == 0 {
		t.Error("subscription produced no rules (template not applied)")
	} else if last := doc.Rules[len(doc.Rules)-1]; !strings.HasPrefix(last, "MATCH,") {
		t.Errorf("last rule = %q, want MATCH catch-all", last)
	}
}

// TestSubscription_UsesCustomTemplate 保存自定义模板后，/sub 立即生效。
func TestSubscription_UsesCustomTemplate(t *testing.T) {
	srv, st := newTestServer(t, templateNodes())
	h := srv.Handler()
	ep, _ := st.CreateEndpoint("dev")

	custom := "mode: rule\nproxy-groups:\n  - name: 唯一组\n    type: select\n    proxies: [DIRECT, '{{nodes}}']\nrules:\n  - MATCH,唯一组\n"
	if err := st.SetClashTemplate(custom); err != nil {
		t.Fatalf("SetClashTemplate: %v", err)
	}

	req := httptest.NewRequest("GET", "/sub/"+ep.Path+"?token="+ep.Token+"&format=clash", nil)
	req.RemoteAddr = "1.2.3.4:5678"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}

	body := w.Body.String()
	if !strings.Contains(body, "唯一组") {
		t.Error("custom template not applied to /sub output")
	}
	if !strings.Contains(body, "香港优选") {
		t.Error("nodes not injected into custom template")
	}
}
