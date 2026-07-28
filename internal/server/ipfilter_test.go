package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

// getFromIP sends a GET request with an explicit source address.
func getFromIP(t *testing.T, h http.Handler, path, remoteAddr string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	req.RemoteAddr = remoteAddr
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// sentinelHandler answers 204 and records the path it saw, so a test can tell
// "middleware passed the request through" from "middleware answered itself".
func sentinelHandler(seen *string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*seen = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})
}

// TestIPFilter_GlobalRuleDeniesEveryPath verifies a live scope=global rule 404s
// the source on every path: admin UI, login API, health check and a valid
// subscription address alike.
func TestIPFilter_GlobalRuleDeniesEveryPath(t *testing.T) {
	nodes := []*subscription.Node{
		{Name: "香港-01", Type: "ss", Server: "example.com", Port: 8388,
			Cipher: "aes-256-gcm", Password: "p", Available: true},
	}
	srv, st := newTestServer(t, nodes)
	ep, err := st.CreateEndpointForUser(1, "dev")
	if err != nil {
		t.Fatalf("CreateEndpointForUser: %v", err)
	}
	if _, err := st.AddIPAccessRule("203.0.113.7", store.IPRuleScopeGlobal,
		store.IPRuleSourceManual, "test", 0); err != nil {
		t.Fatalf("AddIPAccessRule: %v", err)
	}
	h := srv.ipFilterMiddleware(srv.Handler())

	for _, path := range []string{
		"/",
		"/login",
		"/api/status",
		"/api/login",
		"/healthz",
		"/sub/" + ep.Path + "?token=" + ep.Token,
	} {
		if w := getFromIP(t, h, path, "203.0.113.7:5000"); w.Code != http.StatusNotFound {
			t.Errorf("denied IP GET %s: status = %d, want 404", path, w.Code)
		}
	}
}

// TestIPFilter_CIDRRuleDenies verifies a CIDR rule covers every address in the
// block (the matching engine lives in the store; this pins the middleware to it).
func TestIPFilter_CIDRRuleDenies(t *testing.T) {
	srv, st := newTestServer(t, nil)
	if _, err := st.AddIPAccessRule("203.0.113.0/24", store.IPRuleScopeGlobal,
		store.IPRuleSourceManual, "block", 0); err != nil {
		t.Fatalf("AddIPAccessRule: %v", err)
	}
	var seen string
	h := srv.ipFilterMiddleware(sentinelHandler(&seen))

	if w := getFromIP(t, h, "/api/status", "203.0.113.200:5000"); w.Code != http.StatusNotFound {
		t.Errorf("in-block IP: status = %d, want 404", w.Code)
	}
	if w := getFromIP(t, h, "/api/status", "203.0.114.1:5000"); w.Code != http.StatusNoContent {
		t.Errorf("out-of-block IP: status = %d, want 204 (pass through)", w.Code)
	}
}

// TestIPFilter_DeniedResponseIndistinguishable verifies the deny 404 is byte
// identical to the 404 a non-existent path produces, headers included: a banned
// scanner must not be able to detect the ban.
func TestIPFilter_DeniedResponseIndistinguishable(t *testing.T) {
	srv, st := newTestServer(t, nil)
	if err := st.SetSitePath(testSitePath); err != nil {
		t.Fatalf("SetSitePath: %v", err)
	}
	if _, err := st.AddIPAccessRule("203.0.113.7", store.IPRuleScopeGlobal,
		store.IPRuleSourceManual, "test", 0); err != nil {
		t.Fatalf("AddIPAccessRule: %v", err)
	}
	h := srv.ipFilterMiddleware(srv.Handler())

	// Denied source asking for a path that exists under the boundary.
	denied := getFromIP(t, h, "/"+testSitePath+"/api/status", "203.0.113.7:5000")
	// Allowed source asking for a path that does not exist (Site Path miss).
	missing := getFromIP(t, h, "/nope/api/status", "203.0.113.8:5000")

	if denied.Code != missing.Code {
		t.Fatalf("status: denied = %d, missing = %d, want equal", denied.Code, missing.Code)
	}
	if denied.Body.String() != missing.Body.String() {
		t.Errorf("body: denied = %q, missing = %q, want equal",
			denied.Body.String(), missing.Body.String())
	}
	for _, key := range []string{"Content-Type", "X-Content-Type-Options", "Content-Length"} {
		if got, want := denied.Header().Get(key), missing.Header().Get(key); got != want {
			t.Errorf("header %s: denied = %q, missing = %q, want equal", key, got, want)
		}
	}
	if len(denied.Header()) != len(missing.Header()) {
		t.Errorf("header count: denied = %d, missing = %d, want equal",
			len(denied.Header()), len(missing.Header()))
	}
}

