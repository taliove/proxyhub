package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/taliove/proxyhub/internal/store"
)

// TestLogin_ReturnsUserPayload 登录成功响应必须携带 users 表里的完整身份
// (id/username/role/must_change_password),前端据此判断是否需要强制改密。
func TestLogin_ReturnsUserPayload(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")

	w := doLogin(t, h, "owner", "a-very-strong-pass", "9.9.9.10")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}

	var resp struct {
		OK   bool `json:"ok"`
		User struct {
			ID                 int64  `json:"id"`
			Username           string `json:"username"`
			Role               string `json:"role"`
			MustChangePassword bool   `json:"must_change_password"`
		} `json:"user"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !resp.OK {
		t.Error("ok = false, want true")
	}
	if resp.User.ID <= 0 {
		t.Errorf("user.id = %d, want positive", resp.User.ID)
	}
	if resp.User.Username != "owner" {
		t.Errorf("user.username = %q, want %q", resp.User.Username, "owner")
	}
	if resp.User.Role != store.RoleSuperAdmin {
		t.Errorf("user.role = %q, want %q", resp.User.Role, store.RoleSuperAdmin)
	}
	if resp.User.MustChangePassword {
		t.Error("user.must_change_password = true, want false for setup-created super admin")
	}

	// 与 users 表一致
	u, err := st.GetUserByUsername("owner")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	if u.ID != resp.User.ID {
		t.Errorf("user.id = %d, want %d (from users table)", resp.User.ID, u.ID)
	}
}

// TestLogin_SessionCarriesIdentity 新会话必须带 UserID/Role 载荷,
// 后续请求 requireAuth 才能直接解析出 UserScope(不再依赖 settings KV)。
func TestLogin_SessionCarriesIdentity(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")

	w := doLogin(t, h, "owner", "a-very-strong-pass", "9.9.9.11")
	cookies := w.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("no session cookie")
	}
	payload, ok := srv.sessions.Lookup(cookies[0].Value)
	if !ok {
		t.Fatal("session not found after login")
	}
	if payload.Legacy {
		t.Error("new session marked Legacy=true; ticket 02 must attach identity payload")
	}
	u, _ := st.GetUserByUsername("owner")
	if payload.UserID != u.ID {
		t.Errorf("session UserID = %d, want %d", payload.UserID, u.ID)
	}
	if payload.Role != store.RoleSuperAdmin {
		t.Errorf("session Role = %q, want %q", payload.Role, store.RoleSuperAdmin)
	}
}

// TestLogin_UnknownUser 不存在的用户名:走 dummy bcrypt 比较后仍应返回 401,
// 且不报"用户不存在"(防用户名枚举)。
func TestLogin_UnknownUser(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")

	w := doLogin(t, h, "nobody", "any-password-12ch", "9.9.9.12")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
	// 验证码上线后失败体是 JSON(带 captcha_required 提示前端出图),
	// 但错误文案必须仍是通用的 invalid credentials,不得泄露"用户不存在"。
	var resp struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal %q: %v", w.Body.String(), err)
	}
	if resp.Error != "invalid credentials" {
		t.Errorf("error = %q, want generic 'invalid credentials'", resp.Error)
	}
	if strings.Contains(strings.ToLower(w.Body.String()), "nobody") {
		t.Errorf("body leaks the attempted username: %q", w.Body.String())
	}
}

// TestLogin_DisabledUserRejected 禁用账号凭据即便正确也必须 403,
// 且不触发 IP2Ban 计数(避免误封合法用户所在 IP)。
func TestLogin_DisabledUserRejected(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")

	// 造一个普通用户,然后禁用
	hash, _ := bcrypt.GenerateFromPassword([]byte("member-pass-12ch"), bcrypt.DefaultCost)
	member, err := st.CreateUser("member1", string(hash), store.RoleUser, false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := st.DisableUser(member.ID, time.Now()); err != nil {
		t.Fatalf("DisableUser: %v", err)
	}

	ip := "9.9.9.13"
	w := doLogin(t, h, "member1", "member-pass-12ch", ip)
	if w.Code != http.StatusForbidden {
		t.Errorf("disabled user login status = %d, want 403", w.Code)
	}

	// 禁用失败不应累计 IP 失败阈值:连续多次禁用登录后,正常账号仍能登
	for i := 0; i < 5; i++ {
		doLogin(t, h, "member1", "member-pass-12ch", ip)
	}
	w2 := doLogin(t, h, "owner", "a-very-strong-pass", ip)
	if w2.Code != http.StatusOK {
		t.Errorf("post-disabled login as owner status = %d, want 200 (IP should not be banned)", w2.Code)
	}
}

// TestLogin_UpdatesLastLoginAt 登录成功必须刷新 users.last_login_at,
// 供管理界面展示"最近登录"。
func TestLogin_UpdatesLastLoginAt(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")

	before := time.Now().Add(-time.Second)
	w := doLogin(t, h, "owner", "a-very-strong-pass", "9.9.9.14")
	if w.Code != http.StatusOK {
		t.Fatalf("login status = %d", w.Code)
	}
	u, err := st.GetUserByUsername("owner")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	if u.LastLoginAt == nil {
		t.Fatal("last_login_at = nil after successful login")
	}
	if u.LastLoginAt.Before(before) {
		t.Errorf("last_login_at = %v, want >= %v", u.LastLoginAt, before)
	}
}

// TestLegacySession_ResolvesToFirstSuperAdmin 旧 session(Legacy=true,无身份载荷)
// 在 requireAuth 中必须回退到首个 super_admin,保持 ticket 02 之前的部署
// 在升级后无需强制重新登录。
func TestLegacySession_ResolvesToFirstSuperAdmin(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")

	// 手工注入一条 Legacy 会话,模拟升级前留下的 cookie
	legacyToken, err := srv.sessions.Create() // Create() 默认 Legacy=true
	if err != nil {
		t.Fatalf("Create legacy session: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/me", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: legacyToken})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("legacy session /api/me status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	var resp struct {
		ID       int64  `json:"id"`
		Username string `json:"username"`
		Role     string `json:"role"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	u, _ := st.GetUserByUsername("owner")
	if resp.ID != u.ID || resp.Username != "owner" || resp.Role != store.RoleSuperAdmin {
		t.Errorf("legacy session resolved to %+v, want owner super_admin(id=%d)", resp, u.ID)
	}
}

