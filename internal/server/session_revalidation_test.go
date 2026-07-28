package server

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/taliove/proxyhub/internal/config"
	"github.com/taliove/proxyhub/internal/store"
)

// Regression tests for the go-reviewer H1/H2/M2/M4/M5 findings: session
// claims are re-validated online against the users table, and credential or
// status changes revoke live sessions immediately.

// TestRequireAuth_DisabledUserSessionRejected (H1): a session whose user gets
// disabled must fail on the very next request, not live out its TTL.
func TestRequireAuth_DisabledUserSessionRejected(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")

	memberID := seedRegularUser(t, st, "member1", "member-pass-12ch")
	cookie := memberSession(t, srv, memberID)

	if err := st.DisableUser(memberID, time.Now()); err != nil {
		t.Fatalf("DisableUser: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/me", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 for disabled user's session", w.Code)
	}
}

// TestRequireAuth_DeletedUserSessionRejected (H1 sibling): a deleted user's
// session must fail even on admin routes (which lack requirePasswordChanged's
// own DB re-read).
func TestRequireAuth_DeletedUserSessionRejected(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")

	secondID := seedSuperAdmin(t, st)
	cookie := adminSession(t, srv, secondID)

	if err := st.DeleteUser(secondID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/admin/users", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 for deleted user's session", w.Code)
	}
}

// TestRequireAuth_RoleDowngradeEffectiveImmediately (H2): demoting a super
// admin must strip admin access on their live session without re-login.
func TestRequireAuth_RoleDowngradeEffectiveImmediately(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")

	secondID := seedSuperAdmin(t, st)
	cookie := adminSession(t, srv, secondID)

	demoted := store.RoleUser
	if err := st.UpdateUser(secondID, store.UserUpdate{Role: &demoted}); err != nil {
		t.Fatalf("UpdateUser role: %v", err)
	}

	w := serveAdminHTTP(t, srv, cookie, http.MethodGet, "/api/admin/users", nil)
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 after role downgrade", w.Code)
	}
}

// TestAdminDisableUser_RevokesTargetSessions (M4): disabling via the admin
// API destroys the target's sessions, not just their next-request check.
func TestAdminDisableUser_RevokesTargetSessions(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")
	owner, err := st.GetUserByUsername("owner")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	adminCookie := adminSession(t, srv, owner.ID)

	memberID := seedRegularUser(t, st, "member1", "member-pass-12ch")
	memberCookie := memberSession(t, srv, memberID)

	w := serveAdminHTTP(t, srv, adminCookie, http.MethodPost,
		"/api/admin/users/"+strconv.FormatInt(memberID, 10)+"/disable", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("disable status = %d (body: %s)", w.Code, w.Body.String())
	}

	if _, ok := srv.sessions.Lookup(memberCookie.Value); ok {
		t.Error("target session still alive after admin disable")
	}
}

// TestAdminResetPassword_RevokesTargetSessions (M4): a stolen session must
// die when the admin rotates the credential.
func TestAdminResetPassword_RevokesTargetSessions(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")
	owner, err := st.GetUserByUsername("owner")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	adminCookie := adminSession(t, srv, owner.ID)

	memberID := seedRegularUser(t, st, "member1", "member-pass-12ch")
	memberCookie := memberSession(t, srv, memberID)

	w := serveAdminHTTP(t, srv, adminCookie, http.MethodPost,
		"/api/admin/users/"+strconv.FormatInt(memberID, 10)+"/reset-password", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("reset-password status = %d (body: %s)", w.Code, w.Body.String())
	}

	if _, ok := srv.sessions.Lookup(memberCookie.Value); ok {
		t.Error("target session still alive after password reset")
	}
}

