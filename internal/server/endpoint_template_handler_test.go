package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// TestCreateEndpointWithTemplate tests creating an endpoint with template_name.
func TestCreateEndpointWithTemplate(t *testing.T) {
	srv, st := newEndpointTestServer(t, endpointTestPool())
	userID := int64(1)

	// Create template library
	_, err := st.CreateTemplate(userID, "mobile", `port: 7892
proxy-groups:
  - name: Auto
    type: select
    proxies:
      - '{{nodes}}'`)
	if err != nil {
		t.Fatalf("create template: %v", err)
	}

	h := srv.Handler()
	cookie := authCookie(t, h)

	// Create endpoint with template_name
	reqBody := `{"alias": "test-ep", "template_name": "mobile"}`
	rec := doEndpointRequest(t, h, cookie, "POST", "/api/endpoints", reqBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("create endpoint status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	epID := int64(resp["id"].(float64))

	// Verify template_name is set
	ep, err := st.GetEndpointByIDForUser(userID, epID)
	if err != nil {
		t.Fatalf("get endpoint: %v", err)
	}
	if ep.TemplateName != "mobile" {
		t.Errorf("template_name = %q, want mobile", ep.TemplateName)
	}
}

// TestCreateEndpointWithInvalidTemplate tests binding non-existent template should fail.
func TestCreateEndpointWithInvalidTemplate(t *testing.T) {
	srv, _ := newEndpointTestServer(t, endpointTestPool())
	h := srv.Handler()
	cookie := authCookie(t, h)

	// Create endpoint with non-existent template
	reqBody := `{"alias": "test-ep", "template_name": "nonexistent"}`
	rec := doEndpointRequest(t, h, cookie, "POST", "/api/endpoints", reqBody)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("create with invalid template status = %d, want 400, body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "template not found") {
		t.Errorf("error message should mention 'template not found', got: %s", rec.Body.String())
	}
}

// TestUpdateEndpointTemplate tests PUT /api/endpoints/{id}/template.
func TestUpdateEndpointTemplate(t *testing.T) {
	srv, st := newEndpointTestServer(t, endpointTestPool())
	userID := int64(1)

	// Setup templates
	_, err := st.CreateTemplate(userID, "desktop", `port: 7890
proxy-groups:
  - name: Auto
    type: select
    proxies:
      - '{{nodes}}'`)
	if err != nil {
		t.Fatalf("create template: %v", err)
	}

	ep, err := st.CreateEndpointForUser(userID, "test-ep")
	if err != nil {
		t.Fatalf("create endpoint: %v", err)
	}

	h := srv.Handler()
	cookie := authCookie(t, h)

	// Bind template
	reqBody := `{"template_name": "desktop"}`
	rec := doEndpointRequest(t, h, cookie, "PUT", fmt.Sprintf("/api/endpoints/%d/template", ep.ID), reqBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("update template status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}

	// Verify
	epAfter, _ := st.GetEndpointByIDForUser(userID, ep.ID)
	if epAfter.TemplateName != "desktop" {
		t.Errorf("template_name = %q, want desktop", epAfter.TemplateName)
	}

	// Clear template (reset to follow default)
	reqBody = `{"template_name": ""}`
	rec = doEndpointRequest(t, h, cookie, "PUT", fmt.Sprintf("/api/endpoints/%d/template", ep.ID), reqBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("clear template status = %d, want 200", rec.Code)
	}

	epCleared, _ := st.GetEndpointByIDForUser(userID, ep.ID)
	if epCleared.TemplateName != "" {
		t.Errorf("cleared template_name = %q, want empty", epCleared.TemplateName)
	}
}

// TestUpdateEndpointTemplateNotFound verifies that binding a non-existent
// template via PUT returns 400 (not 500).
func TestUpdateEndpointTemplateNotFound(t *testing.T) {
	srv, st := newEndpointTestServer(t, endpointTestPool())
	userID := int64(1)

	ep, err := st.CreateEndpointForUser(userID, "test-ep")
	if err != nil {
		t.Fatalf("create endpoint: %v", err)
	}

	h := srv.Handler()
	cookie := authCookie(t, h)

	rec := doEndpointRequest(t, h, cookie, "PUT", fmt.Sprintf("/api/endpoints/%d/template", ep.ID), `{"template_name": "nonexistent"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bind missing template status = %d, want 400, body: %s", rec.Code, rec.Body.String())
	}
}
