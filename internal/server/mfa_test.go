package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"

	"github.com/taliove/proxyhub/internal/mfa"
	"github.com/taliove/proxyhub/internal/store"
)

// currentTOTP returns a code valid for secret at the current time.
func currentTOTP(t *testing.T, secret string) string {
	t.Helper()
	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}
	return code
}

// mfaRequest issues an authenticated JSON request carrying cookie.
func mfaRequest(t *testing.T, h http.Handler, cookie *http.Cookie, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	req.RemoteAddr = "9.9.9.30:3000"
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// unenrolledSession creates a user with a known password and logs in, without
// enrolling MFA (unlike the shared helpers, which enroll so business routes
// stay reachable). Returns the session cookie and the user id.
func unenrolledSession(t *testing.T, srv *Server, h http.Handler, username, password, role string) (*http.Cookie, int64) {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("GenerateFromPassword: %v", err)
	}
	user, err := srv.st.CreateUser(username, string(hash), role, false)
	if err != nil {
		t.Fatalf("CreateUser(%s): %v", username, err)
	}
	w := doLogin(t, h, username, password, "9.9.9.30")
	if w.Code != http.StatusOK {
		t.Fatalf("login %s status = %d (body: %s)", username, w.Code, w.Body.String())
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == "session" {
			return c, user.ID
		}
	}
	t.Fatalf("no session cookie for %s", username)
	return nil, 0
}

// TestMFAEnroll_TwoStage covers the enrollment contract: stage one returns a
// secret plus otpauth URL but must NOT activate TOTP, stage two activates it
// only when the submitted code verifies and returns the recovery codes once.
func TestMFAEnroll_TwoStage(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")
	cookie, userID := unenrolledSession(t, srv, h, "rookie", "init-pass-1", store.RoleUser)

	// Stage one: no totp_code -> secret staged, MFA still disabled.
	rec := mfaRequest(t, h, cookie, "POST", "/api/me/mfa/enroll", `{}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("stage-one status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	var stage1 struct {
		Secret     string `json:"secret"`
		OTPAuthURL string `json:"otpauth_url"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &stage1); err != nil {
		t.Fatalf("decode stage-one: %v", err)
	}
	if stage1.Secret == "" {
		t.Error("stage-one secret is empty")
	}
	if !strings.HasPrefix(stage1.OTPAuthURL, "otpauth://totp/") {
		t.Errorf("otpauth_url = %q, want otpauth://totp/ prefix", stage1.OTPAuthURL)
	}

	cfg, err := st.GetUserMFAConfig(userID)
	if err != nil {
		t.Fatalf("GetUserMFAConfig: %v", err)
	}
	if cfg.Enabled {
		t.Error("totp_enabled = true after stage one, want false until confirmed")
	}
	if cfg.TOTPSecret != stage1.Secret {
		t.Error("staged secret not persisted")
	}
	if len(cfg.RecoveryCodesHash) != 0 {
		t.Errorf("recovery codes issued at stage one (%d), want none until confirmed",
			len(cfg.RecoveryCodesHash))
	}

	// Stage two with a wrong code: rejected, still not enabled.
	rec = mfaRequest(t, h, cookie, "POST", "/api/me/mfa/enroll", `{"totp_code":"000000"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("stage-two wrong-code status = %d, want 400", rec.Code)
	}
	cfg, _ = st.GetUserMFAConfig(userID)
	if cfg.Enabled {
		t.Fatal("totp_enabled = true after a failed confirmation")
	}

	// Stage two with a valid code: enabled + recovery codes returned once.
	body := fmt.Sprintf(`{"totp_code":%q}`, currentTOTP(t, stage1.Secret))
	rec = mfaRequest(t, h, cookie, "POST", "/api/me/mfa/enroll", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("stage-two status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	var stage2 struct {
		RecoveryCodes []string `json:"recovery_codes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &stage2); err != nil {
		t.Fatalf("decode stage-two: %v", err)
	}
	if len(stage2.RecoveryCodes) != mfa.RecoveryCodeCount {
		t.Fatalf("recovery_codes = %d, want %d", len(stage2.RecoveryCodes), mfa.RecoveryCodeCount)
	}

	cfg, _ = st.GetUserMFAConfig(userID)
	if !cfg.Enabled {
		t.Error("totp_enabled = false after successful confirmation")
	}
	if len(cfg.RecoveryCodesHash) != mfa.RecoveryCodeCount {
		t.Errorf("stored recovery hashes = %d, want %d",
			len(cfg.RecoveryCodesHash), mfa.RecoveryCodeCount)
	}
	// Plaintext codes must never be readable from the database.
	for _, code := range stage2.RecoveryCodes {
		for _, stored := range cfg.RecoveryCodesHash {
			if stored == code {
				t.Fatal("recovery code stored in plaintext")
			}
		}
	}

	// Already enrolled: re-enrolling is refused so a live authenticator
	// cannot be silently replaced by anyone holding the session.
	rec = mfaRequest(t, h, cookie, "POST", "/api/me/mfa/enroll", `{}`)
	if rec.Code != http.StatusConflict {
		t.Errorf("re-enroll status = %d, want 409", rec.Code)
	}

	assertAuditEvent(t, st, "mfa_enrolled", "rookie")
}

