package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/taliove/proxyhub/internal/subscription"
)

// testSitePath 满足 sitepath 校验规则(20 字符、含 4 类字符)的测试用 Site Path。
const testSitePath = "X9k-Qm_2Tz7pLw4Nc8Vb"

// get 发一次 GET 请求并返回记录器。
func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	req.RemoteAddr = "10.0.0.1:1000"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// TestSitePath_DevModePassThrough 未配置 Site Path 时行为与现状完全一致:
// API / 健康检查 / SPA 回退都按原路径可达。
func TestSitePath_DevModePassThrough(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	h := srv.Handler()

	for _, path := range []string{"/api/status", "/healthz", "/", "/login"} {
		if w := get(t, h, path); w.Code != http.StatusOK {
			t.Errorf("GET %s: status = %d, want 200 (dev mode passthrough)", path, w.Code)
		}
	}
	// 未认证访问受保护 API,仍应到达鉴权中间件并返回 401(而非被边界拦截)
	if w := get(t, h, "/api/endpoints"); w.Code != http.StatusUnauthorized {
		t.Errorf("GET /api/endpoints: status = %d, want 401", w.Code)
	}
}

// TestSitePath_ConfiguredPrefixAllows 配置 Site Path 后,管理 UI / API / 订阅在前缀下照常工作。
func TestSitePath_ConfiguredPrefixAllows(t *testing.T) {
	nodes := []*subscription.Node{
		{Name: "香港-01", Type: "ss", Server: "1.2.3.4", Port: 8388,
			Cipher: "aes-256-gcm", Password: "p", Available: true},
	}
	srv, st := newTestServer(t, nodes)
	if err := st.SetSitePath(testSitePath); err != nil {
		t.Fatalf("SetSitePath: %v", err)
	}
	h := srv.Handler()

	for _, path := range []string{
		"/" + testSitePath + "/api/status",
		"/" + testSitePath + "/healthz",
		"/" + testSitePath + "/",
		"/" + testSitePath, // 不带尾斜杠视同前缀根
	} {
		if w := get(t, h, path); w.Code != http.StatusOK {
			t.Errorf("GET %s: status = %d, want 200", path, w.Code)
		}
	}

	// 前缀下未认证访问受保护 API → 401(请求到达了鉴权层)
	if w := get(t, h, "/"+testSitePath+"/api/endpoints"); w.Code != http.StatusUnauthorized {
		t.Errorf("GET /<site-path>/api/endpoints: status = %d, want 401", w.Code)
	}

	// 订阅端点在前缀下完整可用
	ep, err := st.CreateEndpoint("dev")
	if err != nil {
		t.Fatalf("CreateEndpoint: %v", err)
	}
	w := get(t, h, "/"+testSitePath+"/sub/"+ep.Path+"?token="+ep.Token)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /<site-path>/sub/...: status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "proxies") {
		t.Errorf("subscription under prefix missing 'proxies': %s", w.Body.String())
	}
}

// TestSitePath_RootAndPrefixlessReturn404 配置 Site Path 后,根路径与无前缀请求一律普通 404。
func TestSitePath_RootAndPrefixlessReturn404(t *testing.T) {
	nodes := []*subscription.Node{
		{Name: "香港-01", Type: "ss", Server: "1.2.3.4", Port: 8388,
			Cipher: "aes-256-gcm", Password: "p", Available: true},
	}
	srv, st := newTestServer(t, nodes)
	if err := st.SetSitePath(testSitePath); err != nil {
		t.Fatalf("SetSitePath: %v", err)
	}
	h := srv.Handler()

	ep, err := st.CreateEndpoint("dev")
	if err != nil {
		t.Fatalf("CreateEndpoint: %v", err)
	}

	for _, path := range []string{
		"/",
		"/login",
		"/api/status",
		"/healthz",
		"/api/endpoints",
		"/sub/" + ep.Path + "?token=" + ep.Token, // 订阅无前缀同样不可达
	} {
		if w := get(t, h, path); w.Code != http.StatusNotFound {
			t.Errorf("GET %s: status = %d, want 404", path, w.Code)
		}
	}
}

