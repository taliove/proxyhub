package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/taliove/proxyhub/internal/store"
)

// loginAs 创建用户并登录,返回 session cookie。失败直接 t.Fatal。
func loginAs(t *testing.T, srv *Server, h http.Handler, username, password, role string) *http.Cookie {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("GenerateFromPassword: %v", err)
	}
	user, err := srv.st.CreateUser(username, string(hash), role, false)
	if err != nil {
		t.Fatalf("CreateUser(%s): %v", username, err)
	}
	markMFAEnrolled(t, srv.st, user.ID)
	w := doLogin(t, h, username, password, "9.9.9.20")
	if w.Code != http.StatusOK {
		t.Fatalf("login %s status = %d (body: %s)", username, w.Code, w.Body.String())
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == "session" {
			return c
		}
	}
	t.Fatalf("no session cookie for %s", username)
	return nil
}

// TestMe_ReturnsUserProfile handleMe 必须返回 users 表 + 配额的完整 profile,
// 字段缺失会破坏前端"刷新后恢复登录态"链路。
func TestMe_ReturnsUserProfile(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")

	// 为 owner 配一份 quota
	owner, err := st.GetUserByUsername("owner")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	if err := st.UpsertUserQuota(&store.UserQuota{
		UserID:       owner.ID,
		MaxAirports:  5,
		MaxEndpoints: 10,
	}); err != nil {
		t.Fatalf("UpsertUserQuota: %v", err)
	}

	cookie := authCookie(t, h)
	req := httptest.NewRequest("GET", "/api/me", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}

	var resp struct {
		ID                 int64  `json:"id"`
		Username           string `json:"username"`
		Role               string `json:"role"`
		MustChangePassword bool   `json:"must_change_password"`
		Quota              *struct {
			UserID       int64 `json:"user_id"`
			MaxAirports  int   `json:"max_airports"`
			MaxEndpoints int   `json:"max_endpoints"`
		} `json:"quota"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v (body: %s)", err, w.Body.String())
	}
	if resp.ID != owner.ID {
		t.Errorf("id = %d, want %d", resp.ID, owner.ID)
	}
	if resp.Username != "owner" {
		t.Errorf("username = %q, want owner", resp.Username)
	}
	if resp.Role != store.RoleSuperAdmin {
		t.Errorf("role = %q, want super_admin", resp.Role)
	}
	if resp.Quota == nil {
		t.Fatal("quota missing in /api/me response")
	}
	if resp.Quota.MaxAirports != 5 || resp.Quota.MaxEndpoints != 10 {
		t.Errorf("quota = %+v, want MaxAirports=5 MaxEndpoints=10", resp.Quota)
	}
}

// TestMe_WithoutQuota 未配置配额的用户:响应不应带 quota 字段(而非 500),
// 前端用 nil 判断"未配置"。
func TestMe_WithoutQuota(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")

	cookie := authCookie(t, h)
	req := httptest.NewRequest("GET", "/api/me", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, has := resp["quota"]; has {
		t.Errorf("quota present but no quota row seeded; body: %s", w.Body.String())
	}
}

// TestMe_RegularUserSeesOwnProfile 普通用户只能看到自己的 profile,
// 即使 super admin 存在也不会越权。
func TestMe_RegularUserSeesOwnProfile(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")

	memberCookie := loginAs(t, srv, h, "member1", "member-pass-12ch", store.RoleUser)

	req := httptest.NewRequest("GET", "/api/me", nil)
	req.AddCookie(memberCookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var resp struct {
		Username string `json:"username"`
		Role     string `json:"role"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Username != "member1" {
		t.Errorf("username = %q, want member1", resp.Username)
	}
	if resp.Role != store.RoleUser {
		t.Errorf("role = %q, want user", resp.Role)
	}
}

// TestMe_NoSession 未登录访问必须 401。
func TestMe_NoSession(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	h := srv.Handler()

	req := httptest.NewRequest("GET", "/api/me", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

// TestMe_UserDeletedMidSession 会话仍有效但 users 表行被删除时,
// handleMe 应视为未授权(401),避免后续操作落到不存在的 user_id。
func TestMe_UserDeletedMidSession(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")

	memberCookie := loginAs(t, srv, h, "member1", "member-pass-12ch", store.RoleUser)

	member, err := st.GetUserByUsername("member1")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	if err := st.DeleteUser(member.ID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/me", nil)
	req.AddCookie(memberCookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 when user deleted", w.Code)
	}
}

// TestChangeMyPassword_MustChangeFlow 首登强改密全链路(ticket 04):
// must_change_password=true 的会话被 requirePasswordChanged 挡在业务路由外
// (403),但 /api/me、/api/me/password、/api/logout 必须可达,否则改密接口
// 把自己锁死。改密成功后旧会话销毁,新密码登录可访问业务路由。
func TestChangeMyPassword_MustChangeFlow(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")

	// 管理员下发的账号走 must_change_password=true 创建路径(ticket 03)。
	hash, err := bcrypt.GenerateFromPassword([]byte("init-pass-1"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("GenerateFromPassword: %v", err)
	}
	rookieUser, err := st.CreateUser("rookie", string(hash), store.RoleUser, true)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	// 本测试的主题是强改密门,不是 MFA 门:先把 MFA 置为已绑定,否则改密通过后
	// 业务路由仍会被 requireMFAEnrolled 挡住,断言测的就不是改密了。
	markMFAEnrolled(t, st, rookieUser.ID)
	w := doLogin(t, h, "rookie", "init-pass-1", "9.9.9.21")
	if w.Code != http.StatusOK {
		t.Fatalf("login status = %d (body: %s)", w.Code, w.Body.String())
	}
	var cookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "session" {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("no session cookie after login")
	}

	do := func(method, path, body string) *httptest.ResponseRecorder {
		var req *http.Request
		if body != "" {
			req = httptest.NewRequest(method, path, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
		} else {
			req = httptest.NewRequest(method, path, nil)
		}
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	// 业务路由:403 + must_change_password 标记(前端据此跳改密页)。
	if rec := do("GET", "/api/endpoints", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("GET /api/endpoints status = %d, want 403", rec.Code)
	}
	// 豁免面:读自身状态、改自己密码必须可达。
	if rec := do("GET", "/api/me", ""); rec.Code != http.StatusOK {
		t.Fatalf("GET /api/me status = %d, want 200 (exempt from password gate)", rec.Code)
	}
	if rec := do("POST", "/api/me/password", `{"old_password":"wrong","new_password":"new-pass-123"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /api/me/password wrong-old status = %d, want 400 (endpoint reachable)", rec.Code)
	}
	// 登出同样豁免,否则首登用户连"换个账号登录"都做不到。
	if rec := do("POST", "/api/logout", ""); rec.Code != http.StatusOK {
		t.Fatalf("POST /api/logout status = %d, want 200 (exempt from password gate)", rec.Code)
	}

	// 重新登录拿到会话,完成改密。
	w = doLogin(t, h, "rookie", "init-pass-1", "9.9.9.21")
	for _, c := range w.Result().Cookies() {
		if c.Name == "session" {
			cookie = c
		}
	}
	rec := do("POST", "/api/me/password", `{"old_password":"init-pass-1","new_password":"new-pass-123"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/me/password status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}

	rookie, err := st.GetUserByUsername("rookie")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	if rookie.MustChangePassword {
		t.Error("must_change_password still true after successful change")
	}

	// 旧会话已销毁:再用旧 cookie 访问应 401。
	if rec := do("GET", "/api/me", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("GET /api/me with destroyed session status = %d, want 401", rec.Code)
	}

	// 新密码登录后业务路由放行。
	w = doLogin(t, h, "rookie", "new-pass-123", "9.9.9.21")
	if w.Code != http.StatusOK {
		t.Fatalf("re-login with new password status = %d", w.Code)
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == "session" {
			cookie = c
		}
	}
	if rec := do("GET", "/api/endpoints", ""); rec.Code != http.StatusOK {
		t.Errorf("GET /api/endpoints after password change status = %d, want 200", rec.Code)
	}
}
