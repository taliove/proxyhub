package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/taliove/proxyhub/internal/captcha"
	"github.com/taliove/proxyhub/internal/store"
)

// stubCaptcha is a deterministic captcha seam for login tests: the only
// accepted answer is stubGoodAnswer, and every submitted id is consumed
// (right or wrong, like the real service) so the single-use contract is
// observable from the handler layer.
type stubCaptcha struct {
	mu       sync.Mutex
	issued   int
	consumed map[string]bool
	issueErr error
}

const stubGoodAnswer = "GOOD42"

func newStubCaptcha() *stubCaptcha {
	return &stubCaptcha{consumed: map[string]bool{}}
}

func (s *stubCaptcha) Issue(ip string) (captcha.Challenge, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.issueErr != nil {
		return captcha.Challenge{}, s.issueErr
	}
	s.issued++
	return captcha.Challenge{ID: "stub-challenge", ImageBase64: "data:image/png;base64,AAAA"}, nil
}

func (s *stubCaptcha) Verify(id, answer string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id == "" || s.consumed[id] {
		return false
	}
	s.consumed[id] = true
	return answer == stubGoodAnswer
}

// loginBody is the login request payload including the captcha fields.
type loginBody struct {
	Username      string `json:"username"`
	Password      string `json:"password"`
	CaptchaID     string `json:"captcha_id,omitempty"`
	CaptchaAnswer string `json:"captcha_answer,omitempty"`
}

// doLoginCaptcha posts a login request carrying captcha fields.
func doLoginCaptcha(t *testing.T, h http.Handler, body loginBody, ip string) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/login", bytes.NewReader(raw))
	req.RemoteAddr = ip + ":2000"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// captchaRequiredFlag reads the captcha_required flag off a JSON response.
func captchaRequiredFlag(t *testing.T, w *httptest.ResponseRecorder) bool {
	t.Helper()
	var resp struct {
		CaptchaRequired bool `json:"captcha_required"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal login response %q: %v", w.Body.String(), err)
	}
	return resp.CaptchaRequired
}

// auditEventCount counts audit rows of the given type.
func auditEventCount(t *testing.T, st *store.Store, eventType string) int {
	t.Helper()
	events, _, err := st.ListAuditEvents(store.AuditFilter{EventTypes: []string{eventType}}, 100, 0)
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}
	return len(events)
}

// failCountFor reads banned_ips.fail_count for an IP (0 when absent).
func failCountFor(t *testing.T, st *store.Store, ip string) int {
	t.Helper()
	rows, err := st.ListBannedIPs()
	if err != nil {
		t.Fatalf("ListBannedIPs: %v", err)
	}
	for _, r := range rows {
		if r.IP == ip {
			return r.FailCount
		}
	}
	return 0
}

// TestCaptchaEndpoint_IssuesChallengeWithoutAuth GET /api/captcha is public
// (the login page has no session yet) and returns an id plus a PNG data URL.
func TestCaptchaEndpoint_IssuesChallengeWithoutAuth(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	h := srv.Handler()

	req := httptest.NewRequest("GET", "/api/captcha", nil)
	req.RemoteAddr = "9.8.7.1:3000"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	var resp struct {
		ChallengeID string `json:"challenge_id"`
		ImageBase64 string `json:"image_base64"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.ChallengeID == "" {
		t.Error("challenge_id empty")
	}
	if !strings.HasPrefix(resp.ImageBase64, "data:image/png;base64,") {
		t.Errorf("image_base64 not a PNG data URL: %.32q", resp.ImageBase64)
	}
}

// TestCaptchaEndpoint_RateLimitedAfter30PerMinute the 31st issue from the
// same IP inside a minute must be refused with 429.
func TestCaptchaEndpoint_RateLimitedAfter30PerMinute(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	h := srv.Handler()

	issue := func() int {
		req := httptest.NewRequest("GET", "/api/captcha", nil)
		req.RemoteAddr = "9.8.7.2:3000"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w.Code
	}

	for i := 0; i < captcha.IssueRateLimit; i++ {
		if code := issue(); code != http.StatusOK {
			t.Fatalf("issue #%d status = %d, want 200", i+1, code)
		}
	}
	if code := issue(); code != http.StatusTooManyRequests {
		t.Errorf("issue #%d status = %d, want 429", captcha.IssueRateLimit+1, code)
	}

	// The budget is per IP: another source still gets a challenge.
	req := httptest.NewRequest("GET", "/api/captcha", nil)
	req.RemoteAddr = "9.8.7.3:3000"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("other IP status = %d, want 200", w.Code)
	}
}