// assertAuditEvent fails unless an audit row of eventType exists for username.
func assertAuditEvent(t *testing.T, st *store.Store, eventType, username string) {
	t.Helper()
	events, _, err := st.ListAuditEvents(store.AuditFilter{EventTypes: []string{eventType}}, 50, 0)
	if err != nil {
		t.Fatalf("ListAuditEvents(%s): %v", eventType, err)
	}
	for _, e := range events {
		if e.Username == username {
			return
		}
	}
	t.Errorf("no %s audit event for %q (found %d of that type)", eventType, username, len(events))
}

// enrollMFA runs both enrollment stages over HTTP and returns the secret and
// the plaintext recovery codes.
func enrollMFA(t *testing.T, h http.Handler, cookie *http.Cookie) (secret string, codes []string) {
	t.Helper()
	rec := mfaRequest(t, h, cookie, "POST", "/api/me/mfa/enroll", `{}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("enroll stage one status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	var stage1 struct {
		Secret string `json:"secret"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &stage1); err != nil {
		t.Fatalf("decode enroll stage one: %v", err)
	}

	body := fmt.Sprintf(`{"totp_code":%q}`, currentTOTP(t, stage1.Secret))
	rec = mfaRequest(t, h, cookie, "POST", "/api/me/mfa/enroll", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("enroll stage two status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	var stage2 struct {
		RecoveryCodes []string `json:"recovery_codes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &stage2); err != nil {
		t.Fatalf("decode enroll stage two: %v", err)
	}
	return stage1.Secret, stage2.RecoveryCodes
}

// TestMFARegenerateRecovery_RequiresSecondFactor asserts regeneration is gated
// on a second-factor confirmation and that the previous batch dies with it.
func TestMFARegenerateRecovery_RequiresSecondFactor(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")
	cookie, userID := unenrolledSession(t, srv, h, "rookie", "init-pass-1", store.RoleUser)
	secret, oldCodes := enrollMFA(t, h, cookie)

	// No confirmation code at all -> refused.
	if rec := mfaRequest(t, h, cookie, "POST", "/api/me/mfa/regenerate-recovery", `{}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("regenerate without code status = %d, want 400", rec.Code)
	}
	// Wrong confirmation code -> refused.
	if rec := mfaRequest(t, h, cookie, "POST", "/api/me/mfa/regenerate-recovery", `{"code":"000000"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("regenerate with wrong code status = %d, want 400", rec.Code)
	}
	// The old batch survives a refused attempt.
	cfg, _ := st.GetUserMFAConfig(userID)
	if len(cfg.RecoveryCodesHash) != mfa.RecoveryCodeCount {
		t.Fatalf("stored codes = %d after refused regenerate, want %d untouched",
			len(cfg.RecoveryCodesHash), mfa.RecoveryCodeCount)
	}

	// A valid TOTP confirms: new batch issued, old batch fully invalidated.
	body := fmt.Sprintf(`{"code":%q}`, currentTOTP(t, secret))
	rec := mfaRequest(t, h, cookie, "POST", "/api/me/mfa/regenerate-recovery", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("regenerate status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		RecoveryCodes []string `json:"recovery_codes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode regenerate: %v", err)
	}
	if len(resp.RecoveryCodes) != mfa.RecoveryCodeCount {
		t.Fatalf("new codes = %d, want %d", len(resp.RecoveryCodes), mfa.RecoveryCodeCount)
	}

	cfg, _ = st.GetUserMFAConfig(userID)
	stored := strings.Join(cfg.RecoveryCodesHash, ",")
	for _, old := range oldCodes {
		if _, ok, _ := mfa.VerifyRecoveryCode("["+quoteHashes(cfg.RecoveryCodesHash)+"]", old); ok {
			t.Fatalf("old recovery code %q still valid after regeneration", old)
		}
	}
	for _, fresh := range resp.RecoveryCodes {
		if strings.Contains(stored, fresh) {
			t.Fatal("new recovery code stored in plaintext")
		}
	}
}

