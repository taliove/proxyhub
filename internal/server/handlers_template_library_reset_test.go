package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/taliove/proxyhub/internal/store"
)

// TestResetTemplateToDefault verifies that resetting a template restores
// embedded default content and creates a new version.
func TestResetTemplateToDefault(t *testing.T) {
	srv, st := newEndpointTestServer(t, endpointTestPool())

	const anyHash = "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"
	owner, err := st.CreateUser("reset-owner", anyHash, store.RoleUser, false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	// Create a template with custom content
	customContent := "port: 9999\nmode: direct"
	tmpl, err := st.CreateTemplate(owner.ID, "custom", customContent)
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}

	// Update it once more to have version 2
	err = st.UpdateTemplate(owner.ID, "custom", "port: 8888\nmode: rule")
	if err != nil {
		t.Fatalf("UpdateTemplate: %v", err)
	}

	// Verify we have 2 versions
	versions, err := st.ListVersions(owner.ID, "custom")
	if err != nil || len(versions) != 2 {
		t.Fatalf("expected 2 versions, got %d (err=%v)", len(versions), err)
	}

	h := srv.Handler()
	cookie := templateLibrarySession(t, srv, owner.ID, store.RoleUser)

	// Reset template
	rec := doEndpointRequest(t, h, cookie, "POST", "/api/templates/custom/reset", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("reset status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}

	// Verify response contains success message
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if msg, ok := resp["message"].(string); !ok || !strings.Contains(msg, "reset") {
		t.Errorf("expected success message with 'reset', got: %v", resp)
	}

	// Verify template content is now embedded default
	tmpl, err = st.GetTemplateByName(owner.ID, "custom")
	if err != nil {
		t.Fatalf("GetTemplateByName after reset: %v", err)
	}

	// Embedded default should contain characteristic strings
	if !strings.Contains(tmpl.Content, "proxy-groups:") {
		t.Errorf("reset content missing 'proxy-groups:', got: %s", tmpl.Content[:100])
	}
	if !strings.Contains(tmpl.Content, "🚀 节点选择") {
		t.Errorf("reset content missing characteristic emoji groups")
	}

	// Verify a new version was created (should have 3 versions now)
	versions, err = st.ListVersions(owner.ID, "custom")
	if err != nil {
		t.Fatalf("ListVersions after reset: %v", err)
	}
	if len(versions) != 3 {
		t.Errorf("expected 3 versions after reset, got %d", len(versions))
	}

	// Latest version (version 3) should have embedded default content
	latestMeta := versions[0] // versions are ordered DESC
	if latestMeta.Version != 3 {
		t.Errorf("latest version = %d, want 3", latestMeta.Version)
	}

	// Get the actual content of version 3
	latestVersion, err := st.GetVersionContent(owner.ID, "custom", 3)
	if err != nil {
		t.Fatalf("GetVersionContent(3): %v", err)
	}
	if !strings.Contains(latestVersion.Content, "proxy-groups:") {
		t.Errorf("latest version content missing 'proxy-groups:', got first 200 chars: %s", latestVersion.Content[:min(200, len(latestVersion.Content))])
	}
}

// TestResetTemplateNotFound verifies 404 for non-existent template.
func TestResetTemplateNotFound(t *testing.T) {
	srv, st := newEndpointTestServer(t, endpointTestPool())

	const anyHash = "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"
	owner, err := st.CreateUser("reset-404", anyHash, store.RoleUser, false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	h := srv.Handler()
	cookie := templateLibrarySession(t, srv, owner.ID, store.RoleUser)

	rec := doEndpointRequest(t, h, cookie, "POST", "/api/templates/nonexistent/reset", "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("reset nonexistent status = %d, want 404", rec.Code)
	}
}

// TestResetTemplateCrossUserIsolation verifies a user cannot reset another user's template.
func TestResetTemplateCrossUserIsolation(t *testing.T) {
	srv, st := newEndpointTestServer(t, endpointTestPool())

	const anyHash = "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"
	owner, err := st.CreateUser("reset-cross-owner", anyHash, store.RoleUser, false)
	if err != nil {
		t.Fatalf("CreateUser owner: %v", err)
	}
	alice, err := st.CreateUser("reset-cross-alice", anyHash, store.RoleUser, false)
	if err != nil {
		t.Fatalf("CreateUser alice: %v", err)
	}

	// Owner creates a template
	if _, err := st.CreateTemplate(owner.ID, "owners-tmpl", "port: 7890"); err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}

	h := srv.Handler()
	aliceCookie := templateLibrarySession(t, srv, alice.ID, store.RoleUser)

	// Alice tries to reset owner's template
	rec := doEndpointRequest(t, h, aliceCookie, "POST", "/api/templates/owners-tmpl/reset", "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("cross-user reset status = %d, want 404", rec.Code)
	}
}
