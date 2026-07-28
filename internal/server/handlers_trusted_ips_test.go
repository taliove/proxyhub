package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/taliove/proxyhub/internal/store"
)

// Trusted IP management surface (login hardening ticket 10).

// trustedIPsResponse mirrors the GET /api/me/trusted-ips envelope.
type trustedIPsResponse struct {
	Trusted []struct {
		IP         string    `json:"ip"`
		ExpiresAt  time.Time `json:"expires_at"`
		LastUsedAt time.Time `json:"last_used_at"`
		Expired    bool      `json:"expired"`
		RegionCode string    `json:"region_code"`
		RegionName string    `json:"region_name"`
	} `json:"trusted"`
	Recommendations []struct {
		IP           string `json:"ip"`
		MFASuccesses int    `json:"mfa_successes"`
		RegionCode   string `json:"region_code"`
		RegionName   string `json:"region_name"`
	} `json:"recommendations"`
	AutoTrustIP bool `json:"auto_trust_ip"`
	Threshold   int  `json:"threshold"`
}

// trustedIPRequest issues an authenticated request through the full Handler()
// chain from a fixed source address.
func trustedIPRequest(t *testing.T, srv *Server, cookie *http.Cookie, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	req.RemoteAddr = "9.9.9.60:4000"
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

// decodeTrustedIPs reads the list envelope, failing the test on a non-200.
func decodeTrustedIPs(t *testing.T, rec *httptest.ResponseRecorder) trustedIPsResponse {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	var resp trustedIPsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode trusted ips: %v (body: %s)", err, rec.Body.String())
	}
	return resp
}

// seedMFALogins books n login_success rows carrying the mfa= marker that the
// recommendation engine counts.
func seedMFALogins(t *testing.T, st *store.Store, username, ip string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if err := st.RecordAuditEvent("login_success", ip, username, "mfa=totp"); err != nil {
			t.Fatalf("RecordAuditEvent: %v", err)
		}
	}
}

// TestTrustedIPs_ListShowsGrantsWithExpiryAndGeo covers the management view:
// active and expired grants are both listed, the expired flag distinguishes
// them, and the geo columns come from the (stubbed) offline lookup.
func TestTrustedIPs_ListShowsGrantsWithExpiryAndGeo(t *testing.T) {
	srv, st := newTestServer(t, nil)
	userID := seedRegularUser(t, st, "alice", "init-pass-1")
	cookie := memberSession(t, srv, userID)
	srv.countryLookup = func(string) (string, error) { return "HK", nil }

	if err := st.AddTrustedIP(userID, "203.0.113.7"); err != nil {
		t.Fatalf("AddTrustedIP: %v", err)
	}

	resp := decodeTrustedIPs(t, trustedIPRequest(t, srv, cookie, http.MethodGet, "/api/me/trusted-ips", ""))
	if len(resp.Trusted) != 1 {
		t.Fatalf("trusted len = %d, want 1 (body: %+v)", len(resp.Trusted), resp.Trusted)
	}
	row := resp.Trusted[0]
	if row.IP != "203.0.113.7" {
		t.Errorf("ip = %q, want 203.0.113.7", row.IP)
	}
	if row.Expired {
		t.Error("expired = true for a fresh grant, want false")
	}
	if row.RegionCode != "HK" || row.RegionName == "" {
		t.Errorf("geo = (%q,%q), want HK with a Chinese name", row.RegionCode, row.RegionName)
	}
	if row.ExpiresAt.IsZero() || row.LastUsedAt.IsZero() {
		t.Errorf("timestamps not serialized: expires=%v last_used=%v", row.ExpiresAt, row.LastUsedAt)
	}
	if !row.ExpiresAt.After(time.Now().UTC()) {
		t.Errorf("expires_at = %v, want in the future", row.ExpiresAt)
	}
	if resp.AutoTrustIP {
		t.Error("auto_trust_ip = true by default, want false")
	}
	if resp.Threshold != trustRecommendationThreshold {
		t.Errorf("threshold = %d, want %d", resp.Threshold, trustRecommendationThreshold)
	}
}

