package server

import (
	"strings"
	"testing"

	"github.com/taliove/proxyhub/internal/subscription"
)

// TestRenderSubscriptionTemplateFallback tests the 4-level fallback chain for Clash template resolution:
// endpoint.template_name -> user default template -> system_settings.clash_template -> embedded default.
func TestRenderSubscriptionTemplateFallback(t *testing.T) {
	srv, st := newEndpointTestServer(t, endpointTestPool())
	userID := int64(789)

	nodes := []*subscription.Node{
		{Name: "test-node", Type: "ss", Server: "x.example.com", Port: 8388, Cipher: "aes-256-gcm", Password: "p"},
	}

	// Level 4: Embedded default (no user template, no global override)
	data, _, err := srv.renderSubscription(nodes, "clash", userID)
	if err != nil {
		t.Fatalf("render with embedded default: %v", err)
	}
	if !strings.Contains(string(data), "proxies:") {
		t.Errorf("embedded default should contain 'proxies:', got:\n%s", string(data))
	}

	// Level 3: Global default (set system_settings.clash_template)
	globalTemplate := `port: 7890
proxy-groups:
  - name: Auto
    type: select
    proxies:
      - '{{nodes}}'`
	if err := st.SetClashTemplate(globalTemplate); err != nil {
		t.Fatalf("set global template: %v", err)
	}
	data, _, err = srv.renderSubscription(nodes, "clash", userID)
	if err != nil {
		t.Fatalf("render with global default: %v", err)
	}
	if !strings.Contains(string(data), "port: 7890") {
		t.Errorf("should use global template (port 7890), got:\n%s", string(data))
	}

	// Level 2: User default template
	_, err = st.CreateTemplate(userID, "my-default", `port: 7891
proxy-groups:
  - name: Auto
    type: select
    proxies:
      - '{{nodes}}'`)
	if err != nil {
		t.Fatalf("create user template: %v", err)
	}
	if err := st.SetDefaultTemplate(userID, "my-default"); err != nil {
		t.Fatalf("set default template: %v", err)
	}
	data, _, err = srv.renderSubscription(nodes, "clash", userID)
	if err != nil {
		t.Fatalf("render with user default: %v", err)
	}
	if !strings.Contains(string(data), "port: 7891") {
		t.Errorf("should use user default template (port 7891), got:\n%s", string(data))
	}

	// Level 1: Endpoint-specific template (highest priority)
	_, err = st.CreateTemplate(userID, "mobile", `port: 7892
proxy-groups:
  - name: Auto
    type: select
    proxies:
      - '{{nodes}}'`)
	if err != nil {
		t.Fatalf("create mobile template: %v", err)
	}
	ep, err := st.CreateEndpointForUser(userID, "test-ep")
	if err != nil {
		t.Fatalf("create endpoint: %v", err)
	}
	if err := st.UpdateEndpointTemplate(userID, ep.ID, "mobile"); err != nil {
		t.Fatalf("bind template: %v", err)
	}
	epAfter, _ := st.GetEndpointByIDForUser(userID, ep.ID)
	data, _, err = srv.renderSubscriptionForEndpoint(nodes, "clash", epAfter)
	if err != nil {
		t.Fatalf("render with endpoint template: %v", err)
	}
	if !strings.Contains(string(data), "port: 7892") {
		t.Errorf("should use endpoint-specific template (port 7892), got:\n%s", string(data))
	}
}

// TestRenderSubscriptionTemplateSoftReference tests soft reference behavior:
// deleting a referenced template causes fallback instead of error.
func TestRenderSubscriptionTemplateSoftReference(t *testing.T) {
	srv, st := newEndpointTestServer(t, endpointTestPool())
	userID := int64(890)

	nodes := []*subscription.Node{
		{Name: "test-node", Type: "ss", Server: "y.example.com", Port: 8388, Cipher: "aes-256-gcm", Password: "p"},
	}

	// Create user default template and endpoint-specific template
	_, err := st.CreateTemplate(userID, "default-tmpl", `port: 7890
proxy-groups:
  - name: Auto
    type: select
    proxies:
      - '{{nodes}}'`)
	if err != nil {
		t.Fatalf("create default template: %v", err)
	}
	if err := st.SetDefaultTemplate(userID, "default-tmpl"); err != nil {
		t.Fatalf("set default: %v", err)
	}

	_, err = st.CreateTemplate(userID, "temp-tmpl", `port: 7891
proxy-groups:
  - name: Auto
    type: select
    proxies:
      - '{{nodes}}'`)
	if err != nil {
		t.Fatalf("create temp template: %v", err)
	}

	ep, err := st.CreateEndpointForUser(userID, "test-ep")
	if err != nil {
		t.Fatalf("create endpoint: %v", err)
	}
	if err := st.UpdateEndpointTemplate(userID, ep.ID, "temp-tmpl"); err != nil {
		t.Fatalf("bind temp template: %v", err)
	}

	// Before deletion: should use temp-tmpl
	epBefore, _ := st.GetEndpointByIDForUser(userID, ep.ID)
	data, _, err := srv.renderSubscriptionForEndpoint(nodes, "clash", epBefore)
	if err != nil {
		t.Fatalf("render before deletion: %v", err)
	}
	if !strings.Contains(string(data), "port: 7891") {
		t.Errorf("before deletion should use temp template (port 7891), got:\n%s", string(data))
	}

	// Delete the referenced template
	if err := st.DeleteTemplate(userID, "temp-tmpl"); err != nil {
		t.Fatalf("delete template: %v", err)
	}

	// After deletion: should fall back to user default (no error)
	epAfter, _ := st.GetEndpointByIDForUser(userID, ep.ID)
	data, _, err = srv.renderSubscriptionForEndpoint(nodes, "clash", epAfter)
	if err != nil {
		t.Fatalf("render after deletion should not error: %v", err)
	}
	if !strings.Contains(string(data), "port: 7890") {
		t.Errorf("after deletion should fall back to default (port 7890), got:\n%s", string(data))
	}
}

// TestRenderSubscriptionV2RayUnaffected tests that V2Ray format is unaffected by template logic.
func TestRenderSubscriptionV2RayUnaffected(t *testing.T) {
	srv, st := newEndpointTestServer(t, endpointTestPool())
	userID := int64(991)

	nodes := []*subscription.Node{
		{Name: "v2ray-node", Type: "vmess", Server: "z.example.com", Port: 443, UUID: "00000000-0000-0000-0000-000000000000"},
	}

	// Create endpoint with template binding
	_, err := st.CreateTemplate(userID, "clash-only", `port: 7893
proxy-groups:
  - name: Auto
    type: select
    proxies:
      - '{{nodes}}'`)
	if err != nil {
		t.Fatalf("create template: %v", err)
	}
	ep, err := st.CreateEndpointForUser(userID, "v2ray-ep")
	if err != nil {
		t.Fatalf("create endpoint: %v", err)
	}
	if err := st.UpdateEndpointTemplate(userID, ep.ID, "clash-only"); err != nil {
		t.Fatalf("bind template: %v", err)
	}

	// V2Ray format should still work (base64 link list)
	epData, _ := st.GetEndpointByIDForUser(userID, ep.ID)
	data, contentType, err := srv.renderSubscriptionForEndpoint(nodes, "v2ray", epData)
	if err != nil {
		t.Fatalf("render v2ray: %v", err)
	}
	if contentType != "text/plain; charset=utf-8" {
		t.Errorf("v2ray content-type = %s, want text/plain", contentType)
	}
	if strings.Contains(string(data), "port: 7893") {
		t.Errorf("v2ray format should not use Clash template")
	}
}
