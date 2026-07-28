package server

import (
	"bytes"
	"embed"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

	"github.com/taliove/proxyhub/internal/config"
	"github.com/taliove/proxyhub/internal/detection"
	"github.com/taliove/proxyhub/internal/geoip"
	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

// fakeNodes 实现 NodeSource
type fakeNodes struct {
	nodes       []*subscription.Node
	refreshErr  error // StartRefreshJob 返回的错误（模拟刷新冲突等场景）
	purgeErr    error // PurgeAirportNodes 返回的错误（模拟清空与刷新冲突等场景）
	lastTrigger string
	// lastRefreshUserID 记录 StartRefreshJobForUser 收到的属主(多租户断言用)
	lastRefreshUserID int64
	// testExclusiveErr StartAirportTestExclusive 返回的错误(模拟跨 kind 互斥 409)
	testExclusiveErr error
	// testConflictChecker 记录装配期注入的测试侧冲突查询(验证跨 kind 装配)
	testConflictChecker func(airportID int64) (string, bool)
}

func (f *fakeNodes) Nodes() []*subscription.Node { return f.nodes }
func (f *fakeNodes) LastUpdate() time.Time       { return time.Now() }

// ticket 07 multi-tenant stubs: delegate to legacy single-user behavior.
// NodesForUser 按节点 UserID 过滤:UserID=0(未归属/测试夹具)或与请求 userID 相同的保留;
// userID<=0 视为全局视角,不过滤。其余 ForUser 桩仍委托旧单用户行为。
func (f *fakeNodes) NodesForUser(userID int64) []*subscription.Node {
	if userID <= 0 {
		return f.nodes
	}
	out := make([]*subscription.Node, 0, len(f.nodes))
	for _, n := range f.nodes {
		if n.UserID == 0 || n.UserID == userID {
			out = append(out, n)
		}
	}
	return out
}
func (f *fakeNodes) LastUpdateForUser(userID int64) time.Time       { return time.Now() }

func (f *fakeNodes) StartRefreshJob(trigger string) (int64, string, bool, error) {
	f.lastTrigger = trigger
	if f.refreshErr != nil {
		return 0, "all", false, f.refreshErr
	}
	return 42, "all", true, nil
}

func (f *fakeNodes) CancelRefresh(string) bool { return true }

// ticket 07 multi-tenant stubs.
func (f *fakeNodes) StartRefreshJobForUser(userID int64, trigger string) (int64, string, bool, error) {
	f.lastRefreshUserID = userID
	return f.StartRefreshJob(trigger)
}
func (f *fakeNodes) StartAirportRefreshJobForUser(userID int64, trigger string, airportID int64) (int64, string, bool, error) {
	return f.StartAirportRefreshJob(trigger, airportID)
}
func (f *fakeNodes) CancelRefreshForUser(userID int64, key string) bool { return true }
func (f *fakeNodes) UpdateNodeTestResultForUser(userID int64, nodeKey, mode string, available bool, latency int, downMbps, upMbps float64, failReason, failDetail string) bool {
	return f.UpdateNodeTestResult(nodeKey, mode, available, latency, downMbps, upMbps, failReason, failDetail)
}

// StartAirportTestExclusive 测试 mock:默认直接在"临界区"内调 start
// (fake 无并发,无 TOCTOU);testExclusiveErr 非空时模拟跨 kind 冲突。
func (f *fakeNodes) StartAirportTestExclusive(airportID int64, start func() (int64, string, bool, error)) (int64, string, bool, error) {
	if f.testExclusiveErr != nil {
		return 0, "airport-1", false, f.testExclusiveErr
	}
	return start()
}

// SetAirportTestConflictChecker 测试 mock:记录注入的回调供断言。
func (f *fakeNodes) SetAirportTestConflictChecker(fn func(airportID int64) (string, bool)) {
	f.testConflictChecker = fn
}

func (f *fakeNodes) StartAirportRefreshJob(trigger string, airportID int64) (int64, string, bool, error) {
	f.lastTrigger = trigger
	if f.refreshErr != nil {
		return 0, "", false, f.refreshErr
	}
	return 43, "airport-1", true, nil
}

func (f *fakeNodes) UpdateNodeTestResult(nodeKey, mode string, available bool, latency int, downMbps, upMbps float64, failReason, failDetail string) bool {
	// 测试 mock：查找并更新节点，模拟真实行为
	for _, n := range f.nodes {
		if n.NodeKey() == nodeKey {
			if mode == "bandwidth" {
				n.BandwidthDownMbps = downMbps
				n.BandwidthUpMbps = upMbps
			} else {
				n.Available = available
				n.Latency = latency
				n.DetectionFailReason = failReason
				n.DetectionFailDetail = failDetail
			}
			return true
		}
	}
	return false
}

// UpdateNodeIdentity 测试 mock：按 NodeKey 命中后替换节点对象(不可变语义),
// 与真实 Aggregator 行为一致。name/region 为空表示不改该字段。
func (f *fakeNodes) UpdateNodeIdentity(nodeKey, name, region string) bool {
	return f.UpdateNodeIdentityForUser(0, nodeKey, name, region)
}

// UpdateNodeIdentityForUser ticket 07 多租户桩:与 UpdateNodeIdentity 同语义。
func (f *fakeNodes) UpdateNodeIdentityForUser(userID int64, nodeKey, name, region string) bool {
	for i, n := range f.nodes {
		if n.NodeKey() != nodeKey {
			continue
		}
		updated := *n
		if name != "" {
			updated.Name = name
		}
		if region != "" {
			updated.Region = region
		}
		f.nodes[i] = &updated
		return true
	}
	return false
}

// PurgeAirportNodes 测试 mock：剔除机场节点、保留自建节点，与真实 Aggregator 行为一致。
func (f *fakeNodes) PurgeAirportNodes() (int, error) {
	return f.PurgeAirportNodesForUser(0)
}

// PurgeAirportNodesForUser ticket 07 多租户桩:与 PurgeAirportNodes 同语义。
func (f *fakeNodes) PurgeAirportNodesForUser(userID int64) (int, error) {
	if f.purgeErr != nil {
		return 0, f.purgeErr
	}
	var kept []*subscription.Node
	for _, n := range f.nodes {
		if n.Source == subscription.SourceSelfHosted {
			kept = append(kept, n)
		}
	}
	removed := len(f.nodes) - len(kept)
	f.nodes = kept
	return removed, nil
}

func newTestServer(t *testing.T, nodes []*subscription.Node) (*Server, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { st.Close() })

	cfg := &config.Config{}
	logger := slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil))
	// 测试不关心 SPA 静态资源，传入空 embed.FS（handleSPA 会优雅报错，不影响 API 测试）
	var emptyFS embed.FS
	geo := geoip.NewResolver(st, "")

	fakeNodeSource := &fakeNodes{nodes: nodes}

	// 构造真实的 DetectionService 以支持节点测试 handler
	detector := detection.NewDetector(4, time.Second, time.Second)
	detectionService := NewDetectionService(
		detector,
		st,
		logger,
		fakeNodeSource.Nodes,
		st.GetDetectionTargets,
	)

	srv := New(cfg, st, fakeNodeSource, emptyFS, logger, detectionService, geo)
	registerTestStore(t, st)
	return srv, st
}