// TestTrustedIPs_ListIsPerUser one user's grants must never leak into another's
// management view.
func TestTrustedIPs_ListIsPerUser(t *testing.T) {
	srv, st := newTestServer(t, nil)
	aliceID := seedRegularUser(t, st, "alice", "init-pass-1")
	bobID := seedRegularUser(t, st, "bob", "init-pass-2")
	srv.countryLookup = func(string) (string, error) { return "", nil }

	if err := st.AddTrustedIP(bobID, "198.51.100.9"); err != nil {
		t.Fatalf("AddTrustedIP: %v", err)
	}

	resp := decodeTrustedIPs(t, trustedIPRequest(t, srv,
		memberSession(t, srv, aliceID), http.MethodGet, "/api/me/trusted-ips", ""))
	if len(resp.Trusted) != 0 {
		t.Errorf("alice sees %d grants, want 0 (bob's grant leaked)", len(resp.Trusted))
	}
}

// TestTrustedIPs_RevokeDropsGrant revoking removes the row, and IsTrustedIP
// (the login-path predicate) stops matching - the next login gets challenged.
func TestTrustedIPs_RevokeDropsGrant(t *testing.T) {
	srv, st := newTestServer(t, nil)
	userID := seedRegularUser(t, st, "alice", "init-pass-1")
	cookie := memberSession(t, srv, userID)
	srv.countryLookup = func(string) (string, error) { return "", nil }

	const ip = "203.0.113.7"
	if err := st.AddTrustedIP(userID, ip); err != nil {
		t.Fatalf("AddTrustedIP: %v", err)
	}

	rec := trustedIPRequest(t, srv, cookie, http.MethodDelete, "/api/me/trusted-ips/"+ip, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("revoke status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	trusted, err := st.IsTrustedIP(userID, ip)
	if err != nil {
		t.Fatalf("IsTrustedIP: %v", err)
	}
	if trusted {
		t.Error("IsTrustedIP = true after revoke, want false (next login must be challenged)")
	}
	assertAuditEvent(t, st, "trusted_ip_revoked", "alice")

	// Idempotent: revoking again is still a 200.
	if again := trustedIPRequest(t, srv, cookie, http.MethodDelete, "/api/me/trusted-ips/"+ip, ""); again.Code != http.StatusOK {
		t.Errorf("second revoke status = %d, want 200", again.Code)
	}
}

// TestTrustedIPs_RecommendationAtThreshold an address with three real MFA
// logins shows up as a recommendation; two logins do not, and an already
// trusted address drops off the list.
func TestTrustedIPs_RecommendationAtThreshold(t *testing.T) {
	srv, st := newTestServer(t, nil)
	userID := seedRegularUser(t, st, "alice", "init-pass-1")
	cookie := memberSession(t, srv, userID)
	srv.countryLookup = func(string) (string, error) { return "JP", nil }

	const familiar = "203.0.113.20"
	const occasional = "203.0.113.21"
	seedMFALogins(t, st, "alice", familiar, trustRecommendationThreshold)
	seedMFALogins(t, st, "alice", occasional, trustRecommendationThreshold-1)
	// Trusted-IP logins must not feed the engine back into itself.
	if err := st.RecordAuditEvent("login_success", "203.0.113.22", "alice", "mfa_skipped=trusted_ip"); err != nil {
		t.Fatalf("RecordAuditEvent: %v", err)
	}

	resp := decodeTrustedIPs(t, trustedIPRequest(t, srv, cookie, http.MethodGet, "/api/me/trusted-ips", ""))
	if len(resp.Recommendations) != 1 {
		t.Fatalf("recommendations = %+v, want only %s", resp.Recommendations, familiar)
	}
	rec := resp.Recommendations[0]
	if rec.IP != familiar {
		t.Errorf("recommendation ip = %q, want %q", rec.IP, familiar)
	}
	if rec.MFASuccesses != trustRecommendationThreshold {
		t.Errorf("mfa_successes = %d, want %d", rec.MFASuccesses, trustRecommendationThreshold)
	}
	if rec.RegionCode != "JP" {
		t.Errorf("region_code = %q, want JP", rec.RegionCode)
	}

	// Adopt it: the address moves from recommendations into the trusted list.
	adopt := trustedIPRequest(t, srv, cookie, http.MethodPost, "/api/me/trusted-ips",
		fmt.Sprintf(`{"ip":%q}`, familiar))
	if adopt.Code != http.StatusOK {
		t.Fatalf("adopt status = %d, want 200 (body: %s)", adopt.Code, adopt.Body.String())
	}
	trusted, err := st.IsTrustedIP(userID, familiar)
	if err != nil {
		t.Fatalf("IsTrustedIP: %v", err)
	}
	if !trusted {
		t.Fatal("IsTrustedIP = false after adopting the recommendation")
	}
	after := decodeTrustedIPs(t, trustedIPRequest(t, srv, cookie, http.MethodGet, "/api/me/trusted-ips", ""))
	if len(after.Recommendations) != 0 {
		t.Errorf("recommendations after adopting = %+v, want empty", after.Recommendations)
	}
	if len(after.Trusted) != 1 || after.Trusted[0].IP != familiar {
		t.Errorf("trusted after adopting = %+v, want just %s", after.Trusted, familiar)
	}
}

// TestTrustedIPs_AdoptBelowThresholdRejected the adopt endpoint must not be a
// self-service way to exempt an arbitrary address from MFA.
func TestTrustedIPs_AdoptBelowThresholdRejected(t *testing.T) {
	srv, st := newTestServer(t, nil)
	userID := seedRegularUser(t, st, "alice", "init-pass-1")
	cookie := memberSession(t, srv, userID)
	srv.countryLookup = func(string) (string, error) { return "", nil }

	const stranger = "203.0.113.30"
	rec := trustedIPRequest(t, srv, cookie, http.MethodPost, "/api/me/trusted-ips",
		fmt.Sprintf(`{"ip":%q}`, stranger))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body: %s)", rec.Code, rec.Body.String())
	}
	trusted, err := st.IsTrustedIP(userID, stranger)
	if err != nil {
		t.Fatalf("IsTrustedIP: %v", err)
	}
	if trusted {
		t.Error("an unfamiliar address was trusted below the threshold")
	}
}

