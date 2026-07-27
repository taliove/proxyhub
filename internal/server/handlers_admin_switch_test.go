package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/taliove/proxyhub/internal/store"
)

// TestAdminSwitchUser_EntersTargetUserSpace covers the happy path: super
// admin switches to a normal user, the response carries the target's
// profile, and the session persists acting_user_id so current-view reports
// the impersonated identity.
func TestAdminSwitchUser_EntersTargetUserSpace(t *testing.T) {
	srv, st := newTestServer(t, nil)
	superID := seedSuperAdmin(t, st)
	targetID := seedXrayUser(t, st, "alice", 31000, 31010)
	cookie := adminSession(t, srv, superID)

	w := serveAdminHTTP(t, srv, cookie, http.MethodPost, "/api/admin/switch-user", map[string]any{
		"user_id": targetID,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var view adminUserView
	if err := json.NewDecoder(w.Body).Decode(&view); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if view.ID != targetID {
		t.Errorf("view.id = %d, want %d", view.ID, targetID)
	}
	if view.Username != "alice" {
		t.Errorf("view.username = %q, want alice", view.Username)
	}

	// Session must persist the acting target.
	payload, ok := srv.sessions.Lookup(cookie.Value)
	if !ok {
		t.Fatal("session missing after switch")
	}
	if payload.ActingUserID != targetID {
		t.Errorf("session acting_user_id = %d, want %d", payload.ActingUserID, targetID)
	}

	// Current view reflects the impersonation.
	w2 := serveAdminHTTP(t, srv, cookie, http.MethodGet, "/api/admin/current-view", nil)
	if w2.Code != http.StatusOK {
		t.Fatalf("current-view status = %d; body=%s", w2.Code, w2.Body.String())
	}
	var cv adminCurrentViewResponse
	if err := json.NewDecoder(w2.Body).Decode(&cv); err != nil {
		t.Fatalf("decode current-view: %v", err)
	}
	if !cv.Acting {
		t.Error("current-view acting = false, want true")
	}
	if cv.ActingUserID != targetID {
		t.Errorf("current-view acting_user_id = %d, want %d", cv.ActingUserID, targetID)
	}
	if cv.ActingUsername != "alice" {
		t.Errorf("current-view acting_username = %q, want alice", cv.ActingUsername)
	}
	if cv.Profile.ID != targetID {
		t.Errorf("current-view profile.id = %d, want %d", cv.Profile.ID, targetID)
	}
	if cv.Username != "boss" {
		t.Errorf("current-view username (self) = %q, want boss", cv.Username)
	}
}

// TestAdminSwitchUser_UnknownTargetReturns404 rejects switches to users
// that do not exist.
func TestAdminSwitchUser_UnknownTargetReturns404(t *testing.T) {
	srv, st := newTestServer(t, nil)
	superID := seedSuperAdmin(t, st)
	cookie := adminSession(t, srv, superID)

	w := serveAdminHTTP(t, srv, cookie, http.MethodPost, "/api/admin/switch-user", map[string]any{
		"user_id": 99999,
	})
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", w.Code, w.Body.String())
	}
}

// TestAdminSwitchUser_RejectsInvalidBody guards against malformed payloads.
func TestAdminSwitchUser_RejectsInvalidBody(t *testing.T) {
	srv, st := newTestServer(t, nil)
	superID := seedSuperAdmin(t, st)
	cookie := adminSession(t, srv, superID)

	w := serveAdminHTTP(t, srv, cookie, http.MethodPost, "/api/admin/switch-user", map[string]any{
		"user_id": 0,
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// TestAdminSwitchUser_SelfIsNoOp: switching to oneself clears any active
// impersonation (treated as exit).
func TestAdminSwitchUser_SelfIsNoOp(t *testing.T) {
	srv, st := newTestServer(t, nil)
	superID := seedSuperAdmin(t, st)
	targetID := seedXrayUser(t, st, "alice", 31000, 31010)
	cookie := adminSession(t, srv, superID)

	// First switch into alice's space.
	w := serveAdminHTTP(t, srv, cookie, http.MethodPost, "/api/admin/switch-user", map[string]any{
		"user_id": targetID,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("first switch status = %d", w.Code)
	}
	// Now switch to self: should clear acting target.
	w2 := serveAdminHTTP(t, srv, cookie, http.MethodPost, "/api/admin/switch-user", map[string]any{
		"user_id": superID,
	})
	if w2.Code != http.StatusOK {
		t.Fatalf("self-switch status = %d; body=%s", w2.Code, w2.Body.String())
	}
	payload, _ := srv.sessions.Lookup(cookie.Value)
	if payload.ActingUserID != 0 {
		t.Errorf("acting_user_id = %d, want 0 after self-switch", payload.ActingUserID)
	}
}

// TestAdminExitSwitch_ClearsActingTarget: exit restores the admin's own
// scope and current-view reports acting=false.
func TestAdminExitSwitch_ClearsActingTarget(t *testing.T) {
	srv, st := newTestServer(t, nil)
	superID := seedSuperAdmin(t, st)
	targetID := seedXrayUser(t, st, "alice", 31000, 31010)
	cookie := adminSession(t, srv, superID)

	// Enter alice's space first.
	_ = serveAdminHTTP(t, srv, cookie, http.MethodPost, "/api/admin/switch-user", map[string]any{
		"user_id": targetID,
	})

	w := serveAdminHTTP(t, srv, cookie, http.MethodPost, "/api/admin/exit-switch", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("exit status = %d; body=%s", w.Code, w.Body.String())
	}
	payload, _ := srv.sessions.Lookup(cookie.Value)
	if payload.ActingUserID != 0 {
		t.Errorf("acting_user_id = %d, want 0 after exit", payload.ActingUserID)
	}

	w2 := serveAdminHTTP(t, srv, cookie, http.MethodGet, "/api/admin/current-view", nil)
	if w2.Code != http.StatusOK {
		t.Fatalf("current-view status = %d", w2.Code)
	}
	var cv adminCurrentViewResponse
	if err := json.NewDecoder(w2.Body).Decode(&cv); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if cv.Acting {
		t.Error("current-view acting = true after exit, want false")
	}
	if cv.Profile.ID != superID {
		t.Errorf("current-view profile.id = %d, want %d (self)", cv.Profile.ID, superID)
	}
}

// TestAdminExitSwitch_Idempotent: exiting without an active switch is a
// 200 no-op (safe for the navbar to call on page load).
func TestAdminExitSwitch_Idempotent(t *testing.T) {
	srv, st := newTestServer(t, nil)
	superID := seedSuperAdmin(t, st)
	cookie := adminSession(t, srv, superID)

	w := serveAdminHTTP(t, srv, cookie, http.MethodPost, "/api/admin/exit-switch", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
}

// TestAdminSwitch_OrdinaryUserForbidden: every switch endpoint is gated by
// requireAdmin; ordinary users get a stable 403.
func TestAdminSwitch_OrdinaryUserForbidden(t *testing.T) {
	srv, st := newTestServer(t, nil)
	_ = seedSuperAdmin(t, st)
	memberID := seedRegularUser(t, st, "bob", "Password1")
	memberCookie := memberSession(t, srv, memberID)

	cases := []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodPost, "/api/admin/switch-user", map[string]any{"user_id": 1}},
		{http.MethodPost, "/api/admin/exit-switch", nil},
		{http.MethodGet, "/api/admin/current-view", nil},
	}
	for _, tc := range cases {
		w := serveAdminHTTP(t, srv, memberCookie, tc.method, tc.path, tc.body)
		if w.Code != http.StatusForbidden {
			t.Errorf("%s %s: status = %d, want 403; body=%s", tc.method, tc.path, w.Code, w.Body.String())
		}
	}
}

// TestAdminCurrentView_WithoutActingReturnsSelf: when no impersonation is
// active, current-view reports the admin's own profile with acting=false.
func TestAdminCurrentView_WithoutActingReturnsSelf(t *testing.T) {
	srv, st := newTestServer(t, nil)
	superID := seedSuperAdmin(t, st)
	cookie := adminSession(t, srv, superID)

	w := serveAdminHTTP(t, srv, cookie, http.MethodGet, "/api/admin/current-view", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", w.Code, w.Body.String())
	}
	var cv adminCurrentViewResponse
	if err := json.NewDecoder(w.Body).Decode(&cv); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if cv.Acting {
		t.Error("acting = true, want false with no active switch")
	}
	if cv.Profile.ID != superID || cv.Profile.Role != store.RoleSuperAdmin {
		t.Errorf("profile = %+v, want self super_admin", cv.Profile)
	}
	if cv.Username != "boss" {
		t.Errorf("username = %q, want boss", cv.Username)
	}
}

// TestAdminSwitchUser_DisabledTargetRejected: entering a disabled user's
// space is refused (admin should enable the account first).
func TestAdminSwitchUser_DisabledTargetRejected(t *testing.T) {
	srv, st := newTestServer(t, nil)
	superID := seedSuperAdmin(t, st)
	targetID := seedXrayUser(t, st, "alice", 31000, 31010)
	cookie := adminSession(t, srv, superID)

	// Disable alice via the existing admin endpoint.
	w := serveAdminHTTP(t, srv, cookie, http.MethodPost,
		"/api/admin/users/"+strconv.FormatInt(targetID, 10)+"/disable", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("disable status = %d", w.Code)
	}

	w2 := serveAdminHTTP(t, srv, cookie, http.MethodPost, "/api/admin/switch-user", map[string]any{
		"user_id": targetID,
	})
	if w2.Code != http.StatusConflict {
		t.Fatalf("switch status = %d, want 409; body=%s", w2.Code, w2.Body.String())
	}
}
