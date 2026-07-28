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
