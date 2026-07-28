package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/taliove/proxyhub/internal/store"
)

// End-to-end login coverage (login hardening ticket 12).
//
// The other login tests in this package each pin one mechanism: captcha_test.go
// the captcha gate, mfa_test.go the two stages, handlers_trusted_ips_test.go the
// trust surface. What none of them exercise is the whole path a real account
// walks - forced enrollment, the second stage, a trust grant, and the way the
// three layers (IP2Ban, captcha, MFA) interact when one of them fires mid-flow.
// These tests drive only HTTP, so a regression in the ordering of the gates
// shows up here even when every unit test still passes.

// e2eLoginResponse is the union of the password-stage response shapes: a
// completed login ({ok, user}), a challenge ({mfa_required, mfa_pending_token})
// or a captcha demand ({captcha_required}).
type e2eLoginResponse struct {
	OK              bool   `json:"ok"`
	MFARequired     bool   `json:"mfa_required"`
	PendingToken    string `json:"mfa_pending_token"`
	CaptchaRequired bool   `json:"captcha_required"`
	User            struct {
		Username      string `json:"username"`
		MustEnrollMFA bool   `json:"must_enroll_mfa"`
	} `json:"user"`
}

// decodeLoginResponse parses a login/second-stage body.
func decodeLoginResponse(t *testing.T, body []byte) e2eLoginResponse {
	t.Helper()
	var resp e2eLoginResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode login response %q: %v", body, err)
	}
	return resp
}

// TestLoginE2E_BannedIPRefusedBeforeAnythingElse the ban check is the outermost
// layer: a banned address gets 403 without a captcha hint, without a password
// check and without an audit row, so a banned attacker learns nothing about
// which of the three gates it is behind.
func TestLoginE2E_BannedIPRefusedBeforeAnythingElse(t *testing.T) {
	srv, st := newTestServer(t, nil)
	srv.captcha = newStubCaptcha()
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")

	const ip = "203.0.113.10"
	if _, err := st.BanIP(ip, time.Hour, time.Now()); err != nil {
		t.Fatalf("BanIP: %v", err)
	}

	// Correct credentials, correct captcha: still refused.
	w := doLoginCaptcha(t, h, loginBody{
		Username: "owner", Password: "a-very-strong-pass",
		CaptchaID: "stub-challenge", CaptchaAnswer: stubGoodAnswer,
	}, ip)
	if w.Code != http.StatusForbidden {
		t.Fatalf("banned login status = %d, want 403 (body: %s)", w.Code, w.Body.String())
	}
	if sessionCookieOf(w) != nil {
		t.Error("banned login handed out a session cookie")
	}
	if got := auditEventCount(t, st, "login_success"); got != 0 {
		t.Errorf("login_success rows = %d, want 0 for a banned address", got)
	}
}

// TestLoginE2E_HoneypotBansInstantly a honeypot username is an immediate ban
// plus a honeypot_ban audit row, and the ban then shuts the door on the same
// address even with real credentials.
func TestLoginE2E_HoneypotBansInstantly(t *testing.T) {
	srv, st := newTestServer(t, nil)
	srv.captcha = newStubCaptcha()
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")

	const ip = "203.0.113.11"
	w := doLogin(t, h, "admin", "whatever", ip)
	if w.Code != http.StatusForbidden {
		t.Fatalf("honeypot status = %d, want 403 (body: %s)", w.Code, w.Body.String())
	}
	if got := auditEventCount(t, st, "honeypot_ban"); got != 1 {
		t.Fatalf("honeypot_ban rows = %d, want 1", got)
	}
	banned, err := st.IsBanned(ip, time.Now())
	if err != nil {
		t.Fatalf("IsBanned: %v", err)
	}
	if !banned {
		t.Fatal("honeypot hit did not ban the source address")
	}

	// The ban is now the operative gate for that address.
	again := doLogin(t, h, "owner", "a-very-strong-pass", ip)
	if again.Code != http.StatusForbidden {
		t.Errorf("post-honeypot login status = %d, want 403", again.Code)
	}
}