// TestIPFilter_NoRulePassesThrough verifies zero cost when nothing matches: the
// request reaches the downstream handler with its path untouched.
func TestIPFilter_NoRulePassesThrough(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	var seen string
	h := srv.ipFilterMiddleware(sentinelHandler(&seen))

	for _, path := range []string{"/", "/api/status", "/healthz"} {
		seen = ""
		if w := getFromIP(t, h, path, "198.51.100.5:5000"); w.Code != http.StatusNoContent {
			t.Errorf("GET %s: status = %d, want 204 (pass through)", path, w.Code)
			continue
		}
		if seen != path {
			t.Errorf("GET %s: downstream path = %q, want unchanged", path, seen)
		}
	}
}

// TestIPFilter_UnrelatedRuleScopePassesThrough verifies a scope=sub rule does not
// touch the site surface - the pull blacklist is not a site-wide ban.
func TestIPFilter_UnrelatedRuleScopePassesThrough(t *testing.T) {
	srv, st := newTestServer(t, nil)
	if _, err := st.AddIPAccessRule("203.0.113.7", store.IPRuleScopeSub,
		store.IPRuleSourceAuto, "pull abuse", 0); err != nil {
		t.Fatalf("AddIPAccessRule: %v", err)
	}
	var seen string
	h := srv.ipFilterMiddleware(sentinelHandler(&seen))

	if w := getFromIP(t, h, "/api/status", "203.0.113.7:5000"); w.Code != http.StatusNoContent {
		t.Errorf("sub-scoped rule: status = %d, want 204 (global surface unaffected)", w.Code)
	}
}

// TestIPFilter_LoopbackAlwaysAllowed verifies the escape hatch: even a rule that
// names loopback (or a block containing it) cannot lock the operator out via the
// SSH tunnel / localhost path.
func TestIPFilter_LoopbackAlwaysAllowed(t *testing.T) {
	srv, st := newTestServer(t, nil)
	for _, target := range []string{"127.0.0.1", "127.0.0.0/8", "::1"} {
		if _, err := st.AddIPAccessRule(target, store.IPRuleScopeGlobal,
			store.IPRuleSourceManual, "bad rule", 0); err != nil {
			t.Fatalf("AddIPAccessRule(%s): %v", target, err)
		}
	}
	var seen string
	h := srv.ipFilterMiddleware(sentinelHandler(&seen))

	for _, addr := range []string{"127.0.0.1:5000", "[::1]:5000"} {
		if w := getFromIP(t, h, "/api/status", addr); w.Code != http.StatusNoContent {
			t.Errorf("loopback %s: status = %d, want 204 (never denied)", addr, w.Code)
		}
	}
}

// TestIPFilter_DeleteRestoresAccess verifies access comes back the moment the
// rule is removed (no restart, no cache flush call at the middleware level).
func TestIPFilter_DeleteRestoresAccess(t *testing.T) {
	srv, st := newTestServer(t, nil)
	rule, err := st.AddIPAccessRule("203.0.113.7", store.IPRuleScopeGlobal,
		store.IPRuleSourceManual, "test", 0)
	if err != nil {
		t.Fatalf("AddIPAccessRule: %v", err)
	}
	var seen string
	h := srv.ipFilterMiddleware(sentinelHandler(&seen))

	if w := getFromIP(t, h, "/api/status", "203.0.113.7:5000"); w.Code != http.StatusNotFound {
		t.Fatalf("before delete: status = %d, want 404", w.Code)
	}
	if err := st.DeleteIPAccessRule(rule.ID); err != nil {
		t.Fatalf("DeleteIPAccessRule: %v", err)
	}
	if w := getFromIP(t, h, "/api/status", "203.0.113.7:5000"); w.Code != http.StatusNoContent {
		t.Errorf("after delete: status = %d, want 204 (access restored)", w.Code)
	}
}

