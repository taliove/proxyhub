package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/taliove/proxyhub/internal/store"
)

// End-to-end pull guard coverage (pull-guard ticket 11).
//
// The individual guard tests (subguard_test.go, geoguard_pull_test.go,
// ratelimit_test.go, blacklist_test.go) each pin one mechanism. What none of
// them exercise is the whole path a real subscription pull walks: invalid
// token uniform 404, guard chain ordering, rate limit exhaustion leading to
// auto-escalation, manual rules overriding, geo three-mode behavior, disabled
// status logging, and global scope locking the entire site. These tests drive
// only HTTP, so a regression in guard ordering or status recording shows up
// even when every unit test passes.

// TestPullGuardE2E_InvalidTokenUniform404 is the iron rule: an unauthenticated
// request gets the stock 404 regardless of what rules are live, so guard
// responses (429/403) never leak to a caller without a valid token.
func TestPullGuardE2E_InvalidTokenUniform404(t *testing.T) {
	srv, st := newTestServer(t, pullLogNodes())
	h := srv.Handler()

	ep, _ := st.CreateEndpoint("无效token设备")

	// Install every guard type that could produce a non-404.
	if err := st.SetSetting("pull_rate_limit_per_hour", "1"); err != nil {
		t.Fatalf("SetSetting rate limit: %v", err)
	}
	if _, err := st.AddIPAccessRule("203.0.113.10", store.IPRuleScopeSub, store.IPRuleSourceManual, "test sub rule", 0); err != nil {
		t.Fatalf("AddIPAccessRule sub: %v", err)
	}
	if _, err := st.AddIPAccessRule("203.0.113.11", store.IPRuleScopeGlobal, store.IPRuleSourceManual, "test global rule", 0); err != nil {
		t.Fatalf("AddIPAccessRule global: %v", err)
	}
	if err := st.UpdateEndpointGeoConfig(ep.ID, store.GeoModeEnforce, "ZZ", ""); err != nil {
		t.Fatalf("UpdateEndpointGeoConfig: %v", err)
	}

	cases := []struct {
		name string
		url  string
		ip   string
	}{
		{"unknown path", "/sub/nonexistent?token=x", "203.0.113.10"},
		{"wrong token", "/sub/" + ep.Path + "?token=wrong", "203.0.113.10"},
		{"missing token", "/sub/" + ep.Path, "203.0.113.10"},
		{"disabled endpoint with valid token", "/sub/" + ep.Path + "?token=" + ep.Token, "203.0.113.10"},
	}

	// Disable the endpoint for the last case.
	if err := st.SetEndpointEnabled(ep.ID, false); err != nil {
		t.Fatalf("SetEndpointEnabled: %v", err)
	}

	for _, c := range cases {
		req := httptest.NewRequest("GET", c.url, nil)
		req.RemoteAddr = c.ip + ":12345"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404 (body: %s)", c.name, w.Code, w.Body.String())
		}
		if body := w.Body.String(); body != "404 page not found\n" {
			t.Errorf("%s: body = %q, want stock 404 body", c.name, body)
		}
	}

	// None of those attempts may leak a guard status.
	stats, _ := st.EndpointStats(ep.ID)
	for _, s := range stats {
		if s.Status == store.PullStatusRateLimited || s.Status == store.PullStatusBlacklisted || s.Status == store.PullStatusGeoBlocked {
			t.Errorf("unauthenticated pull recorded guard status %s: %+v", s.Status, s)
		}
	}
}

// TestPullGuardE2E_NormalPullLogsOK a valid pull with no guards blocking
// records exactly one status=ok row.
func TestPullGuardE2E_NormalPullLogsOK(t *testing.T) {
	srv, st := newTestServer(t, pullLogNodes())
	h := srv.Handler()

	ep, _ := st.CreateEndpoint("正常设备")

	req := httptest.NewRequest("GET", "/sub/"+ep.Path+"?token="+ep.Token, nil)
	req.RemoteAddr = "203.0.113.20:9999"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}

	got := pullStatusesFor(t, st, ep.ID)
	if got["203.0.113.20"][store.PullStatusOK] != 1 {
		t.Errorf("statuses = %+v, want exactly one ok row", got)
	}
	// No guard traces.
	for _, count := range got["203.0.113.20"] {
		if count > 1 {
			t.Errorf("multiple status rows for one pull: %+v", got)
		}
	}
}

