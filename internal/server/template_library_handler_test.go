package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/taliove/proxyhub/internal/store"
)

// templateLibrarySession issues a session cookie for the given user without
// touching the login path (hash content is irrelevant here).
func templateLibrarySession(t *testing.T, srv *Server, userID int64, role string) *http.Cookie {
	t.Helper()
	token, err := srv.sessions.CreateWithPayload(SessionPayload{UserID: userID, Role: role})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	return &http.Cookie{Name: "session", Value: token}
}

// TestTemplateLibraryHTTPCrossUserIsolation verifies at the HTTP layer that a
// regular user cannot see or operate another user's templates (store-level
// isolation is covered in the store package).
func TestTemplateLibraryHTTPCrossUserIsolation(t *testing.T) {
	srv, st := newEndpointTestServer(t, endpointTestPool())

	const anyHash = "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"
	owner, err := st.CreateUser("tpl-owner", anyHash, store.RoleUser, false)
	if err != nil {
		t.Fatalf("CreateUser owner: %v", err)
	}
	alice, err := st.CreateUser("tpl-alice", anyHash, store.RoleUser, false)
	if err != nil {
		t.Fatalf("CreateUser alice: %v", err)
	}

	if _, err := st.CreateTemplate(owner.ID, "secret", "port: 7890"); err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}

	h := srv.Handler()
	aliceCookie := templateLibrarySession(t, srv, alice.ID, store.RoleUser)

	// Alice cannot read owner's template
	rec := doEndpointRequest(t, h, aliceCookie, "GET", "/api/templates/secret", "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("cross-user GET status = %d, want 404", rec.Code)
	}
	// Alice cannot update owner's template
	rec = doEndpointRequest(t, h, aliceCookie, "PUT", "/api/templates/secret", `{"content":"port: 1"}`)
	if rec.Code != http.StatusNotFound {
		t.Errorf("cross-user PUT status = %d, want 404", rec.Code)
	}
	// Alice cannot delete owner's template
	rec = doEndpointRequest(t, h, aliceCookie, "DELETE", "/api/templates/secret", "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("cross-user DELETE status = %d, want 404", rec.Code)
	}
	// Alice cannot mark owner's template as default
	rec = doEndpointRequest(t, h, aliceCookie, "PUT", "/api/templates/secret/default", "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("cross-user set-default status = %d, want 404", rec.Code)
	}
	// Alice's list must not contain owner's template
	rec = doEndpointRequest(t, h, aliceCookie, "GET", "/api/templates", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "secret") {
		t.Errorf("cross-user list leaked template: %s", rec.Body.String())
	}
}

// TestTemplateLibrarySuperAdminOwnLibrary verifies that a super admin who is
// NOT impersonating manages their own library (their endpoints render with
// their own user id), instead of being rejected as "global scope".
func TestTemplateLibrarySuperAdminOwnLibrary(t *testing.T) {
	srv, st := newEndpointTestServer(t, endpointTestPool())

	const anyHash = "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"
	admin, err := st.CreateUser("tpl-admin", anyHash, store.RoleSuperAdmin, false)
	if err != nil {
		t.Fatalf("CreateUser admin: %v", err)
	}

	h := srv.Handler()
	cookie := templateLibrarySession(t, srv, admin.ID, store.RoleSuperAdmin)

	// Create in own library
	rec := doEndpointRequest(t, h, cookie, "POST", "/api/templates", `{"name":"admin-tmpl","content":"port: 7890"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("super admin create status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}

	// Listed in own library
	rec = doEndpointRequest(t, h, cookie, "GET", "/api/templates", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "admin-tmpl") {
		t.Fatalf("super admin list missing own template: status=%d body=%s", rec.Code, rec.Body.String())
	}

	// Lands in the admin's own user space
	tmpl, err := st.GetTemplateByName(admin.ID, "admin-tmpl")
	if err != nil || tmpl == nil {
		t.Fatalf("template not stored under admin's user id: %v", err)
	}
}


// TestTemplateLibraryQuotaHTTP verifies the quota contract at the HTTP layer:
// exceeding max_templates returns 403 with the "template quota exceeded" error
// body the frontend keys on.
func TestTemplateLibraryQuotaHTTP(t *testing.T) {
	srv, st := newEndpointTestServer(t, endpointTestPool())

	const anyHash = "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"
	owner, err := st.CreateUser("quota-owner", anyHash, store.RoleUser, false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	userID := owner.ID

	if err := st.UpsertUserQuota(&store.UserQuota{UserID: userID, MaxAirports: 10, MaxEndpoints: 10, MaxTemplates: 1}); err != nil {
		t.Fatalf("upsert quota: %v", err)
	}
	if _, err := st.CreateTemplate(userID, "first", "port: 7890"); err != nil {
		t.Fatalf("create first template: %v", err)
	}

	h := srv.Handler()
	cookie := templateLibrarySession(t, srv, userID, store.RoleUser)
	rec := doEndpointRequest(t, h, cookie, "POST", "/api/templates", `{"name":"second","content":"port: 7891"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("over-quota create status = %d, want 403, body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "template quota exceeded") {
		t.Errorf("quota error body should contain 'template quota exceeded', got: %s", rec.Body.String())
	}
}
// TestSubscriptionSoftReferenceHTTP verifies end-to-end over HTTP that deleting
// a template bound to an endpoint leaves /sub rendering (fallback to the user
// default), never a 5xx.
func TestSubscriptionSoftReferenceHTTP(t *testing.T) {
	srv, st := newEndpointTestServer(t, endpointTestPool())
	userID := int64(1)

	if _, err := st.CreateTemplate(userID, "fallback-tmpl", `port: 64000
proxy-groups:
  - name: FALLBACK-MARKER
    type: select
    proxies:
      - '{{nodes}}'`); err != nil {
		t.Fatalf("create default template: %v", err)
	}
	if err := st.SetDefaultTemplate(userID, "fallback-tmpl"); err != nil {
		t.Fatalf("set default: %v", err)
	}
	if _, err := st.CreateTemplate(userID, "bound-tmpl", `port: 64001
proxy-groups:
  - name: BOUND-MARKER
    type: select
    proxies:
      - '{{nodes}}'`); err != nil {
		t.Fatalf("create bound template: %v", err)
	}

	ep, err := st.CreateEndpointForUser(userID, "soft-ref-ep")
	if err != nil {
		t.Fatalf("create endpoint: %v", err)
	}
	if err := st.UpdateEndpointTemplate(userID, ep.ID, "bound-tmpl"); err != nil {
		t.Fatalf("bind template: %v", err)
	}

	h := srv.Handler()
	subURL := fmt.Sprintf("/sub/%s?token=%s&format=clash", ep.Path, ep.Token)

	// Bound template renders
	req := httptest.NewRequest("GET", subURL, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("sub status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "BOUND-MARKER") {
		t.Errorf("expected bound template marker, got:\n%s", rec.Body.String())
	}

	// Delete the bound template; /sub must keep rendering via fallback
	if err := st.DeleteTemplate(userID, "bound-tmpl"); err != nil {
		t.Fatalf("delete template: %v", err)
	}
	req = httptest.NewRequest("GET", subURL, nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("sub after delete status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "FALLBACK-MARKER") {
		t.Errorf("expected fallback template marker after delete, got:\n%s", rec.Body.String())
	}
}