// TestSecurityPolicy_CaptchaTriggerThreshold the new setting parses like
// ban_threshold, defaults to 1, and accepts 0 (captcha always on).
func TestSecurityPolicy_CaptchaTriggerThreshold(t *testing.T) {
	srv, st := newTestServer(t, nil)

	if got := srv.loadSecurityPolicy().CaptchaTriggerThreshold; got != defaultCaptchaTriggerThreshold {
		t.Errorf("default threshold = %d, want %d", got, defaultCaptchaTriggerThreshold)
	}

	cases := []struct {
		value string
		want  int
	}{
		{"3", 3},
		{"0", 0},
		{"", defaultCaptchaTriggerThreshold},
		{"not-a-number", defaultCaptchaTriggerThreshold},
		{"-2", defaultCaptchaTriggerThreshold},
	}
	for _, c := range cases {
		if err := st.SetSetting("captcha_trigger_threshold", c.value); err != nil {
			t.Fatalf("SetSetting(%q): %v", c.value, err)
		}
		if got := srv.loadSecurityPolicy().CaptchaTriggerThreshold; got != c.want {
			t.Errorf("threshold for %q = %d, want %d", c.value, got, c.want)
		}
	}
}

// TestLogin_CaptchaRequiredAfterFailureThreshold with the default threshold
// of 1, a single wrong password puts the IP behind the captcha wall: the
// next attempt is refused before the password is even checked.
func TestLogin_CaptchaRequiredAfterFailureThreshold(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")
	ip := "9.8.8.1"

	first := doLoginCaptcha(t, h, loginBody{Username: "owner", Password: "wrong-password"}, ip)
	if first.Code != http.StatusUnauthorized {
		t.Fatalf("first attempt status = %d, want 401", first.Code)
	}
	if !captchaRequiredFlag(t, first) {
		t.Error("first failure response missing captcha_required=true")
	}

	// Correct password without a captcha must still be refused.
	second := doLoginCaptcha(t, h, loginBody{Username: "owner", Password: "a-very-strong-pass"}, ip)
	if second.Code != http.StatusUnauthorized {
		t.Fatalf("second attempt status = %d, want 401 (captcha wall)", second.Code)
	}
	if !captchaRequiredFlag(t, second) {
		t.Error("captcha wall response missing captcha_required=true")
	}
	if len(second.Result().Cookies()) != 0 {
		t.Error("captcha wall handed out a session cookie")
	}
}

// TestLogin_CaptchaRequiredFromFirstAttemptWhenThresholdZero threshold=0
// means always on: an IP with no failure record at all (fail_count NULL /
// row absent, treated as 0) already needs a captcha.
func TestLogin_CaptchaRequiredFromFirstAttemptWhenThresholdZero(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")
	if err := st.SetSetting("captcha_trigger_threshold", "0"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	w := doLoginCaptcha(t, h, loginBody{Username: "owner", Password: "a-very-strong-pass"}, "9.8.8.2")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 with threshold=0", w.Code)
	}
	if !captchaRequiredFlag(t, w) {
		t.Error("missing captcha_required=true with threshold=0")
	}
}