// quoteHashes renders hashes as the inner body of a JSON array so the mfa
// verifier can be pointed at the stored set.
func quoteHashes(hashes []string) string {
	quoted := make([]string, len(hashes))
	for i, h := range hashes {
		quoted[i] = fmt.Sprintf("%q", h)
	}
	return strings.Join(quoted, ",")
}

// TestMFARegenerateRecovery_AcceptsRecoveryCode verifies a recovery code is an
// acceptable second factor, so a user who lost the authenticator is not locked
// out of regenerating.
func TestMFARegenerateRecovery_AcceptsRecoveryCode(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")
	cookie, _ := unenrolledSession(t, srv, h, "rookie", "init-pass-1", store.RoleUser)
	_, oldCodes := enrollMFA(t, h, cookie)

	body := fmt.Sprintf(`{"code":%q}`, oldCodes[0])
	rec := mfaRequest(t, h, cookie, "POST", "/api/me/mfa/regenerate-recovery", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("regenerate with recovery code status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
}

// TestMFARegenerateRecovery_RejectsUnenrolled guards against handing out
// recovery codes to an account that never bound an authenticator (that path is
// enrollment, and it must stay the only way to get a first batch).
func TestMFARegenerateRecovery_RejectsUnenrolled(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")
	cookie, _ := unenrolledSession(t, srv, h, "rookie", "init-pass-1", store.RoleUser)

	// The route sits behind the full guard chain, so an unenrolled caller is
	// turned away by requireMFAEnrolled before the handler runs: 403 pointing
	// at enrollment, which is where a first batch of codes actually comes from.
	rec := mfaRequest(t, h, cookie, "POST", "/api/me/mfa/regenerate-recovery", `{"code":"123456"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("regenerate while unenrolled status = %d, want 403", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode 403 body: %v", err)
	}
	if enroll, _ := body["must_enroll_mfa"].(bool); !enroll {
		t.Errorf("403 body = %v, want must_enroll_mfa: true", body)
	}
}

// TestAdminResetMFA covers the super-admin escape hatch: the target returns to
// the never-enrolled state and the action is audited. Ordinary users must not
// reach the endpoint at all.
func TestAdminResetMFA(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	admin := authCookie(t, h)

	cookie, userID := unenrolledSession(t, srv, h, "rookie", "init-pass-1", store.RoleUser)
	enrollMFA(t, h, cookie)
	if cfg, _ := st.GetUserMFAConfig(userID); !cfg.Enabled {
		t.Fatal("precondition failed: target not enrolled")
	}

	// Ordinary user cannot reset anyone (not even themselves) here.
	path := fmt.Sprintf("/api/admin/users/%d/reset-mfa", userID)
	if rec := mfaRequest(t, h, cookie, "POST", path, ""); rec.Code != http.StatusForbidden {
		t.Errorf("non-admin reset status = %d, want 403", rec.Code)
	}

	rec := mfaRequest(t, h, admin, "POST", path, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("admin reset status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}

	cfg, err := st.GetUserMFAConfig(userID)
	if err != nil {
		t.Fatalf("GetUserMFAConfig: %v", err)
	}
	if cfg.Enabled || cfg.TOTPSecret != "" || len(cfg.RecoveryCodesHash) != 0 {
		t.Errorf("target not back to unenrolled: %+v", cfg)
	}
	assertAuditEvent(t, st, "mfa_reset", "rookie")

	// Unknown user id is a 404, not a silent success.
	if rec := mfaRequest(t, h, admin, "POST", "/api/admin/users/999999/reset-mfa", ""); rec.Code != http.StatusNotFound {
		t.Errorf("reset unknown user status = %d, want 404", rec.Code)
	}
}

// TestMe_ReportsMustEnrollMFA asserts the session payload surfaces the
// enrollment obligation on both the login response and /api/me, which is what
// the frontend routes on.
func TestMe_ReportsMustEnrollMFA(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")

	hash, err := bcrypt.GenerateFromPassword([]byte("init-pass-1"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("GenerateFromPassword: %v", err)
	}
	if _, err := srv.st.CreateUser("rookie", string(hash), store.RoleUser, false); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	w := doLogin(t, h, "rookie", "init-pass-1", "9.9.9.30")
	var login struct {
		User struct {
			MustEnrollMFA bool `json:"must_enroll_mfa"`
		} `json:"user"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &login); err != nil {
		t.Fatalf("decode login: %v", err)
	}
	if !login.User.MustEnrollMFA {
		t.Error("login user.must_enroll_mfa = false, want true for unenrolled account")
	}

	var cookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "session" {
			cookie = c
		}
	}

	rec := mfaRequest(t, h, cookie, "GET", "/api/me", "")
	var me struct {
		MustEnrollMFA bool `json:"must_enroll_mfa"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil {
		t.Fatalf("decode me: %v", err)
	}
	if !me.MustEnrollMFA {
		t.Error("/api/me must_enroll_mfa = false, want true before enrollment")
	}

	enrollMFA(t, h, cookie)

	rec = mfaRequest(t, h, cookie, "GET", "/api/me", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil {
		t.Fatalf("decode me after enroll: %v", err)
	}
	if me.MustEnrollMFA {
		t.Error("/api/me must_enroll_mfa = true after enrollment, want false")
	}
}

// Second login stage: POST /api/login/mfa (login hardening ticket 06).

// loginStageOne runs the password stage for an enrolled account and returns the
// pending token it hands out. Fails the test if the response is not a challenge.
func loginStageOne(t *testing.T, h http.Handler, username, password, ip string) string {
	t.Helper()
	w := doLogin(t, h, username, password, ip)
	if w.Code != http.StatusOK {
		t.Fatalf("password stage status = %d (body: %s)", w.Code, w.Body.String())
	}
	var resp struct {
		OK           bool   `json:"ok"`
		MFARequired  bool   `json:"mfa_required"`
		PendingToken string `json:"mfa_pending_token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode password stage: %v", err)
	}
	if resp.OK || !resp.MFARequired || resp.PendingToken == "" {
		t.Fatalf("password stage did not issue a challenge: %s", w.Body.String())
	}
	return resp.PendingToken
}

