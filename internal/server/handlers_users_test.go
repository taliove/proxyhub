package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/taliove/proxyhub/internal/store"
)

// adminSession returns a session cookie scoped to the given (already-persisted)
// super admin. Sessions must carry identity payload so requireAdmin can read
// the role claim; the legacy fallback always resolves to super admin so we
// cannot use it to test ordinary-user rejection.
func adminSession(t *testing.T, srv *Server, userID int64) *http.Cookie {
	t.Helper()
	markMFAEnrolled(t, srv.st, userID)
	token, err := srv.sessions.CreateWithPayload(SessionPayload{
		UserID: userID,
		Role:   store.RoleSuperAdmin,
	})
	if err != nil {
		t.Fatalf("create admin session: %v", err)
	}
	return &http.Cookie{Name: "session", Value: token}
}

// memberSession returns a session cookie for an ordinary user. Used to
// assert that the admin surface rejects non-super-admin callers with 403.
func memberSession(t *testing.T, srv *Server, userID int64) *http.Cookie {
	t.Helper()
	markMFAEnrolled(t, srv.st, userID)
	token, err := srv.sessions.CreateWithPayload(SessionPayload{
		UserID: userID,
		Role:   store.RoleUser,
	})
	if err != nil {
		t.Fatalf("create member session: %v", err)
	}
	return &http.Cookie{Name: "session", Value: token}
}

// serveAdminHTTP issues an authenticated request through the full Handler()
// chain so requireAuth + requireAdmin + method routing all run exactly as in
// production.
func serveAdminHTTP(t *testing.T, srv *Server, cookie *http.Cookie, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		req = httptest.NewRequest(method, path, bytes.NewReader(buf))
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	return w
}

// seedRegularUser inserts an ordinary user with the given password and
// returns the user id. Helper for tests that need a non-admin identity.
func seedRegularUser(t *testing.T, st *store.Store, username, password string) int64 {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	u, err := st.CreateUser(username, string(hash), store.RoleUser, false)
	if err != nil {
		t.Fatalf("CreateUser(%s): %v", username, err)
	}
	return u.ID
}

// TestAdminUsers_SuperAdminListsAllUsers verifies the super admin sees the
// full user list with quota and usage counts.
func TestAdminUsers_SuperAdminListsAllUsers(t *testing.T) {
	srv, st := newTestServer(t, nil)
	superID := seedSuperAdmin(t, st)
	adminCookie := adminSession(t, srv, superID)

	userID := seedXrayUser(t, st, "alice", 31000, 31010)

	w := serveAdminHTTP(t, srv, adminCookie, http.MethodGet, "/api/admin/users", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var list []adminUserView
	if err := json.NewDecoder(w.Body).Decode(&list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("len = %d, want 2 (super admin + alice)", len(list))
	}
	var alice *adminUserView
	for i := range list {
		if list[i].ID == userID {
			alice = &list[i]
		}
	}
	if alice == nil {
		t.Fatalf("alice (id=%d) not in list: %+v", userID, list)
	}
	if alice.Role != store.RoleUser {
		t.Errorf("alice.role = %q, want user", alice.Role)
	}
	if alice.Quota == nil {
		t.Fatal("alice.quota = nil, want port range from seedXrayUser")
	}
	if alice.Quota.XrayPortStart != 31000 || alice.Quota.XrayPortEnd != 31010 {
		t.Errorf("alice quota ports = %d-%d, want 31000-31010",
			alice.Quota.XrayPortStart, alice.Quota.XrayPortEnd)
	}
}

// TestAdminUsers_RegularUserForbidden asserts every admin endpoint rejects
// an ordinary user session with 403.
func TestAdminUsers_RegularUserForbidden(t *testing.T) {
	srv, st := newTestServer(t, nil)
	superID := seedSuperAdmin(t, st)
	memberID := seedRegularUser(t, st, "member", "member-pass-1")
	memberCookie := memberSession(t, srv, memberID)

	endpoints := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/admin/users"},
		{http.MethodPost, "/api/admin/users"},
		{http.MethodGet, fmt.Sprintf("/api/admin/users/%d", superID)},
		{http.MethodPut, fmt.Sprintf("/api/admin/users/%d", superID)},
		{http.MethodPost, fmt.Sprintf("/api/admin/users/%d/disable", superID)},
		{http.MethodPost, fmt.Sprintf("/api/admin/users/%d/enable", superID)},
		{http.MethodDelete, fmt.Sprintf("/api/admin/users/%d", superID)},
		{http.MethodPost, fmt.Sprintf("/api/admin/users/%d/reset-password", superID)},
	}
	for _, ep := range endpoints {
		t.Run(ep.method+" "+ep.path, func(t *testing.T) {
			w := serveAdminHTTP(t, srv, memberCookie, ep.method, ep.path, nil)
			if w.Code != http.StatusForbidden {
				t.Errorf("status = %d, want 403; body=%s", w.Code, w.Body.String())
			}
		})
	}
}

