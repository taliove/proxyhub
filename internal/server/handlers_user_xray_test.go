package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/xraymgr"
)

// seedXrayUser creates a user with a quota row and returns its id.
func seedXrayUser(t *testing.T, st *store.Store, username string, portStart, portEnd int) int64 {
	t.Helper()
	u, err := st.CreateUser(username, "x", store.RoleUser, false)
	if err != nil {
		t.Fatalf("CreateUser(%s) error = %v", username, err)
	}
	if err := st.UpsertUserQuota(&store.UserQuota{
		UserID:        u.ID,
		XrayPortStart: portStart,
		XrayPortEnd:   portEnd,
	}); err != nil {
		t.Fatalf("UpsertUserQuota() error = %v", err)
	}
	return u.ID
}

// seedSuperAdmin inserts a super admin user (ticket 01's MigrateAdminToSuperUser
// is a no-op when the settings KV is absent, so tests seed directly).
func seedSuperAdmin(t *testing.T, st *store.Store) int64 {
	t.Helper()
	u, err := st.CreateUser("boss", "x", store.RoleSuperAdmin, false)
	if err != nil {
		t.Fatalf("CreateUser(super) error = %v", err)
	}
	return u.ID
}

// newXrayStubManager builds an xraymgr.Manager with a stub xray binary that
// sleeps forever. Tests assert lifecycle behavior, not real xray semantics.
func newXrayStubManager(t *testing.T, srv *Server, st *store.Store) *xraymgr.Manager {
	t.Helper()
	tmp := t.TempDir()
	binPath := filepath.Join(tmp, "xray-stub.sh")
	stub := "#!/bin/sh\nexec sleep 3600\n"
	if err := os.WriteFile(binPath, []byte(stub), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	mgr, err := xraymgr.New(xraymgr.Config{
		Store:   st,
		XrayBin: binPath,
		WorkDir: filepath.Join(tmp, "xray"),
	})
	if err != nil {
		t.Fatalf("xraymgr.New: %v", err)
	}
	srv.SetXrayManager(mgr)
	return mgr
}

// authedRequest issues an HTTP request with a valid session cookie.
// serveXrayHTTP routes through the full Handler() chain so requireAuth and
// method routing run exactly as in production.
func serveXrayHTTP(t *testing.T, srv *Server, method, path string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	token, err := srv.sessions.Create()
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, path, bytes.NewReader(body))
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	req.AddCookie(&http.Cookie{Name: "session", Value: token})
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	return w
}

// TestGetMyXray_NotConfigured verifies the handler returns 404 when the user
// has no Xray instance yet (not 500, not unauthorized).
func TestGetMyXray_NotConfigured(t *testing.T) {
	srv, st := newTestServer(t, nil)
	_ = seedSuperAdmin(t, st)
	newXrayStubManager(t, srv, st)

	w := serveXrayHTTP(t, srv, http.MethodGet, "/api/me/xray", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", w.Code, w.Body.String())
	}
}

// TestGetMyXray_Unauthorized verifies no session -> 401.
func TestGetMyXray_Unauthorized(t *testing.T) {
	srv, st := newTestServer(t, nil)
	_ = seedSuperAdmin(t, st)
	newXrayStubManager(t, srv, st)

	req := httptest.NewRequest(http.MethodGet, "/api/me/xray", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

// TestGetMyXray_ReturnsRunningInstance covers the happy path: Start the
// user's Xray via the manager, then read status via /api/me/xray.
func TestGetMyXray_ReturnsRunningInstance(t *testing.T) {
	srv, st := newTestServer(t, nil)
	superID := seedSuperAdmin(t, st)
	mgr := newXrayStubManager(t, srv, st)

	// Give the super admin a port range and start their Xray.
	if err := st.UpsertUserQuota(&store.UserQuota{
		UserID: superID, XrayPortStart: 31000, XrayPortEnd: 31010,
	}); err != nil {
		t.Fatalf("UpsertUserQuota: %v", err)
	}
	if _, err := mgr.Start(context.Background(), superID); err != nil {
		t.Fatalf("mgr.Start: %v", err)
	}
	defer func() { _ = mgr.Stop(context.Background(), superID) }()

	w := serveXrayHTTP(t, srv, http.MethodGet, "/api/me/xray", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var view xrayStatusView
	if err := json.NewDecoder(w.Body).Decode(&view); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if view.UserID != superID {
		t.Errorf("UserID = %d, want %d", view.UserID, superID)
	}
	if view.Status != "running" {
		t.Errorf("Status = %q, want running", view.Status)
	}
	if view.Port != 31000 {
		t.Errorf("Port = %d, want 31000", view.Port)
	}
	if !view.ProcessAlive {
		t.Errorf("ProcessAlive = false, want true")
	}
}

// TestAdminGetUserXray verifies the admin endpoint reads any user's status.
func TestAdminGetUserXray(t *testing.T) {
	srv, st := newTestServer(t, nil)
	_ = seedSuperAdmin(t, st)
	mgr := newXrayStubManager(t, srv, st)

	userID := seedXrayUser(t, st, "regular", 32000, 32010)
	if _, err := mgr.Start(context.Background(), userID); err != nil {
		t.Fatalf("mgr.Start: %v", err)
	}
	defer func() { _ = mgr.Stop(context.Background(), userID) }()

	w := serveXrayHTTP(t, srv, http.MethodGet,
		"/api/admin/users/"+strconv.FormatInt(userID, 10)+"/xray", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var view xrayStatusView
	if err := json.NewDecoder(w.Body).Decode(&view); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if view.UserID != userID {
		t.Errorf("UserID = %d, want %d", view.UserID, userID)
	}
	if view.Port != 32000 {
		t.Errorf("Port = %d, want 32000", view.Port)
	}
}

// TestAdminRestartUserXray verifies the restart endpoint bounces the process.
func TestAdminRestartUserXray(t *testing.T) {
	srv, st := newTestServer(t, nil)
	_ = seedSuperAdmin(t, st)
	mgr := newXrayStubManager(t, srv, st)

	userID := seedXrayUser(t, st, "restartme", 33000, 33010)
	st1, err := mgr.Start(context.Background(), userID)
	if err != nil {
		t.Fatalf("mgr.Start: %v", err)
	}
	defer func() { _ = mgr.Stop(context.Background(), userID) }()

	w := serveXrayHTTP(t, srv, http.MethodPost,
		"/api/admin/users/"+strconv.FormatInt(userID, 10)+"/xray/restart", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var view xrayStatusView
	if err := json.NewDecoder(w.Body).Decode(&view); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if view.PID == st1.PID {
		t.Errorf("PID unchanged after restart: %d", view.PID)
	}
	if view.Port != st1.Port {
		t.Errorf("Port changed after restart: %d -> %d, want stable", st1.Port, view.Port)
	}
	if view.Status != "running" {
		t.Errorf("Status = %q, want running", view.Status)
	}
}

// TestAdminGetUserXray_InvalidID verifies a non-numeric id is a 400.
func TestAdminGetUserXray_InvalidID(t *testing.T) {
	srv, st := newTestServer(t, nil)
	_ = seedSuperAdmin(t, st)
	newXrayStubManager(t, srv, st)

	w := serveXrayHTTP(t, srv, http.MethodGet, "/api/admin/users/notanumber/xray", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}