// TestLoginE2E_CaptchaTriggersThenPasses the progressive part: the first
// failure is a plain 401 that flags captcha_required, the next attempt without
// an answer is refused and audited as captcha_failure, and an answer plus the
// right password gets through.
func TestLoginE2E_CaptchaTriggersThenPasses(t *testing.T) {
	srv, st := newTestServer(t, nil)
	srv.captcha = newStubCaptcha()
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")
	// Default threshold is 1: one failure arms the captcha. Pin it explicitly so
	// the test states the contract it relies on.
	if err := st.SetSetting("captcha_trigger_threshold", "1"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	const ip = "203.0.113.12"

	// Attempt 1: no captcha demanded yet (fail_count 0 < 1), wrong password.
	first := doLogin(t, h, "owner", "wrong-password", ip)
	if first.Code != http.StatusUnauthorized {
		t.Fatalf("first attempt status = %d, want 401", first.Code)
	}
	if !decodeLoginResponse(t, first.Body.Bytes()).CaptchaRequired {
		t.Error("first failure did not flag captcha_required (client would never render the image)")
	}

	// Attempt 2: the gate is armed, so a request without an answer never reaches
	// the password check - it is booked as a captcha failure.
	second := doLogin(t, h, "owner", "a-very-strong-pass", ip)
	if second.Code != http.StatusUnauthorized {
		t.Fatalf("second attempt status = %d, want 401", second.Code)
	}
	if !decodeLoginResponse(t, second.Body.Bytes()).CaptchaRequired {
		t.Error("armed captcha gate did not flag captcha_required")
	}
	if sessionCookieOf(second) != nil {
		t.Error("correct password without a captcha handed out a session")
	}
	if got := auditEventCount(t, st, "captcha_failure"); got != 1 {
		t.Errorf("captcha_failure rows = %d, want 1", got)
	}

	// Attempt 3: captcha solved and password right -> session (owner has no MFA
	// bound yet, so the login completes on branch one).
	third := doLoginCaptcha(t, h, loginBody{
		Username: "owner", Password: "a-very-strong-pass",
		CaptchaID: "stub-challenge", CaptchaAnswer: stubGoodAnswer,
	}, ip)
	if third.Code != http.StatusOK {
		t.Fatalf("third attempt status = %d, want 200 (body: %s)", third.Code, third.Body.String())
	}
	if sessionCookieOf(third) == nil {
		t.Fatal("no session cookie after a solved captcha and correct password")
	}
	// A completed login clears the counter, which disarms the captcha again.
	if n := failCountFor(t, st, ip); n != 0 {
		t.Errorf("fail_count = %d after success, want 0 (captcha must disarm)", n)
	}
}

// TestLoginE2E_UnenrolledSessionIsGatedUntilEnrollment forced enrollment: the
// password stage hands out a session with must_enroll_mfa, but that session is
// 403 + must_enroll_mfa on business routes until the two enrollment stages
// complete. This is the whole reason MFA can be mandatory without locking a
// fresh account out of the box.
func TestLoginE2E_UnenrolledSessionIsGatedUntilEnrollment(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")
	cookie, _ := unenrolledSession(t, srv, h, "rookie", "init-pass-1", store.RoleUser)

	// Business route is closed while unenrolled.
	rec := mfaRequest(t, h, cookie, "GET", "/api/endpoints", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("GET /api/endpoints status = %d, want 403 while unenrolled (body: %s)",
			rec.Code, rec.Body.String())
	}
	var gate struct {
		MustEnroll bool `json:"must_enroll_mfa"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &gate); err != nil {
		t.Fatalf("decode 403: %v", err)
	}
	if !gate.MustEnroll {
		t.Errorf("403 body = %s, want must_enroll_mfa true", rec.Body.String())
	}

	// Enroll over HTTP (both stages), then the same session is admitted.
	enrollMFA(t, h, cookie)
	if rec := mfaRequest(t, h, cookie, "GET", "/api/endpoints", ""); rec.Code != http.StatusOK {
		t.Errorf("GET /api/endpoints status = %d after enrollment, want 200 (body: %s)",
			rec.Code, rec.Body.String())
	}
}

// TestLoginE2E_PendingThenTOTPThenTrusted the core journey of an enrolled
// account across three logins from one address:
//
//	login 1: password -> mfa_pending (no session), TOTP -> session, no trust
//	login 2: password -> mfa_pending again (nothing was trusted)
//	         TOTP with trust_ip -> session + a 30 day grant
//	login 3: password alone -> session, audited as mfa_skipped=trusted_ip
func TestLoginE2E_PendingThenTOTPThenTrusted(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	secret, _ := enrolledLoginFixture(t, srv, h)
	const ip = "203.0.113.20"

	// Login 1: the password stage must not mint a session.
	w := doLogin(t, h, "rookie", "init-pass-1", ip)
	if w.Code != http.StatusOK {
		t.Fatalf("password stage status = %d (body: %s)", w.Code, w.Body.String())
	}
	stage := decodeLoginResponse(t, w.Body.Bytes())
	if stage.OK || !stage.MFARequired || stage.PendingToken == "" {
		t.Fatalf("password stage did not challenge: %s", w.Body.String())
	}
	if sessionCookieOf(w) != nil {
		t.Fatal("password stage handed out a session before the second factor")
	}

	rec := postLoginMFA(t, h, fmt.Sprintf(`{"mfa_pending_token":%q,"code":%q}`,
		stage.PendingToken, currentTOTP(t, secret)), ip)
	if rec.Code != http.StatusOK {
		t.Fatalf("second stage status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	if sessionCookieOf(rec) == nil {
		t.Fatal("no session cookie after a valid TOTP")
	}
	if detail := latestAuditDetail(t, st, "login_success"); detail != "mfa=totp" {
		t.Errorf("login_success detail = %q, want exactly mfa=totp", detail)
	}

	// Login 2: still challenged - completing MFA does not trust an address by
	// itself, only the explicit checkbox does.
	token2 := loginStageOne(t, h, "rookie", "init-pass-1", ip)
	rec = postLoginMFA(t, h, fmt.Sprintf(`{"mfa_pending_token":%q,"code":%q,"trust_ip":true}`,
		token2, currentTOTP(t, secret)), ip)
	if rec.Code != http.StatusOK {
		t.Fatalf("trusting second stage status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	if detail := latestAuditDetail(t, st, "trusted_ip_added"); detail == "" {
		t.Error("trust_ip=true did not record a trusted_ip_added audit row")
	}

	// Login 3: password alone completes, and the audit marker says why.
	third := doLogin(t, h, "rookie", "init-pass-1", ip)
	if third.Code != http.StatusOK {
		t.Fatalf("trusted login status = %d, want 200 (body: %s)", third.Code, third.Body.String())
	}
	trusted := decodeLoginResponse(t, third.Body.Bytes())
	if !trusted.OK || trusted.MFARequired {
		t.Fatalf("trusted login still challenged: %s", third.Body.String())
	}
	if sessionCookieOf(third) == nil {
		t.Fatal("trusted login handed out no session cookie")
	}
	if detail := latestAuditDetail(t, st, "login_success"); detail != "mfa_skipped=trusted_ip" {
		t.Errorf("login_success detail = %q, want mfa_skipped=trusted_ip", detail)
	}

	// The grant is scoped to this address: another address is challenged again.
	other := doLogin(t, h, "rookie", "init-pass-1", "203.0.113.21")
	if !decodeLoginResponse(t, other.Body.Bytes()).MFARequired {
		t.Errorf("login from an untrusted address was not challenged: %s", other.Body.String())
	}

	// And the grant is revocable: after revoking, the trusted address is back to
	// the challenge.
	if err := st.RevokeTrustedIP(userIDOf(t, st, "rookie"), ip); err != nil {
		t.Fatalf("RevokeTrustedIP: %v", err)
	}
	afterRevoke := doLogin(t, h, "rookie", "init-pass-1", ip)
	if !decodeLoginResponse(t, afterRevoke.Body.Bytes()).MFARequired {
		t.Errorf("revoked address was not challenged again: %s", afterRevoke.Body.String())
	}
}

// TestLoginE2E_RecoveryCodeIsSingleUse the paper fallback: a recovery code
// completes the second stage exactly once, is marked mfa=recovery, and the same
// code on a fresh handoff is refused.
func TestLoginE2E_RecoveryCodeIsSingleUse(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	_, codes := enrolledLoginFixture(t, srv, h)
	const ip = "203.0.113.30"

	token := loginStageOne(t, h, "rookie", "init-pass-1", ip)
	rec := postLoginMFA(t, h, fmt.Sprintf(`{"mfa_pending_token":%q,"code":%q}`, token, codes[0]), ip)
	if rec.Code != http.StatusOK {
		t.Fatalf("recovery login status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	if sessionCookieOf(rec) == nil {
		t.Fatal("no session cookie after a recovery-code login")
	}
	if detail := latestAuditDetail(t, st, "login_success"); detail != "mfa=recovery" {
		t.Errorf("login_success detail = %q, want mfa=recovery", detail)
	}

	// One code burned, nine left.
	cfg, err := st.GetUserMFAConfig(userIDOf(t, st, "rookie"))
	if err != nil {
		t.Fatalf("GetUserMFAConfig: %v", err)
	}
	if len(cfg.RecoveryCodesHash) != len(codes)-1 {
		t.Errorf("stored codes = %d, want %d after one use", len(cfg.RecoveryCodesHash), len(codes)-1)
	}

	// Replay on a fresh handoff: refused, and audited as an MFA failure.
	token2 := loginStageOne(t, h, "rookie", "init-pass-1", ip)
	replay := postLoginMFA(t, h, fmt.Sprintf(`{"mfa_pending_token":%q,"code":%q}`, token2, codes[0]), ip)
	if replay.Code != http.StatusUnauthorized {
		t.Errorf("replayed recovery code status = %d, want 401 (body: %s)", replay.Code, replay.Body.String())
	}
	if got := auditEventCount(t, st, "mfa_failure"); got != 1 {
		t.Errorf("mfa_failure rows = %d, want 1", got)
	}
}

// TestLoginE2E_MFAFailuresWalkIntoIP2Ban the second factor is not a free
// retry surface: every wrong code charges the same per-IP counter as a wrong
// password, so grinding codes bans the address (threshold 3 from doSetup) and
// the ban then refuses even the password stage.
func TestLoginE2E_MFAFailuresWalkIntoIP2Ban(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	secret, _ := enrolledLoginFixture(t, srv, h)
	const ip = "203.0.113.40"

	// doSetup writes ban_threshold=3.
	policy := srv.loadSecurityPolicy()
	if policy.BanThreshold != 3 {
		t.Fatalf("ban_threshold = %d, want the 3 written by doSetup", policy.BanThreshold)
	}

	token := loginStageOne(t, h, "rookie", "init-pass-1", ip)
	for i := 0; i < policy.BanThreshold; i++ {
		rec := postLoginMFA(t, h, fmt.Sprintf(`{"mfa_pending_token":%q,"code":"000000"}`, token), ip)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("wrong code #%d status = %d, want 401 (body: %s)", i+1, rec.Code, rec.Body.String())
		}
	}

	banned, err := st.IsBanned(ip, time.Now())
	if err != nil {
		t.Fatalf("IsBanned: %v", err)
	}
	if !banned {
		t.Fatalf("address not banned after %d wrong second factors", policy.BanThreshold)
	}
	if got := auditEventCount(t, st, "threshold_ban"); got == 0 {
		t.Error("no threshold_ban audit row after the MFA failure budget tripped the ban")
	}

	// The ban is the outer gate on the password stage: refused even with the
	// right password, so no new handoff can be obtained from this address.
	w := doLogin(t, h, "rookie", "init-pass-1", ip)
	if w.Code != http.StatusForbidden {
		t.Errorf("password stage after ban status = %d, want 403 (body: %s)", w.Code, w.Body.String())
	}

	// Documented boundary: POST /api/login/mfa has no ban check of its own. A
	// handoff issued before the ban therefore still redeems - it is already
	// bound to one user, one address, a 5 minute TTL and mfaPendingMaxFailures
	// wrong codes, and its holder proved the password. The ban stops new
	// handoffs, not the one already in flight.
	rec := postLoginMFA(t, h, fmt.Sprintf(`{"mfa_pending_token":%q,"code":%q}`,
		token, currentTOTP(t, secret)), ip)
	if rec.Code != http.StatusOK {
		t.Fatalf("in-flight handoff after ban status = %d, want 200 (body: %s)",
			rec.Code, rec.Body.String())
	}

	// What does close that door is the pending budget. Grind a fresh handoff
	// past mfaPendingMaxFailures and the handoff itself dies, so a later valid
	// TOTP on it is a plain 401.
	if err := st.ResetLoginFailures(ip); err != nil {
		t.Fatalf("ResetLoginFailures: %v", err)
	}
	if err := st.UnbanIP(ip); err != nil {
		t.Fatalf("UnbanIP: %v", err)
	}
	fresh := loginStageOne(t, h, "rookie", "init-pass-1", ip)
	for i := 0; i < mfaPendingMaxFailures; i++ {
		if rec := postLoginMFA(t, h,
			fmt.Sprintf(`{"mfa_pending_token":%q,"code":"000000"}`, fresh), ip); rec.Code != http.StatusUnauthorized {
			t.Fatalf("grind #%d status = %d, want 401", i+1, rec.Code)
		}
	}
	dead := postLoginMFA(t, h, fmt.Sprintf(`{"mfa_pending_token":%q,"code":%q}`,
		fresh, currentTOTP(t, secret)), ip)
	if dead.Code != http.StatusUnauthorized {
		t.Errorf("valid TOTP on an exhausted handoff status = %d, want 401 (body: %s)",
			dead.Code, dead.Body.String())
	}
}

// TestLoginE2E_CaptchaGateGuardsTheSecondStageEntry composition guard across
// all three layers: once an address is behind the captcha wall, the password
// stage of an MFA account never reaches the MFA branch, so no pending token is
// issued to an unsolved captcha.
func TestLoginE2E_CaptchaGateGuardsTheSecondStageEntry(t *testing.T) {
	srv, st := newTestServer(t, nil)
	srv.captcha = newStubCaptcha()
	h := srv.Handler()
	secret, _ := enrolledLoginFixture(t, srv, h)
	if err := st.SetSetting("captcha_trigger_threshold", "0"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	const ip = "203.0.113.50"

	// Threshold 0 = captcha always on. Right password, no captcha -> no handoff.
	w := doLogin(t, h, "rookie", "init-pass-1", ip)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body: %s)", w.Code, w.Body.String())
	}
	resp := decodeLoginResponse(t, w.Body.Bytes())
	if !resp.CaptchaRequired {
		t.Error("captcha_required missing on the always-on gate")
	}
	if resp.PendingToken != "" || resp.MFARequired {
		t.Errorf("captcha gate leaked an MFA handoff: %s", w.Body.String())
	}

	// Same request with a solved captcha reaches the MFA branch.
	solved := doLoginCaptcha(t, h, loginBody{
		Username: "rookie", Password: "init-pass-1",
		CaptchaID: "stub-challenge", CaptchaAnswer: stubGoodAnswer,
	}, ip)
	if solved.Code != http.StatusOK {
		t.Fatalf("solved-captcha status = %d, want 200 (body: %s)", solved.Code, solved.Body.String())
	}
	stage := decodeLoginResponse(t, solved.Body.Bytes())
	if !stage.MFARequired || stage.PendingToken == "" {
		t.Fatalf("solved captcha did not reach the MFA branch: %s", solved.Body.String())
	}
	// The second stage carries no captcha of its own: the pending token is the
	// credential, and it is already IP-bound and budgeted.
	rec := postLoginMFA(t, h, fmt.Sprintf(`{"mfa_pending_token":%q,"code":%q}`,
		stage.PendingToken, currentTOTP(t, secret)), ip)
	if rec.Code != http.StatusOK {
		t.Fatalf("second stage status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	if sessionCookieOf(rec) == nil {
		t.Error("no session cookie at the end of the full three-layer path")
	}
}

// TestLoginE2E_LoopbackSkipsCaptchaButNotMFA the loopback carve-out is scoped:
// 127.0.0.1 is exempt from ban, honeypot and captcha (the SSH-tunnel escape
// hatch stays usable), but it is NOT exempt from the second factor.
func TestLoginE2E_LoopbackSkipsCaptchaButNotMFA(t *testing.T) {
	srv, st := newTestServer(t, nil)
	srv.captcha = newStubCaptcha()
	h := srv.Handler()
	secret, _ := enrolledLoginFixture(t, srv, h)
	if err := st.SetSetting("captcha_trigger_threshold", "0"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	// No captcha fields, threshold 0: a non-loopback address would be refused
	// here. Loopback goes straight to the password check and then to the MFA
	// branch.
	w := doLogin(t, h, "rookie", "init-pass-1", "127.0.0.1")
	if w.Code != http.StatusOK {
		t.Fatalf("loopback status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	stage := decodeLoginResponse(t, w.Body.Bytes())
	if stage.CaptchaRequired {
		t.Error("loopback was asked for a captcha")
	}
	if !stage.MFARequired || stage.PendingToken == "" {
		t.Fatalf("loopback skipped the second factor: %s", w.Body.String())
	}
	if sessionCookieOf(w) != nil {
		t.Fatal("loopback password stage handed out a session")
	}

	rec := postLoginMFA(t, h, fmt.Sprintf(`{"mfa_pending_token":%q,"code":%q}`,
		stage.PendingToken, currentTOTP(t, secret)), "127.0.0.1")
	if rec.Code != http.StatusOK {
		t.Fatalf("loopback second stage status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	if sessionCookieOf(rec) == nil {
		t.Error("no session cookie after the loopback second stage")
	}
}

// TestLoginE2E_AdminResetForcesEnrollmentAgain the operator escape hatch seen
// from the login path: after a super admin reset, the account's next password
// login completes without a challenge (nothing left to verify) but carries
// must_enroll_mfa, and its session is gated until it enrolls again. This is the
// HTTP-side equivalent of `proxyhubctl reset-mfa`, which calls the same
// store.ResetUserMFA.
func TestLoginE2E_AdminResetForcesEnrollmentAgain(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	enrolledLoginFixture(t, srv, h)
	userID := userIDOf(t, st, "rookie")

	if err := st.ResetUserMFA(userID); err != nil {
		t.Fatalf("ResetUserMFA: %v", err)
	}

	const ip = "203.0.113.60"
	w := doLogin(t, h, "rookie", "init-pass-1", ip)
	if w.Code != http.StatusOK {
		t.Fatalf("post-reset login status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	resp := decodeLoginResponse(t, w.Body.Bytes())
	if resp.MFARequired {
		t.Fatalf("post-reset login still challenged: %s", w.Body.String())
	}
	if !resp.User.MustEnrollMFA {
		t.Error("post-reset login payload missing must_enroll_mfa")
	}
	cookie := sessionCookieOf(w)
	if cookie == nil {
		t.Fatal("post-reset login handed out no session cookie")
	}
	if rec := mfaRequest(t, h, cookie, "GET", "/api/endpoints", ""); rec.Code != http.StatusForbidden {
		t.Errorf("business route status = %d after reset, want 403 (body: %s)", rec.Code, rec.Body.String())
	}
}

// TestLoginE2E_ResetClearsTrustSoNextLoginIsChallenged a reset alone leaves the
// trust grants in place (they only matter for enrolled accounts), so the
// operator playbook pairs reset-mfa with clearing trusted IPs. This pins the
// combined outcome: after both, the previously trusted address is challenged.
func TestLoginE2E_ResetClearsTrustSoNextLoginIsChallenged(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	secret, _ := enrolledLoginFixture(t, srv, h)
	userID := userIDOf(t, st, "rookie")
	const ip = "203.0.113.70"

	token := loginStageOne(t, h, "rookie", "init-pass-1", ip)
	if rec := postLoginMFA(t, h, fmt.Sprintf(`{"mfa_pending_token":%q,"code":%q,"trust_ip":true}`,
		token, currentTOTP(t, secret)), ip); rec.Code != http.StatusOK {
		t.Fatalf("trusting login status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	// Trusted now: password alone is enough.
	if decodeLoginResponse(t, doLogin(t, h, "rookie", "init-pass-1", ip).Body.Bytes()).MFARequired {
		t.Fatal("address was not trusted after trust_ip=true")
	}

	removed, err := st.RevokeAllTrustedIPs(userID)
	if err != nil {
		t.Fatalf("RevokeAllTrustedIPs: %v", err)
	}
	if removed == 0 {
		t.Fatal("RevokeAllTrustedIPs removed nothing")
	}
	if !decodeLoginResponse(t, doLogin(t, h, "rookie", "init-pass-1", ip).Body.Bytes()).MFARequired {
		t.Error("cleared address was not challenged again")
	}
}

// userIDOf resolves a username to its id.
func userIDOf(t *testing.T, st *store.Store, username string) int64 {
	t.Helper()
	user, err := st.GetUserByUsername(username)
	if err != nil {
		t.Fatalf("GetUserByUsername(%s): %v", username, err)
	}
	return user.ID
}