// TestAdminUsers_UnauthenticatedRejected verifies no session = 401.
func TestAdminUsers_UnauthenticatedRejected(t *testing.T) {
	srv, st := newTestServer(t, nil)
	_ = seedSuperAdmin(t, st)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/users", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

// TestAdminCreateUser_HappyPath verifies the create endpoint lands a user
// with a bcrypt-hashed password, must_change_password=true, and the quota row.
func TestAdminCreateUser_HappyPath(t *testing.T) {
	srv, st := newTestServer(t, nil)
	superID := seedSuperAdmin(t, st)
	adminCookie := adminSession(t, srv, superID)

	body := map[string]any{
		"username":        "newbie",
		"password":        "abc12345",
		"max_airports":    3,
		"max_endpoints":   5,
		"xray_port_start": 34000,
		"xray_port_end":   34010,
	}
	w := serveAdminHTTP(t, srv, adminCookie, http.MethodPost, "/api/admin/users", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", w.Code, w.Body.String())
	}
	var view adminUserView
	if err := json.NewDecoder(w.Body).Decode(&view); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if view.Username != "newbie" {
		t.Errorf("username = %q, want newbie", view.Username)
	}
	if view.Role != store.RoleUser {
		t.Errorf("role = %q, want user (default)", view.Role)
	}
	if !view.MustChangePassword {
		t.Error("must_change_password = false, want true for admin-created account")
	}
	if view.Quota == nil || view.Quota.MaxAirports != 3 || view.Quota.MaxEndpoints != 5 {
		t.Errorf("quota = %+v, want 3/5", view.Quota)
	}

	// Password is hashed and matches the supplied plaintext.
	u, err := st.GetUserByUsername("newbie")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PassHash), []byte("abc12345")) != nil {
		t.Error("stored pass_hash does not match supplied password")
	}
}

// TestAdminCreateUser_RejectsReservedUsername covers the honeypot list:
// admin/root must be refused.
func TestAdminCreateUser_RejectsReservedUsername(t *testing.T) {
	srv, st := newTestServer(t, nil)
	superID := seedSuperAdmin(t, st)
	adminCookie := adminSession(t, srv, superID)

	for _, name := range []string{"admin", "root"} {
		body := map[string]any{"username": name, "password": "abc12345"}
		w := serveAdminHTTP(t, srv, adminCookie, http.MethodPost, "/api/admin/users", body)
		if w.Code != http.StatusBadRequest {
			t.Errorf("username %q: status = %d, want 400; body=%s", name, w.Code, w.Body.String())
		}
	}
}

// TestAdminCreateUser_RejectsWeakPassword covers the complexity rule:
// 8+ chars, must contain both letters and digits.
func TestAdminCreateUser_RejectsWeakPassword(t *testing.T) {
	srv, st := newTestServer(t, nil)
	superID := seedSuperAdmin(t, st)
	adminCookie := adminSession(t, srv, superID)

	cases := []struct {
		name string
		pw   string
	}{
		{"too short", "abc123"},
		{"letters only", "abcdefgh"},
		{"digits only", "12345678"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := map[string]any{"username": "u_" + tc.name, "password": tc.pw}
			w := serveAdminHTTP(t, srv, adminCookie, http.MethodPost, "/api/admin/users", body)
			if w.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400; body=%s", w.Code, w.Body.String())
			}
		})
	}
}