// testStores lets handler-only fixtures (which receive an http.Handler but not
// the Store) reach the store of the server built for the current test. Needed
// because MFA enrollment is mandatory: a fixture that only has a cookie still
// has to mark the account enrolled. Keyed by test name, cleaned up with the
// test.
var (
	testStoresMu sync.Mutex
	testStores   = map[string]*store.Store{}
)

func registerTestStore(t *testing.T, st *store.Store) {
	t.Helper()
	testStoresMu.Lock()
	testStores[t.Name()] = st
	testStoresMu.Unlock()
	t.Cleanup(func() {
		testStoresMu.Lock()
		delete(testStores, t.Name())
		testStoresMu.Unlock()
	})
}

// testStore returns the store registered by newTestServer for this test.
func testStore(t *testing.T) *store.Store {
	t.Helper()
	testStoresMu.Lock()
	defer testStoresMu.Unlock()
	st, ok := testStores[t.Name()]
	if !ok {
		t.Fatalf("no store registered for %s (was the server built by newTestServer?)", t.Name())
	}
	return st
}

// doSetup 走一遍初始化向导
func doSetup(t *testing.T, h http.Handler, username, password string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"username": username,
		"password": password,
		"security": map[string]any{"ban_threshold": 3, "ban_duration": "1h"},
	})
	req := httptest.NewRequest("POST", "/api/setup", bytes.NewReader(body))
	req.RemoteAddr = "10.0.0.1:1000"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func doLogin(t *testing.T, h http.Handler, username, password, ip string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	req := httptest.NewRequest("POST", "/api/login", bytes.NewReader(body))
	req.RemoteAddr = ip + ":2000"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestStatus_Uninitialized(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	h := srv.Handler()

	req := httptest.NewRequest("GET", "/api/status", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp map[string]bool
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["initialized"] {
		t.Error("initialized = true, want false before setup")
	}
}

func TestSetup_Success(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()

	w := doSetup(t, h, "owner", "a-very-strong-pass")
	if w.Code != http.StatusOK {
		t.Fatalf("setup status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}

	init, _ := st.IsSystemInitialized()
	if !init {
		t.Error("system not marked initialized after setup")
	}
}

func TestSetup_RejectsHoneypotUsername(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	h := srv.Handler()

	for _, name := range []string{"admin", "root", "Administrator", "ROOT"} {
		w := doSetup(t, h, name, "a-very-strong-pass")
		if w.Code != http.StatusBadRequest {
			t.Errorf("setup with honeypot %q: status = %d, want 400", name, w.Code)
		}
	}
}

func TestSetup_RejectsShortPassword(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	h := srv.Handler()

	w := doSetup(t, h, "owner", "short")
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for short password", w.Code)
	}
}

func TestSetup_AlreadyInitialized(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	h := srv.Handler()

	doSetup(t, h, "owner", "a-very-strong-pass")
	w := doSetup(t, h, "owner2", "another-strong-pass")
	if w.Code != http.StatusBadRequest {
		t.Errorf("second setup status = %d, want 400", w.Code)
	}
}

func TestLogin_Success(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")

	w := doLogin(t, h, "owner", "a-very-strong-pass", "9.9.9.1")
	if w.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) == 0 || cookies[0].Name != "session" {
		t.Error("no session cookie set on successful login")
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")

	w := doLogin(t, h, "owner", "wrong-password", "9.9.9.2")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestLogin_HoneypotInstantBan(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")

	ip := "6.6.6.6"
	// 单次针对 admin 的尝试即应触发封禁
	w := doLogin(t, h, "admin", "whatever", ip)
	if w.Code != http.StatusForbidden {
		t.Fatalf("honeypot login status = %d, want 403", w.Code)
	}

	banned, _ := st.IsBanned(ip, time.Now())
	if !banned {
		t.Error("honeypot hit did not ban the IP")
	}

	// 即便随后用正确凭据也应被拒（IP 已封）
	w2 := doLogin(t, h, "owner", "a-very-strong-pass", ip)
	if w2.Code != http.StatusForbidden {
		t.Errorf("post-ban login status = %d, want 403", w2.Code)
	}
}

func TestLogin_IP2BanAfterThreshold(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass") // threshold=3

	ip := "7.7.7.7"
	for i := 0; i < 3; i++ {
		doLogin(t, h, "owner", "wrong-password", ip)
	}
	// 第 4 次即使密码正确也被封禁拦截
	w := doLogin(t, h, "owner", "a-very-strong-pass", ip)
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 after exceeding threshold", w.Code)
	}
}

func TestEndpoints_RequireAuth(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	h := srv.Handler()

	req := httptest.NewRequest("GET", "/api/endpoints", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 without session", w.Code)
	}
}

func TestSubscription_TokenGating(t *testing.T) {
	nodes := []*subscription.Node{
		{Name: "香港-01", Type: "ss", Server: "1.2.3.4", Port: 8388,
			Cipher: "aes-256-gcm", Password: "p", Available: true},
	}
	srv, st := newTestServer(t, nodes)
	h := srv.Handler()

	ep, err := st.CreateEndpointForUser(0, "测试设备")
	if err != nil {
		t.Fatalf("CreateEndpoint: %v", err)
	}

	// 正确 token → 200
	req := httptest.NewRequest("GET", "/sub/"+ep.Path+"?token="+ep.Token, nil)
	req.RemoteAddr = "1.2.3.4:5678"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("valid token status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	// 默认返回 Clash 格式，应包含 proxies 段与节点名
	if body := w.Body.String(); !bytes.Contains([]byte(body), []byte("proxies")) {
		t.Errorf("clash subscription missing 'proxies': %s", body)
	}

	// 错误 token → 404（不暴露端点存在）
	req2 := httptest.NewRequest("GET", "/sub/"+ep.Path+"?token=wrong", nil)
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req2)
	if w2.Code != http.StatusNotFound {
		t.Errorf("wrong token status = %d, want 404", w2.Code)
	}

	// 未知 path → 404
	req3 := httptest.NewRequest("GET", "/sub/nonexistent?token=x", nil)
	w3 := httptest.NewRecorder()
	h.ServeHTTP(w3, req3)
	if w3.Code != http.StatusNotFound {
		t.Errorf("unknown path status = %d, want 404", w3.Code)
	}
}

func TestSubscription_RecordsPull(t *testing.T) {
	nodes := []*subscription.Node{
		{Name: "香港-01", Type: "ss", Server: "1.2.3.4", Port: 8388,
			Cipher: "aes-256-gcm", Password: "p", Available: true},
	}
	srv, st := newTestServer(t, nodes)
	h := srv.Handler()

	ep, _ := st.CreateEndpoint("统计设备")
	req := httptest.NewRequest("GET", "/sub/"+ep.Path+"?token="+ep.Token, nil)
	req.RemoteAddr = "5.6.7.8:1234"
	h.ServeHTTP(httptest.NewRecorder(), req)

	stats, err := st.EndpointStats(ep.ID)
	if err != nil {
		t.Fatalf("EndpointStats: %v", err)
	}
	if len(stats) != 1 || stats[0].IP != "5.6.7.8" {
		t.Errorf("pull not recorded correctly: %+v", stats)
	}
}

// fixtureTOTPSecret is the shared secret markMFAEnrolled writes, so fixtures
// that have to clear the second login stage can produce a valid code for it
// (see doLoginEnrolled).
const fixtureTOTPSecret = "JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP"

// markMFAEnrolled 把账号直接置为"已绑定 MFA"态,让会话通过 requireMFAEnrolled。
// MFA 是全员强制的(ticket 05),未绑定会话进不了任何业务路由;绝大多数测试
// 关心的不是绑定流程,所以在夹具里跳过两段式 enroll,直接写库。
// 绑定/强制本身的行为由 mfa_test.go 与 middleware_test.go 走真实 HTTP 覆盖。
func markMFAEnrolled(t *testing.T, st *store.Store, userID int64) {
	t.Helper()
	secret := fixtureTOTPSecret
	enabled := true
	if err := st.UpdateUser(userID, store.UserUpdate{
		TOTPSecret:  &secret,
		TOTPEnabled: &enabled,
	}); err != nil {
		t.Fatalf("mark user %d mfa-enrolled: %v", userID, err)
	}
}

// doLoginEnrolled 登录并在需要时替夹具走完第二段 MFA(ticket 06)。
// 已绑定账号从陌生 IP 登录只拿到 mfa_pending token,夹具需要的是 session
// cookie,所以这里用 markMFAEnrolled 写入的固定密钥算一个 TOTP 码换正式会话。
// 返回第二段(或直通时第一段)的响应,形状与 doLogin 成功时一致。
// 只给"想要一个可用会话"的夹具用;判定分流本身由 auth_test.go/mfa_test.go
// 直接调 doLogin 覆盖。
func doLoginEnrolled(t *testing.T, h http.Handler, username, password, ip string) *httptest.ResponseRecorder {
	t.Helper()
	w := doLogin(t, h, username, password, ip)
	var stage1 struct {
		MFARequired  bool   `json:"mfa_required"`
		PendingToken string `json:"mfa_pending_token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &stage1); err != nil || !stage1.MFARequired {
		return w
	}

	code, err := totp.GenerateCode(fixtureTOTPSecret, time.Now())
	if err != nil {
		t.Fatalf("GenerateCode for fixture secret: %v", err)
	}
	body, _ := json.Marshal(map[string]any{
		"mfa_pending_token": stage1.PendingToken,
		"code":              code,
	})
	req := httptest.NewRequest("POST", "/api/login/mfa", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = ip + ":2000"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// markAllMFAEnrolled 把当前库里所有账号置为已绑定态。夹具常在 doSetup 之后
// 只拿到 cookie、拿不到 user id,这里按库遍历,免得每个 helper 各自查一遍。
func markAllMFAEnrolled(t *testing.T, st *store.Store) {
	t.Helper()
	users, err := st.ListUsers()
	if err != nil {
		t.Fatalf("list users for mfa fixture: %v", err)
	}
	for _, u := range users {
		markMFAEnrolled(t, st, u.ID)
	}
}

// authCookie 走一遍初始化 + 登录，返回可用于后台接口的 session cookie
func authCookie(t *testing.T, h http.Handler) *http.Cookie {
	t.Helper()
	doSetup(t, h, "owner", "a-very-strong-pass")
	markAllMFAEnrolled(t, testStore(t))
	w := doLoginEnrolled(t, h, "owner", "a-very-strong-pass", "9.9.9.9")
	for _, c := range w.Result().Cookies() {
		if c.Name == "session" {
			return c
		}
	}
	t.Fatalf("no session cookie from login (status %d)", w.Code)
	return nil
}

// keywordNodes 返回一组用于关键词过滤测试的节点:干净机场节点 / 命中黑名单机场节点 / 命中黑名单但豁免的自建节点
func keywordNodes() []*subscription.Node {
	return []*subscription.Node{
		{Name: "香港优选", Type: "ss", Server: "1.1.1.1", Port: 8388,
			Cipher: "aes-256-gcm", Password: "p", Available: true, Source: "机场A"},
		{Name: "剩余流量GB", Type: "ss", Server: "2.2.2.2", Port: 8388,
			Cipher: "aes-256-gcm", Password: "p", Available: true, Source: "机场A"},
		{Name: "自建官网", Type: "ss", Server: "3.3.3.3", Port: 8388,
			Cipher: "aes-256-gcm", Password: "p", Available: true, Source: subscription.SourceSelfHosted},
	}
}

func TestSubscription_AppliesKeywordFilter(t *testing.T) {
	srv, st := newTestServer(t, keywordNodes())
	h := srv.Handler()
	// Seed DB with self-hosted node (DB-authoritative after mergeSelfHosted)
	if err := st.CreateSelfHostedNode(&store.SelfHostedNode{
		Name:     "自建官网",
		Protocol: "ss",
		Server:   "3.3.3.3",
		Port:     8388,
		Cipher:   "aes-256-gcm",
		Password: "p",
		Enabled:  true,
	}); err != nil {
		t.Fatalf("CreateSelfHostedNode: %v", err)
	}
	if err := st.SaveSystemSettings(map[string]string{"filter_keywords": "剩余流量,官网"}); err != nil {
		t.Fatalf("SaveSystemSettings: %v", err)
	}
	ep, _ := st.CreateEndpoint("dev")

	req := httptest.NewRequest("GET", "/sub/"+ep.Path+"?token="+ep.Token+"&format=clash", nil)
	req.RemoteAddr = "1.2.3.4:5678"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}

	body := w.Body.String()
	if strings.Contains(body, "剩余流量") {
		t.Errorf("blacklisted airport node leaked into /sub:\n%s", body)
	}
	if !strings.Contains(body, "香港优选") {
		t.Errorf("clean node missing from /sub")
	}
	// 自建节点名命中 "官网" 但必须豁免
	if !strings.Contains(body, "自建官网") {
		t.Errorf("self-hosted node (matches '官网') must be exempt and present")
	}
}

func TestEndpointPreview_WYSIWYGAndNoPullRecorded(t *testing.T) {
	srv, st := newTestServer(t, keywordNodes())
	h := srv.Handler()
	// Seed DB with self-hosted node (DB-authoritative after mergeSelfHosted)
	if err := st.CreateSelfHostedNode(&store.SelfHostedNode{
		Name:     "自建官网",
		Protocol: "ss",
		Server:   "3.3.3.3",
		Port:     8388,
		Cipher:   "aes-256-gcm",
		Password: "p",
		Enabled:  true,
	}); err != nil {
		t.Fatalf("CreateSelfHostedNode: %v", err)
	}
	st.SaveSystemSettings(map[string]string{"filter_keywords": "剩余流量"})
	ep, _ := st.CreateEndpoint("dev")
	cookie := authCookie(t, h)

	req := httptest.NewRequest("GET", "/api/endpoints/"+strconv.FormatInt(ep.ID, 10)+"/preview?format=clash", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}

	var resp struct {
		Count   int    `json:"count"`
		Content string `json:"content"`
		Nodes   []struct {
			Name   string `json:"name"`
			Source string `json:"source"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal preview response: %v", err)
	}

	// 3 个节点剔除 1 个命中黑名单的机场节点 → 剩 2(含豁免的自建节点)
	if resp.Count != 2 {
		t.Errorf("count = %d, want 2", resp.Count)
	}
	for _, n := range resp.Nodes {
		if n.Name == "剩余流量GB" {
			t.Errorf("blacklisted node present in preview node list")
		}
	}
	// WYSIWYG:预览内容与 /sub 一致,应含干净节点
	if !strings.Contains(resp.Content, "香港优选") {
		t.Errorf("preview content is not WYSIWYG, missing clean node:\n%s", resp.Content)
	}

	// 关键:预览绝不记录拉取统计
	stats, err := st.EndpointStats(ep.ID)
	if err != nil {
		t.Fatalf("EndpointStats: %v", err)
	}
	if len(stats) != 0 {
		t.Errorf("preview must NOT record a pull, got %d stat rows", len(stats))
	}
}

func TestEndpointPreview_RequiresAuth(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	h := srv.Handler()

	req := httptest.NewRequest("GET", "/api/endpoints/1/preview", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 without session", w.Code)
	}
}

func TestEndpointPreview_UnknownEndpoint(t *testing.T) {
	srv, _ := newTestServer(t, keywordNodes())
	h := srv.Handler()
	cookie := authCookie(t, h)

	req := httptest.NewRequest("GET", "/api/endpoints/9999/preview", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 for unknown endpoint", w.Code)
	}
}

func TestSubscription_EmptyAfterKeywordFilterReturns503(t *testing.T) {
	// 只有机场节点且全部命中黑名单 → 过滤后为空,应返回 503 而非 500(生成空订阅)
	nodes := []*subscription.Node{
		{Name: "剩余流量A", Type: "ss", Server: "1.1.1.1", Port: 8388,
			Cipher: "aes-256-gcm", Password: "p", Available: true, Source: "机场A"},
		{Name: "剩余流量B", Type: "ss", Server: "2.2.2.2", Port: 8388,
			Cipher: "aes-256-gcm", Password: "p", Available: true, Source: "机场A"},
	}
	srv, st := newTestServer(t, nodes)
	h := srv.Handler()
	st.SaveSystemSettings(map[string]string{"filter_keywords": "剩余流量"})
	ep, _ := st.CreateEndpoint("dev")

	req := httptest.NewRequest("GET", "/sub/"+ep.Path+"?token="+ep.Token+"&format=clash", nil)
	req.RemoteAddr = "1.2.3.4:5678"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 when keyword filter empties the pool (body: %s)", w.Code, w.Body.String())
	}
}

// 池空但存在启用的自建节点时,/sub 不应在 serve-time 合并前提前 503。
// 全新装机只有自建节点的场景必须与订阅测试接口口径一致(ADR 0028 决策 1:所见即所得)。
// 修复前:handleSubscription 在 mergeSelfHosted 之前就对空池返回 503,订阅测试却报 valid。
func TestSubscription_OnlySelfHostedWhenPoolEmpty(t *testing.T) {
	// 聚合池完全为空(全新装机、尚未跑过刷新)
	srv, st := newTestServer(t, nil)
	h := srv.Handler()

	// 直接落库一条启用的自建节点(全零 UUID + example.com,不触真实凭证)
	if err := st.CreateSelfHostedNode(&store.SelfHostedNode{
		Name:     "自建-VLESS",
		Protocol: "vless",
		Server:   "selfhosted.example.com",
		Port:     443,
		UUID:     "00000000-0000-0000-0000-000000000000",
		Enabled:  true,
	}); err != nil {
		t.Fatalf("CreateSelfHostedNode: %v", err)
	}

	ep, err := st.CreateEndpointForUser(0, "dev")
	if err != nil {
		t.Fatalf("CreateEndpoint: %v", err)
	}

	req := httptest.NewRequest("GET", "/sub/"+ep.Path+"?token="+ep.Token+"&format=clash", nil)
	req.RemoteAddr = "1.2.3.4:5678"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 when only self-hosted nodes exist (body: %s)", w.Code, w.Body.String())
	}
	if body := w.Body.String(); !strings.Contains(body, "自建-VLESS") {
		t.Errorf("subscription should contain self-hosted node name, got: %s", body)
	}
}

// 池空且无任何自建节点时,仍应由过滤链末尾的二次 503 兜底(语义不变)。
func TestSubscription_EmptyPoolAndNoSelfHostedReturns503(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	ep, err := st.CreateEndpointForUser(0, "dev")
	if err != nil {
		t.Fatalf("CreateEndpoint: %v", err)
	}

	req := httptest.NewRequest("GET", "/sub/"+ep.Path+"?token="+ep.Token+"&format=clash", nil)
	req.RemoteAddr = "1.2.3.4:5678"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 when pool is empty and no self-hosted nodes (body: %s)", w.Code, w.Body.String())
	}
}