// TestPullGuardE2E_RateLimitTriggersAnd429 the rate limit guard produces 429
// with Retry-After and records rate_limited status when the threshold is
// exceeded.
func TestPullGuardE2E_RateLimitTriggersAnd429(t *testing.T) {
	srv, st := newTestServer(t, pullLogNodes())
	h := srv.Handler()

	// Lower threshold to 3 to speed up the test.
	if err := st.SetSetting("pull_rate_limit_per_hour", "3"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	// Invalidate the cache so the new threshold takes effect immediately.
	srv.pullRateThreshold.invalidate()

	ep, _ := st.CreateEndpoint("限频设备")
	const ip = "203.0.113.30"

	// First 3 pulls succeed.
	for i := 0; i < 3; i++ {
		w := pullFrom(t, h, ep, ip)
		if w.Code != http.StatusOK {
			t.Fatalf("pull %d: status = %d, want 200 (body: %s)", i+1, w.Code, w.Body.String())
		}
	}

	// 4th pull is rate-limited.
	w := pullFrom(t, h, ep, ip)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("4th pull: status = %d, want 429 (body: %s)", w.Code, w.Body.String())
	}
	if w.Header().Get("Retry-After") == "" {
		t.Error("429 response missing Retry-After header")
	}

	got := pullStatusesFor(t, st, ep.ID)
	if got[ip][store.PullStatusOK] != 3 {
		t.Errorf("ok count = %d, want 3", got[ip][store.PullStatusOK])
	}
	if got[ip][store.PullStatusRateLimited] != 1 {
		t.Errorf("rate_limited count = %d, want 1", got[ip][store.PullStatusRateLimited])
	}
}

// TestPullGuardE2E_AutoBlacklistEscalation repeated rate-limit hits trigger
// the escalation chain, which writes a scope=sub source=auto rule, and
// subsequent pulls from that source get 403 + blacklisted status.
func TestPullGuardE2E_AutoBlacklistEscalation(t *testing.T) {
	srv, st := newTestServer(t, pullLogNodes())
	h := srv.Handler()

	// Lower both thresholds: rate limit to 2, escalation to 3 rejections.
	if err := st.SetSetting("pull_rate_limit_per_hour", "2"); err != nil {
		t.Fatalf("SetSetting rate limit: %v", err)
	}
	if err := st.SetSetting("pull_blacklist_escalation_count", "3"); err != nil {
		t.Fatalf("SetSetting escalation count: %v", err)
	}
	srv.pullRateThreshold.invalidate()
	srv.pullEscalation().invalidate()

	ep, _ := st.CreateEndpoint("升级设备")
	const ip = "203.0.113.40"

	// First 2 pulls succeed (under rate limit).
	for i := 0; i < 2; i++ {
		if w := pullFrom(t, h, ep, ip); w.Code != http.StatusOK {
			t.Fatalf("pull %d: status = %d, want 200", i+1, w.Code)
		}
	}

	// Next 3 pulls are rate-limited. The 3rd rejection triggers escalation.
	for i := 0; i < 3; i++ {
		w := pullFrom(t, h, ep, ip)
		if w.Code != http.StatusTooManyRequests {
			t.Fatalf("rate-limited pull %d: status = %d, want 429 (body: %s)", i+1, w.Code, w.Body.String())
		}
	}

	// Verify the auto rule was written.
	rules, err := st.ListIPAccessRules(store.IPRuleScopeSub)
	if err != nil {
		t.Fatalf("ListIPAccessRules: %v", err)
	}
	found := false
	for _, r := range rules {
		if r.IPOrCIDR == ip && r.Source == store.IPRuleSourceAuto && !r.Expired() {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("no auto rule found for %s after escalation: %+v", ip, rules)
	}

	// Next pull from the same IP gets 403 blacklisted.
	w := pullFrom(t, h, ep, ip)
	if w.Code != http.StatusForbidden {
		t.Fatalf("post-escalation pull: status = %d, want 403 (body: %s)", w.Code, w.Body.String())
	}

	got := pullStatusesFor(t, st, ep.ID)
	if got[ip][store.PullStatusBlacklisted] != 1 {
		t.Errorf("blacklisted count = %d, want 1 (statuses: %+v)", got[ip][store.PullStatusBlacklisted], got[ip])
	}
}

// TestPullGuardE2E_ManualSubRuleBlocks a manually written scope=sub rule
// produces 403 immediately, and removing the rule restores access.
func TestPullGuardE2E_ManualSubRuleBlocks(t *testing.T) {
	srv, st := newTestServer(t, pullLogNodes())
	h := srv.Handler()

	ep, _ := st.CreateEndpoint("手动规则设备")
	const ip = "203.0.113.50"

	// Without a rule, pull succeeds.
	if w := pullFrom(t, h, ep, ip); w.Code != http.StatusOK {
		t.Fatalf("initial pull: status = %d, want 200", w.Code)
	}

	// Add a manual sub rule.
	rule, err := st.AddIPAccessRule(ip, store.IPRuleScopeSub, store.IPRuleSourceManual, "manual test ban", 0)
	if err != nil {
		t.Fatalf("AddIPAccessRule: %v", err)
	}

	// Pull is now blocked.
	w := pullFrom(t, h, ep, ip)
	if w.Code != http.StatusForbidden {
		t.Fatalf("blocked pull: status = %d, want 403 (body: %s)", w.Code, w.Body.String())
	}

	got := pullStatusesFor(t, st, ep.ID)
	if got[ip][store.PullStatusBlacklisted] != 1 {
		t.Errorf("blacklisted count = %d, want 1", got[ip][store.PullStatusBlacklisted])
	}

	// Remove the rule.
	if err := st.DeleteIPAccessRule(rule.ID); err != nil {
		t.Fatalf("DeleteIPAccessRule: %v", err)
	}

	// Pull succeeds again.
	if w := pullFrom(t, h, ep, ip); w.Code != http.StatusOK {
		t.Fatalf("post-delete pull: status = %d, want 200", w.Code)
	}
}

// TestPullGuardE2E_GlobalRuleLocksSite a scope=global rule produces 404 for
// subscription pulls AND for login, and loopback is exempt.
func TestPullGuardE2E_GlobalRuleLocksSite(t *testing.T) {
	srv, st := newTestServer(t, pullLogNodes())
	h := srv.Handler()
	doSetup(t, h, "owner", "pass-1234-5678")

	ep, _ := st.CreateEndpoint("全局规则设备")
	const ip = "203.0.113.60"

	// Add a global rule.
	if _, err := st.AddIPAccessRule(ip, store.IPRuleScopeGlobal, store.IPRuleSourceManual, "global test ban", 0); err != nil {
		t.Fatalf("AddIPAccessRule: %v", err)
	}

	// Subscription pull with valid token gets 404 (not 403: global rules hide).
	req := httptest.NewRequest("GET", "/sub/"+ep.Path+"?token="+ep.Token, nil)
	req.RemoteAddr = ip + ":11111"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("sub pull from global-banned IP: status = %d, want 404 (body: %s)", w.Code, w.Body.String())
	}

	// Login from the same IP also gets 404.
	loginReq := httptest.NewRequest("POST", "/api/login", nil)
	loginReq.RemoteAddr = ip + ":11111"
	loginW := httptest.NewRecorder()
	h.ServeHTTP(loginW, loginReq)
	if loginW.Code != http.StatusNotFound {
		t.Errorf("login from global-banned IP: status = %d, want 404 (body: %s)", loginW.Code, loginW.Body.String())
	}

	// Loopback is exempt: subscription pull succeeds.
	loopReq := httptest.NewRequest("GET", "/sub/"+ep.Path+"?token="+ep.Token, nil)
	loopReq.RemoteAddr = "127.0.0.1:22222"
	loopW := httptest.NewRecorder()
	h.ServeHTTP(loopW, loopReq)
	if loopW.Code != http.StatusOK {
		t.Errorf("loopback pull: status = %d, want 200 (body: %s)", loopW.Code, loopW.Body.String())
	}
}

