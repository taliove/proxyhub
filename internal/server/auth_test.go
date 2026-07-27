package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	if body := w.Body.String(); body != "invalid credentials\n" {
		t.Errorf("body = %q, want generic 'invalid credentials'", body)
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