// TestChangeMyPassword_RevokesAllOwnSessions (M4): changing one's own
// password revokes every session of that user, not just the current one.
func TestChangeMyPassword_RevokesAllOwnSessions(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")

	// Two independent sessions for the same user (e.g. two browsers).
	first := doLogin(t, h, "owner", "a-very-strong-pass", "9.9.9.30")
	second := doLogin(t, h, "owner", "a-very-strong-pass", "9.9.9.31")
	cookieOf := func(w *httptest.ResponseRecorder) *http.Cookie {
		t.Helper()
		for _, c := range w.Result().Cookies() {
			if c.Name == "session" {
				return c
			}
		}
		t.Fatal("no session cookie")
		return nil
	}
	c1, c2 := cookieOf(first), cookieOf(second)

	// Change password via the first session.
	req := httptest.NewRequest("POST", "/api/me/password",
		strings.NewReader(`{"old_password":"a-very-strong-pass","new_password":"brand-new-pass1"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(c1)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("change password status = %d (body: %s)", w.Code, w.Body.String())
	}

	if _, ok := srv.sessions.Lookup(c2.Value); ok {
		t.Error("second session still alive after own password change")
	}
}

// TestAdminRoutes_GatedByPasswordChange (M2): a super admin whose account is
// flagged must_change_password is locked out of the admin surface too, while
// the change-password entry point stays reachable.
func TestAdminRoutes_GatedByPasswordChange(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")

	owner, err := st.GetUserByUsername("owner")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	flag := true
	if err := st.UpdateUser(owner.ID, store.UserUpdate{MustChangePassword: &flag}); err != nil {
		t.Fatalf("UpdateUser must_change: %v", err)
	}

	login := doLogin(t, h, "owner", "a-very-strong-pass", "9.9.9.32")
	var cookie *http.Cookie
	for _, c := range login.Result().Cookies() {
		if c.Name == "session" {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("no session cookie from login")
	}

	w := serveAdminHTTP(t, srv, cookie, http.MethodGet, "/api/admin/users", nil)
	if w.Code != http.StatusForbidden {
		t.Errorf("GET /api/admin/users status = %d, want 403 before password change", w.Code)
	}

	req := httptest.NewRequest("POST", "/api/me/password",
		strings.NewReader(`{"old_password":"a-very-strong-pass","new_password":"brand-new-pass1"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("change password status = %d, want 200 (exempt)", rec.Code)
	}
}

// TestClientIP_TrustedProxyOnly (M5): X-Forwarded-For / X-Real-IP are honored
// only when the peer is a trusted reverse proxy (loopback by default, or the
// CIDRs declared in server.trusted_proxies); direct clients cannot spoof.
func TestClientIP_TrustedProxyOnly(t *testing.T) {
	cases := []struct {
		name       string
		remoteAddr string
		xff        string
		xri        string
		want       string
	}{
		{"loopback proxy honors xff", "127.0.0.1:8080", "1.2.3.4, 10.0.0.1", "", "1.2.3.4"},
		{"loopback proxy honors xri", "127.0.0.1:8080", "", "5.6.7.8", "5.6.7.8"},
		{"direct client cannot spoof xff", "9.9.9.9:1234", "127.0.0.1", "", "9.9.9.9"},
		{"direct client cannot spoof xri", "9.9.9.9:1234", "", "127.0.0.1", "9.9.9.9"},
		{"no headers uses remote addr", "9.9.9.9:1234", "", "", "9.9.9.9"},
	}
	srv := &Server{cfg: &config.Config{}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = tc.remoteAddr
			if tc.xff != "" {
				req.Header.Set("X-Forwarded-For", tc.xff)
			}
			if tc.xri != "" {
				req.Header.Set("X-Real-IP", tc.xri)
			}
			if got := srv.clientIP(req); got != tc.want {
				t.Errorf("clientIP = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestClientIP_ConfiguredTrustedProxies: once server.trusted_proxies is set it
// replaces the loopback default - including the empty list, which trusts no
// peer at all (the directly-exposed deployment posture).
func TestClientIP_ConfiguredTrustedProxies(t *testing.T) {
	cases := []struct {
		name     string
		proxies  []string
		peer     string
		xff      string
		want     string
	}{
		{"declared cidr is trusted", []string{"10.0.0.0/8"}, "10.1.2.3:80", "1.2.3.4", "1.2.3.4"},
		{"loopback no longer trusted once configured", []string{"10.0.0.0/8"}, "127.0.0.1:80", "1.2.3.4", "127.0.0.1"},
		{"single ip without mask", []string{"10.9.9.9"}, "10.9.9.9:80", "1.2.3.4", "1.2.3.4"},
		{"empty list trusts nobody", []string{}, "127.0.0.1:80", "1.2.3.4", "127.0.0.1"},
		{"garbage entry is skipped", []string{"not-a-cidr", "10.0.0.0/8"}, "10.1.2.3:80", "1.2.3.4", "1.2.3.4"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := &Server{cfg: &config.Config{}}
			srv.cfg.Server.TrustedProxies = tc.proxies
			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = tc.peer
			req.Header.Set("X-Forwarded-For", tc.xff)
			if got := srv.clientIP(req); got != tc.want {
				t.Errorf("clientIP = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestIsDirectLoopback: only a header-less loopback connection counts as a
// direct local client. A forwarded 127.0.0.1 (caller-controlled) must never
// qualify for any loopback exemption.
func TestIsDirectLoopback(t *testing.T) {
	cases := []struct {
		name       string
		remoteAddr string
		xff        string
		xri        string
		want       bool
	}{
		{"plain loopback", "127.0.0.1:8080", "", "", true},
		{"ipv6 loopback", "[::1]:8080", "", "", true},
		{"loopback with xff is not direct", "127.0.0.1:8080", "127.0.0.1", "", false},
		{"loopback with xri is not direct", "127.0.0.1:8080", "", "9.9.9.9", false},
		{"remote peer", "9.9.9.9:1234", "", "", false},
		{"unparsable peer", "garbage", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = tc.remoteAddr
			if tc.xff != "" {
				req.Header.Set("X-Forwarded-For", tc.xff)
			}
			if tc.xri != "" {
				req.Header.Set("X-Real-IP", tc.xri)
			}
			if got := isDirectLoopback(req); got != tc.want {
				t.Errorf("isDirectLoopback = %v, want %v", got, tc.want)
			}
		})
	}
}