// TestPullGuardE2E_GeoThreeModes pins the geo guard's three behaviors: off is
// silent, observe records geo_would_block without blocking, enforce records
// geo_blocked and answers 403.
func TestPullGuardE2E_GeoThreeModes(t *testing.T) {
	srv, st := newGeoTestServer(t)
	h := srv.Handler()

	epOff, _ := st.CreateEndpoint("地域关闭")
	epObserve, _ := st.CreateEndpoint("地域观察")
	epEnforce, _ := st.CreateEndpoint("地域强制")

	// Configure geo: CN-only, three modes.
	if err := st.UpdateEndpointGeoConfig(epOff.ID, store.GeoModeOff, "CN", ""); err != nil {
		t.Fatalf("UpdateEndpointGeoConfig off: %v", err)
	}
	if err := st.UpdateEndpointGeoConfig(epObserve.ID, store.GeoModeObserve, "CN", ""); err != nil {
		t.Fatalf("UpdateEndpointGeoConfig observe: %v", err)
	}
	if err := st.UpdateEndpointGeoConfig(epEnforce.ID, store.GeoModeEnforce, "CN", ""); err != nil {
		t.Fatalf("UpdateEndpointGeoConfig enforce: %v", err)
	}

	const cnIP = "1.2.4.8"   // CN from geoTable
	const usIP = "8.8.8.8"   // US from geoTable

	// Off mode: both IPs succeed, no geo trace.
	for _, ip := range []string{cnIP, usIP} {
		if w := pullFrom(t, h, epOff, ip); w.Code != http.StatusOK {
			t.Errorf("off mode ip %s: status = %d, want 200", ip, w.Code)
		}
	}
	gotOff := pullStatusesFor(t, st, epOff.ID)
	if gotOff[usIP][store.PullStatusGeoWouldBlock] != 0 {
		t.Errorf("off mode left geo trace: %+v", gotOff)
	}

	// Observe mode: both IPs succeed, US gets geo_would_block trace.
	for _, ip := range []string{cnIP, usIP} {
		if w := pullFrom(t, h, epObserve, ip); w.Code != http.StatusOK {
			t.Errorf("observe mode ip %s: status = %d, want 200", ip, w.Code)
		}
	}
	gotObserve := pullStatusesFor(t, st, epObserve.ID)
	if gotObserve[usIP][store.PullStatusGeoWouldBlock] != 1 {
		t.Errorf("observe mode US: want geo_would_block=1, got %+v", gotObserve[usIP])
	}
	if gotObserve[usIP][store.PullStatusOK] != 1 {
		t.Errorf("observe mode must still deliver: got %+v", gotObserve[usIP])
	}
	if gotObserve[cnIP][store.PullStatusGeoWouldBlock] != 0 {
		t.Errorf("observe mode CN should not leave would_block: %+v", gotObserve[cnIP])
	}

	// Enforce mode: CN succeeds, US gets 403 + geo_blocked.
	if w := pullFrom(t, h, epEnforce, cnIP); w.Code != http.StatusOK {
		t.Errorf("enforce CN: status = %d, want 200", w.Code)
	}
	w := pullFrom(t, h, epEnforce, usIP)
	if w.Code != http.StatusForbidden {
		t.Errorf("enforce US: status = %d, want 403 (body: %s)", w.Code, w.Body.String())
	}
	gotEnforce := pullStatusesFor(t, st, epEnforce.ID)
	if gotEnforce[usIP][store.PullStatusGeoBlocked] != 1 {
		t.Errorf("enforce US: want geo_blocked=1, got %+v", gotEnforce[usIP])
	}
	if gotEnforce[usIP][store.PullStatusOK] != 0 {
		t.Errorf("blocked pull must not count as ok: %+v", gotEnforce[usIP])
	}
}

