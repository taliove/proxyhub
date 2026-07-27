package server

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/taliove/proxyhub/internal/store"
)

// TestHandleListVersions tests GET /api/templates/{name}/versions
func TestHandleListVersions(t *testing.T) {
	srv, st := newEndpointTestServer(t, endpointTestPool())

	const anyHash = "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"
	user, err := st.CreateUser("test-user", anyHash, store.RoleUser, false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	// Create template and make some updates to generate versions
	st.CreateTemplate(user.ID, "my-template", "v1-content")
	st.UpdateTemplate(user.ID, "my-template", "v2-content")
	st.UpdateTemplate(user.ID, "my-template", "v3-content")

	h := srv.Handler()
	cookie := templateLibrarySession(t, srv, user.ID, store.RoleUser)

	// Test: list versions
	rec := doEndpointRequest(t, h, cookie, "GET", "/api/templates/my-template/versions", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Versions []struct {
			Version   int    `json:"version"`
			CreatedAt string `json:"created_at"`
		} `json:"versions"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(resp.Versions) != 3 {
		t.Fatalf("expected 3 versions, got %d", len(resp.Versions))
	}

	// Should be descending order (newest first)
	if resp.Versions[0].Version != 3 || resp.Versions[1].Version != 2 || resp.Versions[2].Version != 1 {
		t.Errorf("expected versions [3,2,1], got [%d,%d,%d]",
			resp.Versions[0].Version, resp.Versions[1].Version, resp.Versions[2].Version)
	}

	// CreatedAt should be non-empty
	if resp.Versions[0].CreatedAt == "" {
		t.Error("created_at should not be empty")
	}
}

// TestHandleListVersionsNotFound tests 404 for non-existent template
func TestHandleListVersionsNotFound(t *testing.T) {
	srv, st := newEndpointTestServer(t, endpointTestPool())

	const anyHash = "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"
	user, _ := st.CreateUser("test-user", anyHash, store.RoleUser, false)

	h := srv.Handler()
	cookie := templateLibrarySession(t, srv, user.ID, store.RoleUser)
	rec := doEndpointRequest(t, h, cookie, "GET", "/api/templates/nonexistent/versions", "")

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

// TestHandleListVersionsCrossUser tests user isolation
func TestHandleListVersionsCrossUser(t *testing.T) {
	srv, st := newEndpointTestServer(t, endpointTestPool())

	const anyHash = "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"
	user1, _ := st.CreateUser("user1", anyHash, store.RoleUser, false)
	user2, _ := st.CreateUser("user2", anyHash, store.RoleUser, false)

	// User1 creates template
	st.CreateTemplate(user1.ID, "template", "content")

	h := srv.Handler()
	user2Cookie := templateLibrarySession(t, srv, user2.ID, store.RoleUser)

	// User2 tries to list user1's versions
	rec := doEndpointRequest(t, h, user2Cookie, "GET", "/api/templates/template/versions", "")

	if rec.Code != http.StatusNotFound {
		t.Errorf("cross-user access should return 404, got %d", rec.Code)
	}
}

// TestHandleGetVersionContent tests GET /api/templates/{name}/versions/{version}
func TestHandleGetVersionContent(t *testing.T) {
	srv, st := newEndpointTestServer(t, endpointTestPool())

	const anyHash = "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"
	user, _ := st.CreateUser("test-user", anyHash, store.RoleUser, false)

	st.CreateTemplate(user.ID, "template", "v1-content")
	st.UpdateTemplate(user.ID, "template", "v2-content")

	h := srv.Handler()
	cookie := templateLibrarySession(t, srv, user.ID, store.RoleUser)

	// Get version 1
	rec := doEndpointRequest(t, h, cookie, "GET", "/api/templates/template/versions/1", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Version   int    `json:"version"`
		Content   string `json:"content"`
		CreatedAt string `json:"created_at"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.Version != 1 {
		t.Errorf("expected version 1, got %d", resp.Version)
	}
	if resp.Content != "v1-content" {
		t.Errorf("expected v1-content, got %q", resp.Content)
	}
	if resp.CreatedAt == "" {
		t.Error("created_at should not be empty")
	}

	// Get version 2
	rec2 := doEndpointRequest(t, h, cookie, "GET", "/api/templates/template/versions/2", "")

	var resp2 struct {
		Content string `json:"content"`
	}
	json.NewDecoder(rec2.Body).Decode(&resp2)
	if resp2.Content != "v2-content" {
		t.Errorf("expected v2-content, got %q", resp2.Content)
	}
}

// TestHandleGetVersionContentNotFound tests 404 cases
func TestHandleGetVersionContentNotFound(t *testing.T) {
	srv, st := newEndpointTestServer(t, endpointTestPool())

	const anyHash = "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"
	user, _ := st.CreateUser("test-user", anyHash, store.RoleUser, false)

	st.CreateTemplate(user.ID, "template", "v1-content")

	h := srv.Handler()
	cookie := templateLibrarySession(t, srv, user.ID, store.RoleUser)

	tests := []struct {
		name     string
		template string
		version  string
	}{
		{"nonexistent template", "nonexistent", "1"},
		{"nonexistent version", "template", "999"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doEndpointRequest(t, h, cookie, "GET", "/api/templates/"+tt.template+"/versions/"+tt.version, "")

			if rec.Code != http.StatusNotFound {
				t.Errorf("expected 404, got %d", rec.Code)
			}
		})
	}
}

// TestHandleGetVersionContentInvalidVersion tests invalid version parameter
func TestHandleGetVersionContentInvalidVersion(t *testing.T) {
	srv, st := newEndpointTestServer(t, endpointTestPool())

	const anyHash = "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"
	user, _ := st.CreateUser("test-user", anyHash, store.RoleUser, false)

	st.CreateTemplate(user.ID, "template", "content")

	h := srv.Handler()
	cookie := templateLibrarySession(t, srv, user.ID, store.RoleUser)
	rec := doEndpointRequest(t, h, cookie, "GET", "/api/templates/template/versions/invalid", "")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("invalid version should return 400, got %d", rec.Code)
	}
}

// TestHandleGetVersionContentCrossUser tests user isolation
func TestHandleGetVersionContentCrossUser(t *testing.T) {
	srv, st := newEndpointTestServer(t, endpointTestPool())

	const anyHash = "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"
	user1, _ := st.CreateUser("user1", anyHash, store.RoleUser, false)
	user2, _ := st.CreateUser("user2", anyHash, store.RoleUser, false)

	st.CreateTemplate(user1.ID, "template", "user1-content")

	h := srv.Handler()
	user2Cookie := templateLibrarySession(t, srv, user2.ID, store.RoleUser)

	// User2 tries to access user1's version
	rec := doEndpointRequest(t, h, user2Cookie, "GET", "/api/templates/template/versions/1", "")

	if rec.Code != http.StatusNotFound {
		t.Errorf("cross-user access should return 404, got %d", rec.Code)
	}
}