// TestLogin_LoopbackExemptFromCaptcha loopback keeps the existing ban and
// honeypot exemptions and additionally never sees a captcha.
func TestLogin_LoopbackExemptFromCaptcha(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")
	if err := st.SetSetting("captcha_trigger_threshold", "0"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	w := doLoginCaptcha(t, h, loginBody{Username: "owner", Password: "a-very-strong-pass"}, "127.0.0.1")
	if w.Code != http.StatusOK {
		t.Fatalf("loopback login status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
}

// TestLogin_WrongCaptchaCountsAsFailureAndAudits a wrong answer is a login
// failure: fail_count grows on the same counter as password errors and a
// captcha_failure audit row is written. The password is never checked.
func TestLogin_WrongCaptchaCountsAsFailureAndAudits(t *testing.T) {
	srv, st := newTestServer(t, nil)
	stub := newStubCaptcha()
	srv.captcha = stub
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")
	if err := st.SetSetting("captcha_trigger_threshold", "0"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	ip := "9.8.8.3"

	w := doLoginCaptcha(t, h, loginBody{
		Username:      "owner",
		Password:      "a-very-strong-pass",
		CaptchaID:     "stub-challenge",
		CaptchaAnswer: "NOPE99",
	}, ip)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for wrong captcha", w.Code)
	}
	if !captchaRequiredFlag(t, w) {
		t.Error("wrong captcha response missing captcha_required=true")
	}
	if got := failCountFor(t, st, ip); got != 1 {
		t.Errorf("fail_count = %d, want 1 (captcha errors share the login failure counter)", got)
	}
	if got := auditEventCount(t, st, "captcha_failure"); got != 1 {
		t.Errorf("captcha_failure audit rows = %d, want 1", got)
	}
	if got := auditEventCount(t, st, "login_failure"); got != 0 {
		t.Errorf("login_failure rows = %d, want 0 (password must not be checked)", got)
	}
}

// TestLogin_WrongCaptchaTripsIP2Ban captcha failures feed the same
// threshold as password failures, so they eventually ban the IP.
func TestLogin_WrongCaptchaTripsIP2Ban(t *testing.T) {
	srv, st := newTestServer(t, nil)
	srv.captcha = newStubCaptcha()
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass") // ban_threshold = 3
	if err := st.SetSetting("captcha_trigger_threshold", "0"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	ip := "9.8.8.4"

	for i := 0; i < 3; i++ {
		doLoginCaptcha(t, h, loginBody{
			Username: "owner", Password: "a-very-strong-pass",
			CaptchaID: "stub-challenge", CaptchaAnswer: "NOPE99",
		}, ip)
	}
	banned, err := st.IsBanned(ip, time.Now())
	if err != nil {
		t.Fatalf("IsBanned: %v", err)
	}
	if !banned {
		t.Error("IP not banned after captcha failures reached ban_threshold")
	}
}

// TestLogin_CorrectCaptchaFallsThroughToPassword a valid answer only clears
// the captcha gate; the password check still decides the outcome, and the
// consumed challenge cannot be replayed.
func TestLogin_CorrectCaptchaFallsThroughToPassword(t *testing.T) {
	srv, st := newTestServer(t, nil)
	stub := newStubCaptcha()
	srv.captcha = stub
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")
	if err := st.SetSetting("captcha_trigger_threshold", "0"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	ip := "9.8.8.5"

	ok := doLoginCaptcha(t, h, loginBody{
		Username: "owner", Password: "a-very-strong-pass",
		CaptchaID: "stub-challenge", CaptchaAnswer: stubGoodAnswer,
	}, ip)
	if ok.Code != http.StatusOK {
		t.Fatalf("valid captcha + valid password status = %d, want 200 (body: %s)", ok.Code, ok.Body.String())
	}

	// Replaying the same id+answer must fail: the challenge was consumed.
	replay := doLoginCaptcha(t, h, loginBody{
		Username: "owner", Password: "a-very-strong-pass",
		CaptchaID: "stub-challenge", CaptchaAnswer: stubGoodAnswer,
	}, ip)
	if replay.Code != http.StatusUnauthorized {
		t.Errorf("replayed challenge status = %d, want 401", replay.Code)
	}
}

// TestLogin_WrongPasswordWithValidCaptcha the password error path keeps its
// own audit event and failure accounting once the captcha gate is cleared.
func TestLogin_WrongPasswordWithValidCaptcha(t *testing.T) {
	srv, st := newTestServer(t, nil)
	srv.captcha = newStubCaptcha()
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")
	if err := st.SetSetting("captcha_trigger_threshold", "0"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	ip := "9.8.8.6"

	w := doLoginCaptcha(t, h, loginBody{
		Username: "owner", Password: "wrong-password",
		CaptchaID: "stub-challenge", CaptchaAnswer: stubGoodAnswer,
	}, ip)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if !captchaRequiredFlag(t, w) {
		t.Error("password failure response missing captcha_required=true (client must render a new captcha)")
	}
	if got := auditEventCount(t, st, "login_failure"); got != 1 {
		t.Errorf("login_failure rows = %d, want 1", got)
	}
	if got := auditEventCount(t, st, "captcha_failure"); got != 0 {
		t.Errorf("captcha_failure rows = %d, want 0", got)
	}
}

// TestLogin_HoneypotBeatsCaptcha ordering guard: a honeypot username is an
// instant ban even when the captcha gate would have rejected the request.
func TestLogin_HoneypotBeatsCaptcha(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")
	if err := st.SetSetting("captcha_trigger_threshold", "0"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	ip := "9.8.8.7"

	w := doLoginCaptcha(t, h, loginBody{Username: "admin", Password: "whatever"}, ip)
	if w.Code != http.StatusForbidden {
		t.Fatalf("honeypot status = %d, want 403 (honeypot must run before captcha)", w.Code)
	}
	banned, _ := st.IsBanned(ip, time.Now())
	if !banned {
		t.Error("honeypot hit did not ban the IP")
	}
	if got := auditEventCount(t, st, "honeypot_ban"); got != 1 {
		t.Errorf("honeypot_ban rows = %d, want 1", got)
	}
}

// TestLogin_RealCaptchaRoundTrip drives the production captcha service end to
// end: issue via GET /api/captcha, solve it, log in. The stub-based tests pin
// the handler contract; this one proves the real wiring works.
func TestLogin_RealCaptchaRoundTrip(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")
	if err := st.SetSetting("captcha_trigger_threshold", "0"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	ip := "9.8.9.1"

	// Default wiring must be the real service (guards against a stub leaking
	// into production), then swap in one whose answer is known so the
	// rendered image can be "solved" without OCR. Everything else (rendering,
	// store, TTL, throttle) stays real.
	if _, ok := srv.captcha.(*captcha.Service); !ok {
		t.Fatalf("srv.captcha type = %T, want *captcha.Service by default", srv.captcha)
	}
	const known = "K7NPQR"
	srv.captcha = captcha.NewService(captcha.Options{
		NewAnswer: func() (string, error) { return known, nil },
	})

	issue := func() string {
		t.Helper()
		req := httptest.NewRequest("GET", "/api/captcha", nil)
		req.RemoteAddr = ip + ":3000"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("issue status = %d, want 200", rec.Code)
		}
		var issued struct {
			ChallengeID string `json:"challenge_id"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &issued); err != nil {
			t.Fatalf("unmarshal issue: %v", err)
		}
		return issued.ChallengeID
	}

	// Wrong answer first: refused, and the challenge is burned by the attempt.
	burned := issue()
	bad := doLoginCaptcha(t, h, loginBody{
		Username: "owner", Password: "a-very-strong-pass",
		CaptchaID: burned, CaptchaAnswer: "ZZZZZZ",
	}, ip)
	if bad.Code != http.StatusUnauthorized {
		t.Fatalf("wrong answer status = %d, want 401", bad.Code)
	}
	retry := doLoginCaptcha(t, h, loginBody{
		Username: "owner", Password: "a-very-strong-pass",
		CaptchaID: burned, CaptchaAnswer: known,
	}, ip)
	if retry.Code != http.StatusUnauthorized {
		t.Fatalf("retry on the burned challenge status = %d, want 401 (submit consumes it either way)", retry.Code)
	}

	// A fresh challenge does let the user in. Lowercase on purpose: the
	// accepted answer is case insensitive.
	good := doLoginCaptcha(t, h, loginBody{
		Username: "owner", Password: "a-very-strong-pass",
		CaptchaID: issue(), CaptchaAnswer: strings.ToLower(known),
	}, ip)
	if good.Code != http.StatusOK {
		t.Fatalf("solved captcha login status = %d, want 200 (body: %s)", good.Code, good.Body.String())
	}
}

// TestLogin_BannedIPGetsNoCaptchaPrompt a banned IP is rejected with 403
// before any captcha logic runs.
func TestLogin_BannedIPGetsNoCaptchaPrompt(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")
	if err := st.SetSetting("captcha_trigger_threshold", "0"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	ip := "9.8.8.8"
	if _, err := st.BanIP(ip, time.Hour, time.Now()); err != nil {
		t.Fatalf("BanIP: %v", err)
	}

	w := doLoginCaptcha(t, h, loginBody{Username: "owner", Password: "a-very-strong-pass"}, ip)
	if w.Code != http.StatusForbidden {
		t.Errorf("banned IP status = %d, want 403", w.Code)
	}
}

// TestLogin_DisabledAccountSkipsCaptchaAccounting a disabled account with
// correct credentials must stay on its own 403 path once the captcha gate
// is cleared, without touching the IP failure counter.
func TestLogin_DisabledAccountSkipsCaptchaAccounting(t *testing.T) {
	srv, st := newTestServer(t, nil)
	srv.captcha = newStubCaptcha()
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")
	if err := st.SetSetting("captcha_trigger_threshold", "0"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte("member-pass-12ch"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("GenerateFromPassword: %v", err)
	}
	member, err := st.CreateUser("member9", string(hash), store.RoleUser, false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := st.DisableUser(member.ID, time.Now()); err != nil {
		t.Fatalf("DisableUser: %v", err)
	}

	ip := "9.8.8.9"
	w := doLoginCaptcha(t, h, loginBody{
		Username: "member9", Password: "member-pass-12ch",
		CaptchaID: "stub-challenge", CaptchaAnswer: stubGoodAnswer,
	}, ip)
	if w.Code != http.StatusForbidden {
		t.Fatalf("disabled account status = %d, want 403", w.Code)
	}
	if got := failCountFor(t, st, ip); got != 0 {
		t.Errorf("fail_count = %d, want 0 for disabled account", got)
	}
}
