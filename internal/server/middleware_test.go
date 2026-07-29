package server

import (
	"encoding/json"
	"net/http"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/taliove/proxyhub/internal/store"
)

// TestRequireMFAEnrolled_OptionalBypassesGate asserts the dev-only escape
// hatch: with server.mfa_optional set, an unenrolled session passes the gate
// and login responses no longer flag must_enroll_mfa.
func TestRequireMFAEnrolled_OptionalBypassesGate(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	srv.cfg.Server.MFAOptional = true
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")
	cookie, _ := unenrolledSession(t, srv, h, "rookie", "init-pass-1", store.RoleUser)

	if rec := mfaRequest(t, h, cookie, "GET", "/api/endpoints", ""); rec.Code != http.StatusOK {
		t.Errorf("GET /api/endpoints status = %d, want 200 with mfa_optional (body: %s)",
			rec.Code, rec.Body.String())
	}
	if rec := mfaRequest(t, h, cookie, "GET", "/api/me", ""); rec.Code == http.StatusOK {
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode /api/me body: %v", err)
		}
		if enroll, _ := body["must_enroll_mfa"].(bool); enroll {
			t.Errorf("/api/me must_enroll_mfa = true, want false with mfa_optional")
		}
	}
}

// TestRequireMFAEnrolled_GatesBusinessRoutes asserts the enforcement contract:
// an authenticated but unenrolled session is refused on business routes with
// 403 + must_enroll_mfa, while every route the enrollment page itself needs
// stays reachable (otherwise the gate locks the user out of clearing it).
func TestRequireMFAEnrolled_GatesBusinessRoutes(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")
	cookie, _ := unenrolledSession(t, srv, h, "rookie", "init-pass-1", store.RoleUser)

	rec := mfaRequest(t, h, cookie, "GET", "/api/endpoints", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("GET /api/endpoints status = %d, want 403 while unenrolled (body: %s)",
			rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode 403 body: %v", err)
	}
	if enroll, _ := body["must_enroll_mfa"].(bool); !enroll {
		t.Errorf("403 body = %v, want must_enroll_mfa: true", body)
	}

	// Exempt surface: the enrollment endpoint, reading own state, changing own
	// password, and logging out.
	if rec := mfaRequest(t, h, cookie, "POST", "/api/me/mfa/enroll", `{}`); rec.Code != http.StatusOK {
		t.Errorf("POST /api/me/mfa/enroll status = %d, want 200 (exempt)", rec.Code)
	}
	if rec := mfaRequest(t, h, cookie, "GET", "/api/me", ""); rec.Code != http.StatusOK {
		t.Errorf("GET /api/me status = %d, want 200 (exempt)", rec.Code)
	}
	if rec := mfaRequest(t, h, cookie, "POST", "/api/me/password",
		`{"old_password":"wrong","new_password":"new-pass-123"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("POST /api/me/password status = %d, want 400 (endpoint reachable)", rec.Code)
	}
	if rec := mfaRequest(t, h, cookie, "POST", "/api/logout", ""); rec.Code != http.StatusOK {
		t.Errorf("POST /api/logout status = %d, want 200 (exempt)", rec.Code)
	}
}

// TestRequireMFAEnrolled_AllowsAfterEnrollment asserts the gate opens as soon
// as enrollment completes, on the same session.
func TestRequireMFAEnrolled_AllowsAfterEnrollment(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")
	cookie, _ := unenrolledSession(t, srv, h, "rookie", "init-pass-1", store.RoleUser)

	enrollMFA(t, h, cookie)

	if rec := mfaRequest(t, h, cookie, "GET", "/api/endpoints", ""); rec.Code != http.StatusOK {
		t.Fatalf("GET /api/endpoints status = %d, want 200 after enrollment (body: %s)",
			rec.Code, rec.Body.String())
	}
}

// TestRequireMFAEnrolled_GatesAdminRoutes asserts the admin chain carries the
// same gate: an unenrolled super admin gets 403 must_enroll_mfa rather than
// walking straight into the admin surface.
func TestRequireMFAEnrolled_GatesAdminRoutes(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")
	cookie, _ := unenrolledSession(t, srv, h, "boss", "init-pass-1", store.RoleSuperAdmin)

	rec := mfaRequest(t, h, cookie, "GET", "/api/admin/users", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("GET /api/admin/users status = %d, want 403 while unenrolled", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode 403 body: %v", err)
	}
	if enroll, _ := body["must_enroll_mfa"].(bool); !enroll {
		t.Errorf("admin 403 body = %v, want must_enroll_mfa: true", body)
	}

	enrollMFA(t, h, cookie)
	if rec := mfaRequest(t, h, cookie, "GET", "/api/admin/users", ""); rec.Code != http.StatusOK {
		t.Errorf("GET /api/admin/users status = %d, want 200 after enrollment", rec.Code)
	}
}

// TestRequireMFAEnrolled_ReflectsAdminReset asserts the gate reads the users
// table rather than trusting the session: a super-admin reset must re-gate a
// still-live session immediately.
func TestRequireMFAEnrolled_ReflectsAdminReset(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")
	cookie, userID := unenrolledSession(t, srv, h, "rookie", "init-pass-1", store.RoleUser)
	enrollMFA(t, h, cookie)

	if rec := mfaRequest(t, h, cookie, "GET", "/api/endpoints", ""); rec.Code != http.StatusOK {
		t.Fatalf("precondition: GET /api/endpoints status = %d, want 200", rec.Code)
	}
	if err := st.ResetUserMFA(userID); err != nil {
		t.Fatalf("ResetUserMFA: %v", err)
	}

	rec := mfaRequest(t, h, cookie, "GET", "/api/endpoints", "")
	if rec.Code != http.StatusForbidden {
		t.Errorf("GET /api/endpoints status = %d, want 403 after MFA reset", rec.Code)
	}
}

// TestRequireMFAEnrolled_RequiresAuth asserts the gate never evaluates without
// an authenticated scope: no cookie is 401, not 403.
func TestRequireMFAEnrolled_RequiresAuth(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")

	cases := []struct{ method, path string }{
		{"GET", "/api/endpoints"},
		{"GET", "/api/admin/users"},
		// Exempt from the MFA gate, but not from authentication.
		{"POST", "/api/me/mfa/enroll"},
		{"POST", "/api/me/mfa/regenerate-recovery"},
	}
	for _, c := range cases {
		if rec := mfaRequest(t, h, nil, c.method, c.path, ""); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without session status = %d, want 401", c.method, c.path, rec.Code)
		}
	}
}

// TestRequireMFAEnrolled_PasswordGateTakesPrecedence asserts the two forced
// flows compose: a user owing both a password change and enrollment is sent to
// the password change first (that gate runs earlier in the chain).
func TestRequireMFAEnrolled_PasswordGateTakesPrecedence(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")

	hash := mustHashPassword(t, "init-pass-1")
	if _, err := srv.st.CreateUser("rookie", hash, store.RoleUser, true); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	w := doLogin(t, h, "rookie", "init-pass-1", "9.9.9.31")
	if w.Code != http.StatusOK {
		t.Fatalf("login status = %d", w.Code)
	}
	var cookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "session" {
			cookie = c
		}
	}

	rec := mfaRequest(t, h, cookie, "GET", "/api/endpoints", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if change, _ := body["must_change_password"].(bool); !change {
		t.Errorf("body = %v, want must_change_password: true to win over the MFA gate", body)
	}
}

// TestRequireMFAEnrolled_ExemptPathsAreExact guards the exemption list against
// prefix-matching mistakes: a business route that merely starts with an exempt
// path must still be gated.
func TestRequireMFAEnrolled_ExemptPathsAreExact(t *testing.T) {
	if mfaExemptPaths["/api/endpoints"] {
		t.Error("/api/endpoints must not be exempt from the MFA gate")
	}
	for _, want := range []string{
		"/api/me", "/api/me/password", "/api/me/mfa/enroll", "/api/logout",
	} {
		if !mfaExemptPaths[want] {
			t.Errorf("%s missing from mfaExemptPaths", want)
		}
	}
}

// mustHashPassword bcrypt-hashes pw for fixtures that need a known password.
func mustHashPassword(t *testing.T, pw string) string {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	return string(hash)
}