// TestTrustedIPs_AutoToggleRoundTrip the auto_trust_ip switch persists per user
// and is reflected by the list endpoint.
func TestTrustedIPs_AutoToggleRoundTrip(t *testing.T) {
	srv, st := newTestServer(t, nil)
	userID := seedRegularUser(t, st, "alice", "init-pass-1")
	cookie := memberSession(t, srv, userID)
	srv.countryLookup = func(string) (string, error) { return "", nil }

	rec := trustedIPRequest(t, srv, cookie, http.MethodPut, "/api/me/trusted-ips/auto", `{"enabled":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("enable status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	if !srv.autoTrustEnabled(userID) {
		t.Error("autoTrustEnabled = false after enabling")
	}
	resp := decodeTrustedIPs(t, trustedIPRequest(t, srv, cookie, http.MethodGet, "/api/me/trusted-ips", ""))
	if !resp.AutoTrustIP {
		t.Error("auto_trust_ip = false in the list envelope after enabling")
	}

	if off := trustedIPRequest(t, srv, cookie, http.MethodPut, "/api/me/trusted-ips/auto", `{"enabled":false}`); off.Code != http.StatusOK {
		t.Fatalf("disable status = %d, want 200", off.Code)
	}
	if srv.autoTrustEnabled(userID) {
		t.Error("autoTrustEnabled = true after disabling")
	}
	// A body without the flag is a 400: silently defaulting would let a UI bug
	// turn the exemption on.
	if bad := trustedIPRequest(t, srv, cookie, http.MethodPut, "/api/me/trusted-ips/auto", `{}`); bad.Code != http.StatusBadRequest {
		t.Errorf("missing enabled status = %d, want 400", bad.Code)
	}
}

// TestTrustedIPs_AdminClear the super admin escape hatch wipes every grant for
// the target and books an audit row; ordinary users are refused.
func TestTrustedIPs_AdminClear(t *testing.T) {
	srv, st := newTestServer(t, nil)
	superID := seedSuperAdmin(t, st)
	targetID := seedRegularUser(t, st, "alice", "init-pass-1")
	for _, ip := range []string{"203.0.113.40", "203.0.113.41"} {
		if err := st.AddTrustedIP(targetID, ip); err != nil {
			t.Fatalf("AddTrustedIP: %v", err)
		}
	}

	path := fmt.Sprintf("/api/admin/users/%d/trusted-ips/clear", targetID)

	// Ordinary users cannot clear anyone's list.
	denied := trustedIPRequest(t, srv, memberSession(t, srv, targetID), http.MethodPost, path, "")
	if denied.Code != http.StatusForbidden {
		t.Fatalf("member status = %d, want 403 (body: %s)", denied.Code, denied.Body.String())
	}

	rec := trustedIPRequest(t, srv, adminSession(t, srv, superID), http.MethodPost, path, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("admin status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		Removed int `json:"removed"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Removed != 2 {
		t.Errorf("removed = %d, want 2", resp.Removed)
	}
	grants, err := st.ListTrustedIPs(targetID)
	if err != nil {
		t.Fatalf("ListTrustedIPs: %v", err)
	}
	if len(grants) != 0 {
		t.Errorf("target still has %d grants after clear", len(grants))
	}
	assertAuditEvent(t, st, "trusted_ip_cleared", "alice")

	// An unknown user id is a 404, not a silent success.
	if missing := trustedIPRequest(t, srv, adminSession(t, srv, superID), http.MethodPost,
		"/api/admin/users/999999/trusted-ips/clear", ""); missing.Code != http.StatusNotFound {
		t.Errorf("unknown user status = %d, want 404", missing.Code)
	}
}

// TestTrustedIPs_AutoTrustOnLogin the second login stage grants trust without
// being asked only when auto_trust_ip is on and the address already cleared the
// threshold; with the switch off nothing is written.
func TestTrustedIPs_AutoTrustOnLogin(t *testing.T) {
	for _, tc := range []struct {
		name      string
		autoOn    bool
		priorMFAs int
		want      bool
	}{
		{name: "off at threshold", autoOn: false, priorMFAs: trustRecommendationThreshold, want: false},
		{name: "on below threshold", autoOn: true, priorMFAs: trustRecommendationThreshold - 1, want: false},
		{name: "on at threshold", autoOn: true, priorMFAs: trustRecommendationThreshold, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, st := newTestServer(t, nil)
			h := srv.Handler()
			secret, _ := enrolledLoginFixture(t, srv, h)
			user, err := st.GetUserByUsername("rookie")
			if err != nil {
				t.Fatalf("GetUserByUsername: %v", err)
			}

			const ip = "9.9.9.70"
			seedMFALogins(t, st, "rookie", ip, tc.priorMFAs)
			if tc.autoOn {
				if err := st.SetUserSetting(user.ID, autoTrustIPSettingKey, "true"); err != nil {
					t.Fatalf("SetUserSetting: %v", err)
				}
			}

			token := loginStageOne(t, h, "rookie", "init-pass-1", ip)
			rec := postLoginMFA(t, h, fmt.Sprintf(`{"mfa_pending_token":%q,"code":%q}`,
				token, currentTOTP(t, secret)), ip)
			if rec.Code != http.StatusOK {
				t.Fatalf("login status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
			}

			trusted, err := st.IsTrustedIP(user.ID, ip)
			if err != nil {
				t.Fatalf("IsTrustedIP: %v", err)
			}
			if trusted != tc.want {
				t.Errorf("IsTrustedIP = %v, want %v", trusted, tc.want)
			}
		})
	}
}

// TestTrustedIPs_AutoTrustSkipsLoopback behind an untrusted reverse proxy every
// request looks like 127.0.0.1; auto-granting it would exempt everyone sharing
// that hop (CONTEXT.md "受信 IP"). Explicit trust of loopback still works.
func TestTrustedIPs_AutoTrustSkipsLoopback(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	secret, _ := enrolledLoginFixture(t, srv, h)
	user, err := st.GetUserByUsername("rookie")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	if err := st.SetUserSetting(user.ID, autoTrustIPSettingKey, "true"); err != nil {
		t.Fatalf("SetUserSetting: %v", err)
	}

	const ip = "127.0.0.1"
	seedMFALogins(t, st, "rookie", ip, trustRecommendationThreshold)

	token := loginStageOne(t, h, "rookie", "init-pass-1", ip)
	rec := postLoginMFA(t, h, fmt.Sprintf(`{"mfa_pending_token":%q,"code":%q}`,
		token, currentTOTP(t, secret)), ip)
	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}

	trusted, err := st.IsTrustedIP(user.ID, ip)
	if err != nil {
		t.Fatalf("IsTrustedIP: %v", err)
	}
	if trusted {
		t.Error("loopback was auto-trusted, want skipped")
	}

	// The explicit path still allows it: adopting the recommendation works.
	adopt := trustedIPRequest(t, srv, memberSession(t, srv, user.ID), http.MethodPost,
		"/api/me/trusted-ips", fmt.Sprintf(`{"ip":%q}`, ip))
	if adopt.Code != http.StatusOK {
		t.Fatalf("explicit adopt status = %d, want 200 (body: %s)", adopt.Code, adopt.Body.String())
	}
	if trusted, err = st.IsTrustedIP(user.ID, ip); err != nil || !trusted {
		t.Errorf("IsTrustedIP after explicit adopt = %v (err=%v), want true", trusted, err)
	}
}