// postLoginMFA submits the second stage from ip.
func postLoginMFA(t *testing.T, h http.Handler, body, ip string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/login/mfa", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = ip + ":2000"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// enrolledLoginFixture sets up a system with one enrolled account whose TOTP
// secret and recovery codes are known, returning the secret and the codes.
func enrolledLoginFixture(t *testing.T, srv *Server, h http.Handler) (secret string, codes []string) {
	t.Helper()
	doSetup(t, h, "owner", "a-very-strong-pass")
	cookie, _ := unenrolledSession(t, srv, h, "rookie", "init-pass-1", store.RoleUser)
	return enrollMFA(t, h, cookie)
}

// TestLoginMFA_TOTPCompletesLogin the happy path: a current TOTP code turns the
// pending token into a real session, clears the IP failure counter and books
// login_success with an mfa=totp marker.
func TestLoginMFA_TOTPCompletesLogin(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	secret, _ := enrolledLoginFixture(t, srv, h)

	const ip = "9.9.9.50"
	token := loginStageOne(t, h, "rookie", "init-pass-1", ip)

	rec := postLoginMFA(t, h, fmt.Sprintf(`{"mfa_pending_token":%q,"code":%q}`,
		token, currentTOTP(t, secret)), ip)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		OK   bool `json:"ok"`
		User struct {
			Username string `json:"username"`
		} `json:"user"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.OK || resp.User.Username != "rookie" {
		t.Errorf("response = %s, want ok with the rookie payload", rec.Body.String())
	}
	if sessionCookieOf(rec) == nil {
		t.Fatal("no session cookie after a successful second factor")
	}
	if detail := latestAuditDetail(t, st, "login_success"); !strings.Contains(detail, "mfa=totp") {
		t.Errorf("login_success detail = %q, want it to carry mfa=totp", detail)
	}
	if n := failCountFor(t, st, ip); n != 0 {
		t.Errorf("fail_count = %d after a successful login, want 0", n)
	}
	// The pending token is one-shot: replaying it must not mint a session.
	replay := postLoginMFA(t, h, fmt.Sprintf(`{"mfa_pending_token":%q,"code":%q}`,
		token, currentTOTP(t, secret)), ip)
	if replay.Code != http.StatusUnauthorized {
		t.Errorf("replay status = %d, want 401", replay.Code)
	}
}

// TestLoginMFA_RecoveryCodeCompletesLoginAndBurns a recovery code is the
// fallback factor: it works exactly once and is marked mfa=recovery.
func TestLoginMFA_RecoveryCodeCompletesLoginAndBurns(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	_, codes := enrolledLoginFixture(t, srv, h)

	const ip = "9.9.9.51"
	token := loginStageOne(t, h, "rookie", "init-pass-1", ip)
	rec := postLoginMFA(t, h, fmt.Sprintf(`{"mfa_pending_token":%q,"code":%q}`, token, codes[0]), ip)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	if sessionCookieOf(rec) == nil {
		t.Fatal("no session cookie after recovery-code login")
	}
	if detail := latestAuditDetail(t, st, "login_success"); !strings.Contains(detail, "mfa=recovery") {
		t.Errorf("login_success detail = %q, want it to carry mfa=recovery", detail)
	}

	// The used code is burned: a fresh pending token plus the same code fails.
	token2 := loginStageOne(t, h, "rookie", "init-pass-1", ip)
	again := postLoginMFA(t, h, fmt.Sprintf(`{"mfa_pending_token":%q,"code":%q}`, token2, codes[0]), ip)
	if again.Code != http.StatusUnauthorized {
		t.Fatalf("reused recovery code status = %d, want 401 (body: %s)", again.Code, again.Body.String())
	}
	// That refusal charged the IP counter (same budget as a wrong password),
	// which is exactly what puts the address behind the captcha wall. Clear it
	// so the next password stage measures the recovery batch, not the captcha.
	if err := st.ResetLoginFailures(ip); err != nil {
		t.Fatalf("ResetLoginFailures: %v", err)
	}
	// A different, untouched code from the same batch still works.
	token3 := loginStageOne(t, h, "rookie", "init-pass-1", ip)
	next := postLoginMFA(t, h, fmt.Sprintf(`{"mfa_pending_token":%q,"code":%q}`, token3, codes[1]), ip)
	if next.Code != http.StatusOK {
		t.Errorf("second recovery code status = %d, want 200 (body: %s)", next.Code, next.Body.String())
	}
}

// TestLoginMFA_WrongCodeIsAuditedAndCounted a wrong code is a 401 that books
// mfa_failure (detail carrying the token prefix) and charges the IP counter, so
// grinding codes walks into IP2Ban.
func TestLoginMFA_WrongCodeIsAuditedAndCounted(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	enrolledLoginFixture(t, srv, h)

	const ip = "9.9.9.52"
	token := loginStageOne(t, h, "rookie", "init-pass-1", ip)
	rec := postLoginMFA(t, h, fmt.Sprintf(`{"mfa_pending_token":%q,"code":"000000"}`, token), ip)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body: %s)", rec.Code, rec.Body.String())
	}
	if sessionCookieOf(rec) != nil {
		t.Error("wrong second factor handed out a session cookie")
	}
	detail := latestAuditDetail(t, st, "mfa_failure")
	if !strings.Contains(detail, token[:8]) {
		t.Errorf("mfa_failure detail = %q, want the pending token prefix %q", detail, token[:8])
	}
	if strings.Contains(detail, token) {
		t.Error("mfa_failure detail leaks the full pending token")
	}
	if n := failCountFor(t, st, ip); n != 1 {
		t.Errorf("fail_count = %d after one wrong code, want 1", n)
	}
}

// TestLoginMFA_FailureBudgetDestroysPendingAndBansIP the pending session
// tolerates mfaPendingMaxFailures wrong codes; the same attempts drive the IP
// past the ban threshold (setup seeds 3), so the address ends up banned.
func TestLoginMFA_FailureBudgetDestroysPendingAndBansIP(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	secret, _ := enrolledLoginFixture(t, srv, h)

	const ip = "9.9.9.53"
	token := loginStageOne(t, h, "rookie", "init-pass-1", ip)
	for i := 0; i < mfaPendingMaxFailures; i++ {
		rec := postLoginMFA(t, h, fmt.Sprintf(`{"mfa_pending_token":%q,"code":"000000"}`, token), ip)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d, want 401", i+1, rec.Code)
		}
	}
	if srv.mfaPending.Len() != 0 {
		t.Errorf("pending sessions = %d after the budget was spent, want 0", srv.mfaPending.Len())
	}
	// Even the right code cannot revive a destroyed pending session.
	rec := postLoginMFA(t, h, fmt.Sprintf(`{"mfa_pending_token":%q,"code":%q}`,
		token, currentTOTP(t, secret)), ip)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d after budget exhaustion, want 401", rec.Code)
	}

	banned, err := st.IsBanned(ip, time.Now())
	if err != nil {
		t.Fatalf("IsBanned: %v", err)
	}
	if !banned {
		t.Error("repeated MFA failures did not drive the IP into IP2Ban")
	}
}

// TestLoginMFA_RejectsForeignIPAndUnknownToken the pending token is bound to
// the address that earned it, and an unknown token is simply a 401.
func TestLoginMFA_RejectsForeignIPAndUnknownToken(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	h := srv.Handler()
	secret, _ := enrolledLoginFixture(t, srv, h)

	const ip = "9.9.9.54"
	token := loginStageOne(t, h, "rookie", "init-pass-1", ip)

	code := currentTOTP(t, secret)
	if rec := postLoginMFA(t, h, fmt.Sprintf(`{"mfa_pending_token":%q,"code":%q}`,
		token, code), "9.9.9.99"); rec.Code != http.StatusUnauthorized {
		t.Errorf("foreign-IP submission status = %d, want 401", rec.Code)
	}
	if rec := postLoginMFA(t, h,
		`{"mfa_pending_token":"deadbeef","code":"000000"}`, ip); rec.Code != http.StatusUnauthorized {
		t.Errorf("unknown token status = %d, want 401", rec.Code)
	}
	// The legitimate client on the original address can still finish: a
	// foreign attempt must not spend the budget or destroy the session.
	if rec := postLoginMFA(t, h, fmt.Sprintf(`{"mfa_pending_token":%q,"code":%q}`,
		token, currentTOTP(t, secret)), ip); rec.Code != http.StatusOK {
		t.Errorf("original client status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
}

// TestLoginMFA_TrustIPSkipsNextChallenge trust_ip=true records the grant, and
// the next login from that address goes straight through.
func TestLoginMFA_TrustIPSkipsNextChallenge(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	secret, _ := enrolledLoginFixture(t, srv, h)

	const ip = "9.9.9.55"
	token := loginStageOne(t, h, "rookie", "init-pass-1", ip)
	rec := postLoginMFA(t, h, fmt.Sprintf(`{"mfa_pending_token":%q,"code":%q,"trust_ip":true}`,
		token, currentTOTP(t, secret)), ip)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}

	rookie, err := st.GetUserByUsername("rookie")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	trusted, err := st.IsTrustedIP(rookie.ID, ip)
	if err != nil {
		t.Fatalf("IsTrustedIP: %v", err)
	}
	if !trusted {
		t.Fatal("trust_ip=true did not record a trust grant")
	}

	// Next login from the same address skips the challenge entirely.
	w := doLogin(t, h, "rookie", "init-pass-1", ip)
	if mfaRequiredFlag(t, w) {
		t.Error("trusted address was challenged again")
	}
	if sessionCookieOf(w) == nil {
		t.Error("no session cookie on the trusted follow-up login")
	}
}

// TestLoginMFA_WithoutTrustIPDoesNotGrant the checkbox is opt-in: omitting it
// leaves the address untrusted, so the next login is challenged again.
func TestLoginMFA_WithoutTrustIPDoesNotGrant(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	secret, _ := enrolledLoginFixture(t, srv, h)

	const ip = "9.9.9.56"
	token := loginStageOne(t, h, "rookie", "init-pass-1", ip)
	if rec := postLoginMFA(t, h, fmt.Sprintf(`{"mfa_pending_token":%q,"code":%q}`,
		token, currentTOTP(t, secret)), ip); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}

	rookie, err := st.GetUserByUsername("rookie")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	trusted, err := st.IsTrustedIP(rookie.ID, ip)
	if err != nil {
		t.Fatalf("IsTrustedIP: %v", err)
	}
	if trusted {
		t.Error("a login without trust_ip recorded a trust grant")
	}
	if !mfaRequiredFlag(t, doLogin(t, h, "rookie", "init-pass-1", ip)) {
		t.Error("second login was not challenged despite no trust grant")
	}
}

// TestLoginMFA_MalformedRequests the endpoint is unauthenticated, so its input
// validation is the only boundary. The token is resolved before the code is
// looked at, so anything carrying an unusable token is a 401 regardless of what
// else is missing; only a caller holding a live token gets the "code required"
// 400. An unparseable body never reaches either check.
func TestLoginMFA_MalformedRequests(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	h := srv.Handler()
	enrolledLoginFixture(t, srv, h)

	const ip = "9.9.9.57"
	cases := []struct {
		name string
		body string
		want int
	}{
		{"not json", "not-json", http.StatusBadRequest},
		{"missing token", `{"code":"000000"}`, http.StatusUnauthorized},
		{"unknown token without code", `{"mfa_pending_token":"deadbeef"}`, http.StatusUnauthorized},
		{"empty body", `{}`, http.StatusUnauthorized},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := postLoginMFA(t, h, c.body, ip)
			if rec.Code != c.want {
				t.Errorf("status = %d, want %d (body: %s)", rec.Code, c.want, rec.Body.String())
			}
			if sessionCookieOf(rec) != nil {
				t.Error("malformed second-stage request handed out a session")
			}
		})
	}

	// A live token with no code is the one 400: it is a client bug, not an
	// attempt, so it must not charge the failure budget either.
	token := loginStageOne(t, h, "rookie", "init-pass-1", ip)
	rec := postLoginMFA(t, h, fmt.Sprintf(`{"mfa_pending_token":%q}`, token), ip)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("live token without code status = %d, want 400 (body: %s)", rec.Code, rec.Body.String())
	}
	if srv.mfaPending.Len() != 1 {
		t.Errorf("pending sessions = %d, want 1: a missing code must not spend the handoff", srv.mfaPending.Len())
	}
}

// TestLoginMFA_TrustedLoginRenewalIsRateLimited a trusted-IP login renews the
// grant through TouchTrustedIP, which only writes once per
// TrustedIPRenewInterval. A fresh grant must therefore come out untouched -
// this is what distinguishes a renewal from a blind re-grant (AddTrustedIP
// would move last_used_at on every login). Renewal of an aged row is covered
// in the store package, which owns the clock.
func TestLoginMFA_TrustedLoginRenewalIsRateLimited(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")
	markAllMFAEnrolled(t, st)

	owner, err := st.GetUserByUsername("owner")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	const ip = "9.9.9.58"
	if err := st.AddTrustedIP(owner.ID, ip); err != nil {
		t.Fatalf("AddTrustedIP: %v", err)
	}
	before := trustedGrantFor(t, st, owner.ID, ip)

	if w := doLogin(t, h, "owner", "a-very-strong-pass", ip); mfaRequiredFlag(t, w) {
		t.Fatalf("trusted login was challenged (body: %s)", w.Body.String())
	}

	after := trustedGrantFor(t, st, owner.ID, ip)
	// Within the renewal interval the row must not be rewritten (write
	// reduction is the point of TouchTrustedIP's guard).
	if !after.LastUsedAt.Equal(before.LastUsedAt) {
		t.Errorf("last_used_at moved from %v to %v inside the renewal interval",
			before.LastUsedAt, after.LastUsedAt)
	}
}

// trustedGrantFor returns the single trust grant for (user, ip).
func trustedGrantFor(t *testing.T, st *store.Store, userID int64, ip string) *store.TrustedIP {
	t.Helper()
	grants, err := st.ListTrustedIPs(userID)
	if err != nil {
		t.Fatalf("ListTrustedIPs: %v", err)
	}
	for _, g := range grants {
		if g.IP == ip {
			return g
		}
	}
	t.Fatalf("no trust grant for %s", ip)
	return nil
}