// TestAdminCreateUser_DuplicateUsername returns 409 on a UNIQUE violation.
func TestAdminCreateUser_DuplicateUsername(t *testing.T) {
	srv, st := newTestServer(t, nil)
	superID := seedSuperAdmin(t, st)
	adminCookie := adminSession(t, srv, superID)

	_ = seedRegularUser(t, st, "taken", "abc12345")
	body := map[string]any{"username": "taken", "password": "xyz98765"}
	w := serveAdminHTTP(t, srv, adminCookie, http.MethodPost, "/api/admin/users", body)
	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409; body=%s", w.Code, w.Body.String())
	}
}

// TestAdminGetUser verifies fetching one user by id returns its quota.
func TestAdminGetUser(t *testing.T) {
	srv, st := newTestServer(t, nil)
	superID := seedSuperAdmin(t, st)
	adminCookie := adminSession(t, srv, superID)

	userID := seedXrayUser(t, st, "bob", 32000, 32010)
	w := serveAdminHTTP(t, srv, adminCookie, http.MethodGet,
		fmt.Sprintf("/api/admin/users/%d", userID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var view adminUserView
	if err := json.NewDecoder(w.Body).Decode(&view); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if view.Username != "bob" {
		t.Errorf("username = %q, want bob", view.Username)
	}
	if view.Quota == nil || view.Quota.XrayPortStart != 32000 {
		t.Errorf("quota = %+v, want port_start=32000", view.Quota)
	}
}

// TestAdminGetUser_NotFound returns 404 for an unknown id.
func TestAdminGetUser_NotFound(t *testing.T) {
	srv, st := newTestServer(t, nil)
	superID := seedSuperAdmin(t, st)
	adminCookie := adminSession(t, srv, superID)

	w := serveAdminHTTP(t, srv, adminCookie, http.MethodGet, "/api/admin/users/9999", nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// TestAdminUpdateUser_QuotaAndRole covers the PUT endpoint's two jobs:
// patch quota fields and flip role. Pass hash is untouched.
func TestAdminUpdateUser_QuotaAndRole(t *testing.T) {
	srv, st := newTestServer(t, nil)
	superID := seedSuperAdmin(t, st)
	adminCookie := adminSession(t, srv, superID)

	userID := seedRegularUser(t, st, "carol", "carol-pass-1")
	newRole := store.RoleSuperAdmin
	newMax := 7
	body := map[string]any{
		"role":         newRole,
		"max_airports": newMax,
	}
	w := serveAdminHTTP(t, srv, adminCookie, http.MethodPut,
		fmt.Sprintf("/api/admin/users/%d", userID), body)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}

	u, err := st.GetUserByID(userID)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if u.Role != store.RoleSuperAdmin {
		t.Errorf("role = %q, want super_admin", u.Role)
	}
	quota, err := st.GetUserQuota(userID)
	if err != nil {
		t.Fatalf("GetUserQuota: %v", err)
	}
	if quota.MaxAirports != 7 {
		t.Errorf("max_airports = %d, want 7", quota.MaxAirports)
	}
	// Password is unchanged (bcrypt still matches original).
	if bcrypt.CompareHashAndPassword([]byte(u.PassHash), []byte("carol-pass-1")) != nil {
		t.Error("pass_hash unexpectedly changed by PUT")
	}
}

// TestAdminDisableEnableUser exercises the disable/enable pair. Disable
// must call xraymgr.HandleUserDisabled (verified by the running instance
// stopping); enable must clear the disabled marker.
func TestAdminDisableEnableUser(t *testing.T) {
	srv, st := newTestServer(t, nil)
	superID := seedSuperAdmin(t, st)
	adminCookie := adminSession(t, srv, superID)

	mgr := newXrayStubManager(t, srv, st)
	userID := seedXrayUser(t, st, "dave", 33000, 33010)
	if _, err := mgr.Start(t.Context(), userID); err != nil {
		t.Fatalf("mgr.Start: %v", err)
	}

	// Disable: process stops, disabled_at set.
	w := serveAdminHTTP(t, srv, adminCookie, http.MethodPost,
		fmt.Sprintf("/api/admin/users/%d/disable", userID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("disable status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	u, _ := st.GetUserByID(userID)
	if !u.Disabled() {
		t.Error("user.disabled = false after /disable")
	}
	inst, err := st.GetXrayInstanceByUserID(userID)
	if err != nil {
		t.Fatalf("GetXrayInstanceByUserID: %v", err)
	}
	if inst.Status != "stopped" {
		t.Errorf("xray status = %q, want stopped after disable", inst.Status)
	}

	// Enable: disabled marker cleared.
	w = serveAdminHTTP(t, srv, adminCookie, http.MethodPost,
		fmt.Sprintf("/api/admin/users/%d/enable", userID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("enable status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	u, _ = st.GetUserByID(userID)
	if u.Disabled() {
		t.Error("user.disabled = true after /enable")
	}
}

// TestAdminDeleteUser_Cascade verifies DELETE physically removes the user
// plus every per-user row (airports/endpoints/quota/xray), while audit_logs
// are preserved.
func TestAdminDeleteUser_Cascade(t *testing.T) {
	srv, st := newTestServer(t, nil)
	superID := seedSuperAdmin(t, st)
	adminCookie := adminSession(t, srv, superID)

	mgr := newXrayStubManager(t, srv, st)
	userID := seedXrayUser(t, st, "erin", 35000, 35010)
	if _, err := mgr.Start(t.Context(), userID); err != nil {
		t.Fatalf("mgr.Start: %v", err)
	}

	// Seed per-user resources so cascade has something to remove.
	if _, err := st.CreateAirportForUser(userID, "erin-airport", "https://example.com/sub"); err != nil {
		t.Fatalf("CreateAirportForUser: %v", err)
	}
	if _, err := st.CreateEndpointForUser(userID, "erin-endpoint"); err != nil {
		t.Fatalf("CreateEndpointForUser: %v", err)
	}

	w := serveAdminHTTP(t, srv, adminCookie, http.MethodDelete,
		fmt.Sprintf("/api/admin/users/%d", userID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("delete status = %d, want 200; body=%s", w.Code, w.Body.String())
	}

	if _, err := st.GetUserByID(userID); err == nil {
		t.Error("user still present after DELETE")
	}
	if _, err := st.GetUserQuota(userID); err == nil {
		t.Error("quota still present after DELETE")
	}
	if _, err := st.GetXrayInstanceByUserID(userID); err == nil {
		t.Error("xray instance still present after DELETE")
	}
	// Airports/endpoints belonging to the user are gone. ListAirports includes
	// all users' rows, so filter by user_id.
	airports, err := st.ListAirports()
	if err != nil {
		t.Fatalf("ListAirports: %v", err)
	}
	for _, a := range airports {
		if a.UserID == userID {
			t.Errorf("airport id=%d still owned by deleted user %d", a.ID, userID)
		}
	}
	endpoints, err := st.ListEndpoints()
	if err != nil {
		t.Fatalf("ListEndpoints: %v", err)
	}
	for _, ep := range endpoints {
		if ep.UserID == userID {
			t.Errorf("endpoint id=%d still owned by deleted user %d", ep.ID, userID)
		}
	}
}

// TestAdminDeleteUser_CannotDeleteSelf refuses to delete the calling super
// admin — that would strand the deployment with no way back in.
func TestAdminDeleteUser_CannotDeleteSelf(t *testing.T) {
	srv, st := newTestServer(t, nil)
	superID := seedSuperAdmin(t, st)
	adminCookie := adminSession(t, srv, superID)

	w := serveAdminHTTP(t, srv, adminCookie, http.MethodDelete,
		fmt.Sprintf("/api/admin/users/%d", superID), nil)
	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409; body=%s", w.Code, w.Body.String())
	}
	// Sanity: the super admin row is still there.
	if _, err := st.GetUserByID(superID); err != nil {
		t.Errorf("super admin unexpectedly missing after self-delete attempt: %v", err)
	}
}

// TestAdminResetPassword covers the reset endpoint: a 16-char alphanumeric
// password is returned, the stored hash matches it, and must_change_password
// is set so the next login forces a change.
func TestAdminResetPassword(t *testing.T) {
	srv, st := newTestServer(t, nil)
	superID := seedSuperAdmin(t, st)
	adminCookie := adminSession(t, srv, superID)

	userID := seedRegularUser(t, st, "frank", "old-pass-12")
	w := serveAdminHTTP(t, srv, adminCookie, http.MethodPost,
		fmt.Sprintf("/api/admin/users/%d/reset-password", userID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		OK       bool   `json:"ok"`
		Password string `json:"password"`
		UserID   int64  `json:"user_id"`
		Username string `json:"username"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.OK {
		t.Error("ok = false")
	}
	if len(resp.Password) != 16 {
		t.Errorf("password length = %d, want 16", len(resp.Password))
	}
	for _, r := range resp.Password {
		isAlphaNum := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if !isAlphaNum {
			t.Errorf("password contains non-alphanumeric char %q", r)
		}
	}

	u, err := st.GetUserByID(userID)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if !u.MustChangePassword {
		t.Error("must_change_password = false after reset, want true")
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PassHash), []byte(resp.Password)) != nil {
		t.Error("stored pass_hash does not match returned password")
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PassHash), []byte("old-pass-12")) == nil {
		t.Error("old password still matches after reset")
	}
}

// TestAdminUsers_InvalidIDRejected verifies a non-numeric id is a 400
// across all parameterized endpoints.
func TestAdminUsers_InvalidIDRejected(t *testing.T) {
	srv, st := newTestServer(t, nil)
	superID := seedSuperAdmin(t, st)
	adminCookie := adminSession(t, srv, superID)

	endpoints := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/admin/users/notanumber"},
		{http.MethodPut, "/api/admin/users/notanumber"},
		{http.MethodPost, "/api/admin/users/notanumber/disable"},
		{http.MethodPost, "/api/admin/users/notanumber/enable"},
		{http.MethodDelete, "/api/admin/users/notanumber"},
		{http.MethodPost, "/api/admin/users/notanumber/reset-password"},
	}
	for _, ep := range endpoints {
		t.Run(ep.method+" "+ep.path, func(t *testing.T) {
			w := serveAdminHTTP(t, srv, adminCookie, ep.method, ep.path, nil)
			if w.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400; body=%s", w.Code, w.Body.String())
			}
		})
	}
}

// TestParseAdminUserID_ZeroRejected pins the "positive int64" rule.
func TestParseAdminUserID_ZeroRejected(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/admin/users/0", nil)
	req.SetPathValue("id", "0")
	if _, err := parseAdminUserID(req); err == nil {
		t.Error("parseAdminUserID(0) = nil error, want invalid user id")
	}
	req = httptest.NewRequest(http.MethodGet, "/api/admin/users/-5", nil)
	req.SetPathValue("id", "-5")
	if _, err := parseAdminUserID(req); err == nil {
		t.Error("parseAdminUserID(-5) = nil error, want invalid user id")
	}
}

// TestValidateNewPassword pins the shared complexity rule (8+ chars, letter
// and digit; validateNewPassword in handlers_me.go) in isolation so future
// tweaks break loudly. Admin create-user and self-serve change both use it.
func TestValidateNewPassword(t *testing.T) {
	cases := []struct {
		pw      string
		wantErr bool
	}{
		{"abc12345", false},
		{"ABC123xy", false},
		{"abc123", true},   // too short
		{"abcdefgh", true}, // no digit
		{"12345678", true}, // no letter
		{"", true},         // empty
	}
	for _, tc := range cases {
		t.Run(strconv.Itoa(len(tc.pw))+"_"+tc.pw, func(t *testing.T) {
			err := validateNewPassword(tc.pw)
			if tc.wantErr && err == nil {
				t.Errorf("validateNewPassword(%q) = nil, want error", tc.pw)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("validateNewPassword(%q) = %v, want nil", tc.pw, err)
			}
		})
	}
}

// TestGeneratePassword_Format asserts the reset-password generator's
// contract (16 chars, alphanumeric).
func TestGeneratePassword_Format(t *testing.T) {
	pw, err := generatePassword()
	if err != nil {
		t.Fatalf("generatePassword: %v", err)
	}
	if len(pw) != 16 {
		t.Errorf("len = %d, want 16", len(pw))
	}
	for _, r := range pw {
		isAlphaNum := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if !isAlphaNum {
			t.Errorf("non-alphanumeric char %q in %q", r, pw)
		}
	}
}