// TestSetup_CreatesSuperAdminUser handleSetup 除写 settings KV 外,
// 还必须把首账号落到 users 表,否则 ticket 02 的 handleLogin 查不到用户。
func TestSetup_CreatesSuperAdminUser(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()

	w := doSetup(t, h, "owner", "a-very-strong-pass")
	if w.Code != http.StatusOK {
		t.Fatalf("setup status = %d", w.Code)
	}

	u, err := st.GetUserByUsername("owner")
	if err != nil {
		t.Fatalf("GetUserByUsername after setup: %v", err)
	}
	if u.Role != store.RoleSuperAdmin {
		t.Errorf("role = %q, want super_admin", u.Role)
	}
	if u.MustChangePassword {
		t.Error("must_change_password = true, want false for initial setup")
	}
	// 密码哈希应能匹配初始化时设置的密码
	if bcrypt.CompareHashAndPassword([]byte(u.PassHash), []byte("a-very-strong-pass")) != nil {
		t.Error("pass_hash does not match setup password")
	}
}

// TestSetup_ThenLogin_FullRoundTrip 初始化 -> 登录 -> /api/me 全链路:
// 任何一步退回到 settings KV 都会破坏多租户语义。
func TestSetup_ThenLogin_FullRoundTrip(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	h := srv.Handler()

	doSetup(t, h, "owner", "a-very-strong-pass")
	cookie := authCookie(t, h)

	req := httptest.NewRequest("GET", "/api/me", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("/api/me status = %d, want 200", w.Code)
	}
	var resp struct {
		Username string `json:"username"`
		Role     string `json:"role"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Username != "owner" || resp.Role != store.RoleSuperAdmin {
		t.Errorf("/api/me = %+v, want owner super_admin", resp)
	}
}

// 防止回归:body 必须仍能反序列化(登录请求 DTO 没动)
func TestLogin_InvalidJSON(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")

	req := httptest.NewRequest("POST", "/api/login", bytes.NewReader([]byte("not-json")))
	req.RemoteAddr = "9.9.9.15:2000"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for malformed body", w.Code)
	}
}

// Second-factor branch of handleLogin (login hardening ticket 06). The password
// stage has three outcomes once the credentials check out, and their order is
// load bearing: the enrollment check runs before the trusted-IP check, so an
// account that never bound an authenticator can never be waved through by a
// stale trust grant.

// TestLogin_UnenrolledGetsSessionWithEnrollFlag branch one: no authenticator
// bound means a full session plus must_enroll_mfa, never an MFA challenge -
// there is nothing to challenge with yet.
func TestLogin_UnenrolledGetsSessionWithEnrollFlag(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")

	w := doLogin(t, h, "owner", "a-very-strong-pass", "9.9.9.40")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	var resp struct {
		OK          bool `json:"ok"`
		MFARequired bool `json:"mfa_required"`
		User        struct {
			MustEnrollMFA bool `json:"must_enroll_mfa"`
		} `json:"user"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !resp.OK || resp.MFARequired {
		t.Errorf("ok=%v mfa_required=%v, want ok=true mfa_required=false", resp.OK, resp.MFARequired)
	}
	if !resp.User.MustEnrollMFA {
		t.Error("user.must_enroll_mfa = false, want true for an unenrolled account")
	}
	if sessionCookieOf(w) == nil {
		t.Error("no session cookie: an unenrolled account must still get a session to reach enrollment")
	}
}

// TestLogin_EnrolledUntrustedIPGetsPending branch three: bound authenticator on
// an unknown address stops at stage one with a pending token and no session.
func TestLogin_EnrolledUntrustedIPGetsPending(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")
	markAllMFAEnrolled(t, st)

	w := doLogin(t, h, "owner", "a-very-strong-pass", "9.9.9.41")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	var resp struct {
		OK           bool   `json:"ok"`
		MFARequired  bool   `json:"mfa_required"`
		PendingToken string `json:"mfa_pending_token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.OK {
		t.Error("ok = true, want false: the login is not complete yet")
	}
	if !resp.MFARequired {
		t.Error("mfa_required = false, want true")
	}
	if resp.PendingToken == "" {
		t.Error("mfa_pending_token is empty")
	}
	if c := sessionCookieOf(w); c != nil {
		t.Error("session cookie issued before the second factor was verified")
	}
	// No login_success may be booked at stage one.
	if n := auditEventCount(t, st, "login_success"); n != 0 {
		t.Errorf("login_success count = %d, want 0 at the password stage", n)
	}
}

// TestLogin_TrustedIPSkipsSecondFactor branch two: a live trust grant for this
// (user, ip) short-circuits the challenge, records the skip in the audit trail
// and slides the grant's window forward.
func TestLogin_TrustedIPSkipsSecondFactor(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")
	markAllMFAEnrolled(t, st)

	owner, err := st.GetUserByUsername("owner")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	const ip = "9.9.9.42"
	if err := st.AddTrustedIP(owner.ID, ip); err != nil {
		t.Fatalf("AddTrustedIP: %v", err)
	}

	w := doLogin(t, h, "owner", "a-very-strong-pass", ip)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	var resp struct {
		OK          bool `json:"ok"`
		MFARequired bool `json:"mfa_required"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !resp.OK || resp.MFARequired {
		t.Fatalf("ok=%v mfa_required=%v, want ok=true mfa_required=false", resp.OK, resp.MFARequired)
	}
	if sessionCookieOf(w) == nil {
		t.Fatal("no session cookie for a trusted-IP login")
	}

	detail := latestAuditDetail(t, st, "login_success")
	if !strings.Contains(detail, "mfa_skipped=trusted_ip") {
		t.Errorf("login_success detail = %q, want it to carry mfa_skipped=trusted_ip", detail)
	}
	// The skip marker must not feed the trust recommendation engine, which
	// only counts real second-factor successes (detail LIKE '%mfa=%').
	count, err := st.GetTrustRecommendationCount("owner", ip)
	if err != nil {
		t.Fatalf("GetTrustRecommendationCount: %v", err)
	}
	if count != 0 {
		t.Errorf("recommendation count = %d, want 0: a trusted-IP skip must not recommend itself", count)
	}
}

// TestLogin_TrustedIPIsolatedPerUser a grant belongs to one account: another
// user logging in from the same address still faces the challenge.
func TestLogin_TrustedIPIsolatedPerUser(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")

	hash, err := bcrypt.GenerateFromPassword([]byte("member-pass-12ch"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("GenerateFromPassword: %v", err)
	}
	if _, err := st.CreateUser("member1", string(hash), store.RoleUser, false); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	markAllMFAEnrolled(t, st)

	owner, err := st.GetUserByUsername("owner")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	const ip = "9.9.9.43"
	if err := st.AddTrustedIP(owner.ID, ip); err != nil {
		t.Fatalf("AddTrustedIP: %v", err)
	}

	if mfaRequiredFlag(t, doLogin(t, h, "owner", "a-very-strong-pass", ip)) {
		t.Error("owner was challenged from its own trusted IP")
	}
	if !mfaRequiredFlag(t, doLogin(t, h, "member1", "member-pass-12ch", ip)) {
		t.Error("member1 skipped the second factor on another user's trust grant")
	}
}

// TestLogin_ExpiredTrustGrantChallengesAgain an expired grant is not trust:
// the 30 day window closing puts the address back behind the second factor.
func TestLogin_ExpiredTrustGrantChallengesAgain(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")
	markAllMFAEnrolled(t, st)

	owner, err := st.GetUserByUsername("owner")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	const ip = "9.9.9.44"
	if err := st.AddTrustedIP(owner.ID, ip); err != nil {
		t.Fatalf("AddTrustedIP: %v", err)
	}
	// Revoking is the observable equivalent from the handler's point of view:
	// IsTrustedIP reports false either way (grant expiry itself is covered in
	// the store package, which owns the clock).
	if err := st.RevokeTrustedIP(owner.ID, ip); err != nil {
		t.Fatalf("RevokeTrustedIP: %v", err)
	}

	if !mfaRequiredFlag(t, doLogin(t, h, "owner", "a-very-strong-pass", ip)) {
		t.Error("revoked trust grant still skipped the second factor")
	}
}

// TestLogin_LoopbackIsNotAutoTrusted loopback is carved out of the ban,
// honeypot and captcha gates, but not out of MFA: a local address still has to
// prove a second factor, otherwise anything running on the host (or reaching it
// through a misconfigured reverse proxy) would be a free pass.
func TestLogin_LoopbackIsNotAutoTrusted(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")
	markAllMFAEnrolled(t, st)

	w := doLogin(t, h, "owner", "a-very-strong-pass", "127.0.0.1")
	if !mfaRequiredFlag(t, w) {
		t.Errorf("loopback login skipped the second factor (body: %s)", w.Body.String())
	}
	if sessionCookieOf(w) != nil {
		t.Error("loopback login handed out a session before the second factor")
	}
}

// sessionCookieOf returns the session cookie set by a response, or nil.
func sessionCookieOf(w *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range w.Result().Cookies() {
		if c.Name == "session" {
			return c
		}
	}
	return nil
}

// mfaRequiredFlag reads the mfa_required flag off a login response.
func mfaRequiredFlag(t *testing.T, w *httptest.ResponseRecorder) bool {
	t.Helper()
	var resp struct {
		MFARequired bool `json:"mfa_required"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		return false
	}
	return resp.MFARequired
}

// latestAuditDetail returns the detail of the most recent audit row of the
// given type (ListAuditEvents orders newest first).
func latestAuditDetail(t *testing.T, st *store.Store, eventType string) string {
	t.Helper()
	events, _, err := st.ListAuditEvents(store.AuditFilter{EventTypes: []string{eventType}}, 50, 0)
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}
	if len(events) == 0 {
		t.Fatalf("no %s audit event recorded", eventType)
	}
	return events[0].Detail
}

// TestLogin_CaptchaGateStillRunsBeforePassword the MFA branch is downstream of
// the captcha gate: a wrong captcha is refused before the password is even
// checked, so it can never produce a pending token.
func TestLogin_CaptchaGateRunsBeforeMFABranch(t *testing.T) {
	srv, st := newTestServer(t, nil)
	stub := newStubCaptcha()
	srv.captcha = stub
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")
	markAllMFAEnrolled(t, st)
	if err := st.SaveSystemSettings(map[string]string{"captcha_trigger_threshold": "0"}); err != nil {
		t.Fatalf("SaveSystemSettings: %v", err)
	}

	// Right password, missing captcha -> 401 at the captcha gate, no pending.
	w := doLoginCaptcha(t, h, loginBody{Username: "owner", Password: "a-very-strong-pass"}, "9.9.9.45")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 from the captcha gate (body: %s)", w.Code, w.Body.String())
	}
	if mfaRequiredFlag(t, w) {
		t.Error("captcha gate emitted an mfa_required response")
	}
	if srv.mfaPending.Len() != 0 {
		t.Errorf("pending sessions = %d, want 0: the captcha gate must run first", srv.mfaPending.Len())
	}

	// With a solved captcha the request reaches the MFA branch.
	w = doLoginCaptcha(t, h, loginBody{
		Username:      "owner",
		Password:      "a-very-strong-pass",
		CaptchaID:     "stub-challenge",
		CaptchaAnswer: stubGoodAnswer,
	}, "9.9.9.45")
	if !mfaRequiredFlag(t, w) {
		t.Errorf("solved captcha did not reach the MFA branch (status %d, body %s)", w.Code, w.Body.String())
	}
}

// TestLogin_DisabledAccountNeverReachesMFA a disabled account is rejected by
// verifyCredentials, upstream of the branch, so it cannot obtain a pending
// token to grind codes against.
func TestLogin_DisabledAccountNeverReachesMFA(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")

	hash, err := bcrypt.GenerateFromPassword([]byte("member-pass-12ch"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("GenerateFromPassword: %v", err)
	}
	member, err := st.CreateUser("member1", string(hash), store.RoleUser, false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	markAllMFAEnrolled(t, st)
	if err := st.DisableUser(member.ID, time.Now()); err != nil {
		t.Fatalf("DisableUser: %v", err)
	}

	w := doLogin(t, h, "member1", "member-pass-12ch", "9.9.9.46")
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for a disabled account", w.Code)
	}
	if srv.mfaPending.Len() != 0 {
		t.Errorf("pending sessions = %d, want 0 for a disabled account", srv.mfaPending.Len())
	}
}