// TestPullGuardE2E_DisabledEndpointLogsDisabled a disabled subscription
// address records status=disabled, not bad_token.
func TestPullGuardE2E_DisabledEndpointLogsDisabled(t *testing.T) {
	srv, st := newTestServer(t, pullLogNodes())
	h := srv.Handler()

	ep, _ := st.CreateEndpoint("禁用端点")
	if err := st.SetEndpointEnabled(ep.ID, false); err != nil {
		t.Fatalf("SetEndpointEnabled: %v", err)
	}

	req := httptest.NewRequest("GET", "/sub/"+ep.Path+"?token="+ep.Token, nil)
	req.RemoteAddr = "203.0.113.70:33333"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}

	got := pullStatusesFor(t, st, ep.ID)
	if got["203.0.113.70"][store.PullStatusDisabled] != 1 {
		t.Errorf("disabled count = %d, want 1 (statuses: %+v)", got["203.0.113.70"][store.PullStatusDisabled], got)
	}
	if got["203.0.113.70"][store.PullStatusBadToken] != 0 {
		t.Errorf("disabled must not record bad_token: %+v", got)
	}
}

// TestPullGuardE2E_AggregateStatsExcludeBlocked the aggregator only counts
// status=ok rows, so blocked pulls do not inflate subscription statistics.
func TestPullGuardE2E_AggregateStatsExcludeBlocked(t *testing.T) {
	srv, st := newTestServer(t, pullLogNodes())
	h := srv.Handler()

	if err := st.SetSetting("pull_rate_limit_per_hour", "1"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	srv.pullRateThreshold.invalidate()

	ep, _ := st.CreateEndpoint("统计设备")
	const ip = "203.0.113.80"

	// One successful pull.
	if w := pullFrom(t, h, ep, ip); w.Code != http.StatusOK {
		t.Fatalf("ok pull: status = %d, want 200", w.Code)
	}

	// One rate-limited pull.
	if w := pullFrom(t, h, ep, ip); w.Code != http.StatusTooManyRequests {
		t.Fatalf("rate-limited pull: status = %d, want 429", w.Code)
	}

	// Aggregate stats should only count the ok pull.
	stats, err := st.EndpointStats(ep.ID)
	if err != nil {
		t.Fatalf("EndpointStats: %v", err)
	}

	totalOK := 0
	totalBlocked := 0
	for _, s := range stats {
		if s.Status == store.PullStatusOK {
			totalOK += s.Count
		} else {
			totalBlocked += s.Count
		}
	}

	if totalOK != 1 {
		t.Errorf("ok pulls = %d, want 1", totalOK)
	}
	if totalBlocked != 1 {
		t.Errorf("blocked pulls = %d, want 1", totalBlocked)
	}
}