// TestIPFilter_ExpiredRuleRestoresAccess verifies a lapsed rule stops matching
// without any cleanup pass.
//
// The TTL has to exceed a second: expires_at is persisted at second resolution,
// so a millisecond TTL truncates down to "already expired" and the live-ban half
// of the assertion could never hold.
func TestIPFilter_ExpiredRuleRestoresAccess(t *testing.T) {
	srv, st := newTestServer(t, nil)
	if _, err := st.AddIPAccessRule("203.0.113.7", store.IPRuleScopeGlobal,
		store.IPRuleSourceAuto, "short ban", 1200*time.Millisecond); err != nil {
		t.Fatalf("AddIPAccessRule: %v", err)
	}
	var seen string
	h := srv.ipFilterMiddleware(sentinelHandler(&seen))

	if w := getFromIP(t, h, "/api/status", "203.0.113.7:5000"); w.Code != http.StatusNotFound {
		t.Fatalf("while ban is live: status = %d, want 404", w.Code)
	}

	// Poll rather than sleep a fixed span: the truncated expiry lands somewhere
	// inside the next second, and polling keeps the test both fast and stable.
	deadline := time.Now().Add(4 * time.Second)
	for {
		w := getFromIP(t, h, "/api/status", "203.0.113.7:5000")
		if w.Code == http.StatusNoContent {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("after expiry: status = %d, want 204 (access restored)", w.Code)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestIPFilter_ProductionOrderWithSitePath pins the intended composition -
// sitePathMiddleware outside, ipFilterMiddleware inside, router last - so the
// deny check sees the rewritten path and every combination behaves:
// prefix hit + allowed passes through, prefix hit + denied 404s, prefix miss
// 404s regardless of source.
func TestIPFilter_ProductionOrderWithSitePath(t *testing.T) {
	srv, st := newTestServer(t, nil)
	if err := st.SetSitePath(testSitePath); err != nil {
		t.Fatalf("SetSitePath: %v", err)
	}
	if _, err := st.AddIPAccessRule("203.0.113.7", store.IPRuleScopeGlobal,
		store.IPRuleSourceManual, "test", 0); err != nil {
		t.Fatalf("AddIPAccessRule: %v", err)
	}
	var seen string
	h := srv.sitePathMiddleware(srv.ipFilterMiddleware(sentinelHandler(&seen)))

	cases := []struct {
		name     string
		path     string
		addr     string
		wantCode int
		wantPath string // checked only when the request is expected to pass through
	}{
		{"allowed under prefix", "/" + testSitePath + "/api/status", "198.51.100.5:5000",
			http.StatusNoContent, "/api/status"},
		{"denied under prefix", "/" + testSitePath + "/api/status", "203.0.113.7:5000",
			http.StatusNotFound, ""},
		{"denied at prefix root", "/" + testSitePath + "/", "203.0.113.7:5000",
			http.StatusNotFound, ""},
		{"allowed outside prefix", "/api/status", "198.51.100.5:5000",
			http.StatusNotFound, ""},
		{"denied outside prefix", "/api/status", "203.0.113.7:5000",
			http.StatusNotFound, ""},
	}
	for _, c := range cases {
		seen = ""
		w := getFromIP(t, h, c.path, c.addr)
		if w.Code != c.wantCode {
			t.Errorf("%s: status = %d, want %d", c.name, w.Code, c.wantCode)
			continue
		}
		if c.wantCode == http.StatusNoContent && seen != c.wantPath {
			t.Errorf("%s: downstream path = %q, want %q", c.name, seen, c.wantPath)
		}
	}
}

// TestIPFilter_ForwardedForHonoredBehindLoopbackProxy verifies the middleware
// bans the real client behind a trusted reverse proxy: with a loopback peer the
// X-Forwarded-For address is what clientIP reports, so that is what gets matched.
func TestIPFilter_ForwardedForHonoredBehindLoopbackProxy(t *testing.T) {
	srv, st := newTestServer(t, nil)
	if _, err := st.AddIPAccessRule("203.0.113.7", store.IPRuleScopeGlobal,
		store.IPRuleSourceManual, "test", 0); err != nil {
		t.Fatalf("AddIPAccessRule: %v", err)
	}
	var seen string
	h := srv.ipFilterMiddleware(sentinelHandler(&seen))

	req := httptest.NewRequest("GET", "/api/status", nil)
	req.RemoteAddr = "127.0.0.1:5000"
	req.Header.Set("X-Forwarded-For", "203.0.113.7")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("forwarded denied client: status = %d, want 404", w.Code)
	}
}