// TestSitePath_WrongPrefixReturns404 错误前缀(含大小写变体、非段边界前缀)一律 404。
func TestSitePath_WrongPrefixReturns404(t *testing.T) {
	srv, st := newTestServer(t, nil)
	if err := st.SetSitePath(testSitePath); err != nil {
		t.Fatalf("SetSitePath: %v", err)
	}
	h := srv.Handler()

	for _, path := range []string{
		"/wrongprefix/api/status",
		"/" + strings.ToLower(testSitePath) + "/api/status", // 前缀大小写敏感
		"/" + testSitePath + "extra/api/status",             // 非路径段边界不算命中前缀
		"/api/" + testSitePath + "/status",                  // 前缀必须位于首段
	} {
		if w := get(t, h, path); w.Code != http.StatusNotFound {
			t.Errorf("GET %s: status = %d, want 404", path, w.Code)
		}
	}
}

// TestSitePath_MiddlewareRewritesNonPrefix404 用哨兵 handler 直接验证中间件:
// 前缀路径改写后下放、其余(含已废弃的 /dist 命名空间)一律 404。
// /dist 曾是流量分发数据面的放行豁免,分发删除后豁免亦移除,/dist 不再可达。
func TestSitePath_MiddlewareRewritesNonPrefix404(t *testing.T) {
	srv, st := newTestServer(t, nil)
	if err := st.SetSitePath(testSitePath); err != nil {
		t.Fatalf("SetSitePath: %v", err)
	}

	type seen struct {
		path string
	}
	var got seen
	sentinel := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.path = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})
	h := srv.sitePathMiddleware(sentinel)

	cases := []struct {
		in       string
		wantCode int
		wantPath string // 仅 wantCode=204 时校验:下放给 mux 的路径
	}{
		{"/" + testSitePath + "/api/status", http.StatusNoContent, "/api/status"},
		{"/" + testSitePath + "/", http.StatusNoContent, "/"},
		{"/" + testSitePath, http.StatusNoContent, "/"},
		{"/dist", http.StatusNotFound, ""},           // 豁免已移除:/dist 不再放行
		{"/dist/node-a/ws", http.StatusNotFound, ""}, // 豁免已移除:/dist 子路径不再放行
		{"/distx", http.StatusNotFound, ""},          // 前缀之外一律 404
		{"/", http.StatusNotFound, ""},
		{"/api/status", http.StatusNotFound, ""},
	}
	for _, c := range cases {
		got.path = ""
		if w := get(t, h, c.in); w.Code != c.wantCode {
			t.Errorf("GET %s: status = %d, want %d", c.in, w.Code, c.wantCode)
			continue
		}
		if c.wantCode == http.StatusNoContent && got.path != c.wantPath {
			t.Errorf("GET %s: downstream path = %q, want %q", c.in, got.path, c.wantPath)
		}
	}
}

// TestSitePath_DevModeMiddlewareNoRewrite 未配置时中间件不改写路径,原样下放。
func TestSitePath_DevModeMiddlewareNoRewrite(t *testing.T) {
	srv, _ := newTestServer(t, nil)

	var gotPath string
	sentinel := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})
	h := srv.sitePathMiddleware(sentinel)

	for _, path := range []string{"/", "/api/status", "/anything"} {
		gotPath = ""
		if w := get(t, h, path); w.Code != http.StatusNoContent {
			t.Errorf("GET %s: status = %d, want 204 (dev mode passthrough)", path, w.Code)
			continue
		}
		if gotPath != path {
			t.Errorf("GET %s: downstream path = %q, want unchanged", path, gotPath)
		}
	}
}

// TestSitePath_SaveSettingsDoesNotOverride 通用设置接口不得改写 Site Path(防后台误改锁死)。
func TestSitePath_SaveSettingsDoesNotOverride(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	// 先完成初始化+登录(开发模式),再配置 Site Path,避免登录请求本身被边界拦截
	cookie := authCookie(t, h)
	if err := st.SetSitePath(testSitePath); err != nil {
		t.Fatalf("SetSitePath: %v", err)
	}

	body := strings.NewReader(`{"site_path":"AnotherPath-1234567890ab","filter_keywords":"x"}`)
	req := httptest.NewRequest("POST", "/"+testSitePath+"/api/settings", body)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("save settings status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}

	p, err := st.GetSitePath()
	if err != nil {
		t.Fatalf("GetSitePath: %v", err)
	}
	if p != testSitePath {
		t.Errorf("site path changed to %q via /api/settings, want unchanged %q", p, testSitePath)
	}
}
