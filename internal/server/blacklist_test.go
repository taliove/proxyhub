package server

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/taliove/proxyhub/internal/store"
)

// Pull blacklist guard + auto escalation chain (pull-guard ticket 05).

// subRuleFor reads the scope=sub rule for ip, or nil when there is none.
func subRuleFor(t *testing.T, st *store.Store, ip string) *store.IPAccessRule {
	t.Helper()
	rule, err := st.GetIPAccessRuleByTarget(ip, store.IPRuleScopeSub)
	if err != nil {
		return nil
	}
	return rule
}

// setSettings saves settings and drops the guard caches so the next pull sees
// them (the HTTP settings handler does the same).
func setSettings(t *testing.T, srv *Server, st *store.Store, kv map[string]string) {
	t.Helper()
	if err := st.SaveSystemSettings(kv); err != nil {
		t.Fatalf("SaveSystemSettings(%v): %v", kv, err)
	}
	srv.pullRateThreshold.invalidate()
	srv.pullEscalation().invalidate()
}

// TestBlacklist_ManualSubRuleBlocks a manual scope=sub rule answers 403 with a
// blacklisted trace; an unlisted IP is unaffected.
func TestBlacklist_ManualSubRuleBlocks(t *testing.T) {
	srv, st := newTestServer(t, pullLogNodes())
	h := srv.Handler()
	ep, _ := st.CreateEndpoint("黑名单设备")

	if _, err := st.AddIPAccessRule("203.0.113.20", store.IPRuleScopeSub,
		store.IPRuleSourceManual, "手工封禁", 0); err != nil {
		t.Fatalf("AddIPAccessRule: %v", err)
	}

	w := pullOnce(t, h, ep, "203.0.113.20")
	if w.Code != http.StatusForbidden {
		t.Fatalf("blacklisted pull: status = %d, want 403 (body %s)", w.Code, w.Body.String())
	}
	if w := pullOnce(t, h, ep, "203.0.113.21"); w.Code != http.StatusOK {
		t.Errorf("unlisted pull: status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}

	got := pullStatusesFor(t, st, ep.ID)
	if got["203.0.113.20"][store.PullStatusBlacklisted] != 1 {
		t.Errorf("blacklisted rows = %d, want 1 (all: %+v)",
			got["203.0.113.20"][store.PullStatusBlacklisted], got)
	}
	if got["203.0.113.21"][store.PullStatusOK] != 1 {
		t.Errorf("ok rows for unlisted ip = %d, want 1 (all: %+v)",
			got["203.0.113.21"][store.PullStatusOK], got)
	}
}

// TestBlacklist_GlobalRuleIsNotASubBan scope isolation: a global rule is
// enforced by the site middleware, not by this guard, so the guard itself must
// not treat it as a pull ban (otherwise the two layers would double-count and a
// scope change would silently alter pull behaviour).
func TestBlacklist_GlobalRuleIsNotASubBan(t *testing.T) {
	srv, st := newTestServer(t, pullLogNodes())
	ep, _ := st.CreateEndpoint("全局规则设备")
	if _, err := st.AddIPAccessRule("203.0.113.22", store.IPRuleScopeGlobal,
		store.IPRuleSourceManual, "整站封禁", 0); err != nil {
		t.Fatalf("AddIPAccessRule: %v", err)
	}

	guard := newPullBlacklistGuard(st, srv.logger, srv.pullEscalation())
	verdict := guard.check(subGuardRequest{ep: ep, ip: "203.0.113.22"})
	if verdict.block {
		t.Error("guard blocked on a global rule; scope=sub only")
	}
}

// TestBlacklist_EscalatesOnNthRateLimit the escalation chain writes an auto
// scope=sub rule once the IP has produced escalationCount rate_limited rows,
// and from then on the client gets 403 instead of 429.
func TestBlacklist_EscalatesOnNthRateLimit(t *testing.T) {
	srv, st := newTestServer(t, pullLogNodes())
	// Rate limit 1/hour so every pull after the first is rejected; escalate on
	// the default 10th rejection.
	setSettings(t, srv, st, map[string]string{"pull_rate_limit_per_hour": "1"})
	h := srv.Handler()
	ep, _ := st.CreateEndpoint("升级链设备")
	const ip = "203.0.113.30"

	if w := pullOnce(t, h, ep, ip); w.Code != http.StatusOK {
		t.Fatalf("pull 1: status = %d, want 200", w.Code)
	}

	// Rejections 1..9 stay 429 and must not create a rule yet.
	for i := 1; i < defaultPullBlacklistEscalationCount; i++ {
		w := pullOnce(t, h, ep, ip)
		if w.Code != http.StatusTooManyRequests {
			t.Fatalf("rejection %d: status = %d, want 429 (body %s)", i, w.Code, w.Body.String())
		}
		if rule := subRuleFor(t, st, ip); rule != nil {
			t.Fatalf("rejection %d already escalated (rule %+v), want escalation on %d",
				i, rule, defaultPullBlacklistEscalationCount)
		}
	}

	// The Nth rejection is still answered by the rate limiter (429) and trips
	// the escalation.
	if w := pullOnce(t, h, ep, ip); w.Code != http.StatusTooManyRequests {
		t.Fatalf("rejection %d: status = %d, want 429",
			defaultPullBlacklistEscalationCount, w.Code)
	}
	rule := subRuleFor(t, st, ip)
	if rule == nil {
		t.Fatalf("no scope=sub rule after %d rejections", defaultPullBlacklistEscalationCount)
	}
	if rule.Source != store.IPRuleSourceAuto {
		t.Errorf("rule source = %q, want %q", rule.Source, store.IPRuleSourceAuto)
	}
	if rule.Permanent() {
		t.Error("auto rule is permanent, want a bounded window")
	}
	if rule.Comment == "" {
		t.Error("auto rule has no comment; the operator cannot tell where it came from")
	}

	// From here the blacklist guard answers first: 403, not 429.
	w := pullOnce(t, h, ep, ip)
	if w.Code != http.StatusForbidden {
		t.Fatalf("pull after escalation: status = %d, want 403 (body %s)", w.Code, w.Body.String())
	}
	got := pullStatusesFor(t, st, ep.ID)
	if got[ip][store.PullStatusBlacklisted] != 1 {
		t.Errorf("blacklisted rows = %d, want 1 (all: %+v)",
			got[ip][store.PullStatusBlacklisted], got)
	}
}

// TestBlacklist_EscalationIsolatedPerIP one escalated source must not affect
// another: the count is per IP, so a second address keeps its own quota and is
// still served.
func TestBlacklist_EscalationIsolatedPerIP(t *testing.T) {
	srv, st := newTestServer(t, pullLogNodes())
	setSettings(t, srv, st, map[string]string{
		"pull_rate_limit_per_hour":        "1",
		"pull_blacklist_escalation_count": "2",
	})
	h := srv.Handler()
	ep, _ := st.CreateEndpoint("隔离设备")

	// Abuser: one served pull then two rejections -> escalated.
	pullOnce(t, h, ep, "203.0.113.40")
	pullOnce(t, h, ep, "203.0.113.40")
	pullOnce(t, h, ep, "203.0.113.40")
	if subRuleFor(t, st, "203.0.113.40") == nil {
		t.Fatal("abuser was not escalated after 2 rejections")
	}
	if w := pullOnce(t, h, ep, "203.0.113.40"); w.Code != http.StatusForbidden {
		t.Fatalf("abuser pull: status = %d, want 403", w.Code)
	}

	// Neighbour: untouched rule set, untouched quota.
	if rule := subRuleFor(t, st, "203.0.113.41"); rule != nil {
		t.Errorf("neighbour got a rule %+v, want none", rule)
	}
	if w := pullOnce(t, h, ep, "203.0.113.41"); w.Code != http.StatusOK {
		t.Errorf("neighbour pull: status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
}

// TestBlacklist_AutoRuleExpiryRestoresAccess the auto rule carries the
// configured duration and simply stops matching when it lapses - no cleanup job
// involved. The second address proves it: the abuser's rate-limit window for
// the first address is still full, but a fresh address is served again once the
// rule has expired.
func TestBlacklist_AutoRuleExpiryRestoresAccess(t *testing.T) {
	srv, st := newTestServer(t, pullLogNodes())
	setSettings(t, srv, st, map[string]string{
		"pull_rate_limit_per_hour":        "1",
		"pull_blacklist_escalation_count": "1",
		// 2s, not a sub-second TTL: ip_access_rules stores expires_at with second
		// granularity, so anything under a second truncates into the current
		// second and the rule would be born already expired.
		"pull_blacklist_duration": "2s",
	})
	h := srv.Handler()
	first, _ := st.CreateEndpoint("到期设备甲")
	second, _ := st.CreateEndpoint("到期设备乙")
	const ip = "203.0.113.50"

	pullOnce(t, h, first, ip) // served, fills the 1/hour window
	if w := pullOnce(t, h, first, ip); w.Code != http.StatusTooManyRequests {
		t.Fatalf("rejection: status = %d, want 429", w.Code)
	}
	rule := subRuleFor(t, st, ip)
	if rule == nil {
		t.Fatal("no auto rule after the configured single rejection")
	}
	if rule.ExpiresAt == nil {
		t.Fatal("auto rule is permanent, want the configured 2s window")
	}

	// While the rule holds, even an untouched address is refused.
	if w := pullOnce(t, h, second, ip); w.Code != http.StatusForbidden {
		t.Fatalf("pull of a fresh address while blacklisted: status = %d, want 403", w.Code)
	}

	// Wait past the stored expiry (2s TTL truncated to the second, so up to 2s
	// of real time) with a margin, then the lapsed rule simply stops matching.
	time.Sleep(2100 * time.Millisecond)
	if w := pullOnce(t, h, second, ip); w.Code != http.StatusOK {
		t.Errorf("pull after expiry: status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
}

// TestBlacklist_LoopbackExempt loopback is the operator's escape hatch: it must
// never be escalated, however many times it trips the rate limit.
func TestBlacklist_LoopbackExempt(t *testing.T) {
	srv, st := newTestServer(t, pullLogNodes())
	setSettings(t, srv, st, map[string]string{
		"pull_rate_limit_per_hour":        "1",
		"pull_blacklist_escalation_count": "1",
	})
	h := srv.Handler()
	ep, _ := st.CreateEndpoint("回环设备")

	pullOnce(t, h, ep, "127.0.0.1")
	for i := 1; i <= 3; i++ {
		if w := pullOnce(t, h, ep, "127.0.0.1"); w.Code != http.StatusTooManyRequests {
			t.Fatalf("loopback rejection %d: status = %d, want 429", i, w.Code)
		}
	}
	if rule := subRuleFor(t, st, "127.0.0.1"); rule != nil {
		t.Errorf("loopback escalated to rule %+v, want exempt", rule)
	}
}

// TestBlacklist_EscalationSettings both keys are read from the super admin
// settings, and a garbled or non-positive value keeps the shipped default
// instead of silently disabling (count 0 would ban on the first rejection,
// duration 0 would write a permanent ban).
func TestBlacklist_EscalationSettings(t *testing.T) {
	srv, st := newTestServer(t, nil)

	policy := srv.loadSecurityPolicy()
	if policy.PullBlacklistEscalationCount != defaultPullBlacklistEscalationCount {
		t.Errorf("default count = %d, want %d",
			policy.PullBlacklistEscalationCount, defaultPullBlacklistEscalationCount)
	}
	if policy.PullBlacklistDuration != defaultPullBlacklistDuration {
		t.Errorf("default duration = %v, want %v",
			policy.PullBlacklistDuration, defaultPullBlacklistDuration)
	}

	if err := st.SaveSystemSettings(map[string]string{
		"pull_blacklist_escalation_count": "3",
		"pull_blacklist_duration":         "6h",
	}); err != nil {
		t.Fatalf("SaveSystemSettings: %v", err)
	}
	policy = srv.loadSecurityPolicy()
	if policy.PullBlacklistEscalationCount != 3 {
		t.Errorf("count after save = %d, want 3", policy.PullBlacklistEscalationCount)
	}
	if policy.PullBlacklistDuration != 6*time.Hour {
		t.Errorf("duration after save = %v, want 6h", policy.PullBlacklistDuration)
	}

	for _, bad := range []string{"abc", "0", "-1", " "} {
		if err := st.SaveSystemSettings(map[string]string{
			"pull_blacklist_escalation_count": bad,
			"pull_blacklist_duration":         bad,
		}); err != nil {
			t.Fatalf("SaveSystemSettings(%q): %v", bad, err)
		}
		policy = srv.loadSecurityPolicy()
		if policy.PullBlacklistEscalationCount != defaultPullBlacklistEscalationCount {
			t.Errorf("count for %q = %d, want the default %d",
				bad, policy.PullBlacklistEscalationCount, defaultPullBlacklistEscalationCount)
		}
		if policy.PullBlacklistDuration != defaultPullBlacklistDuration {
			t.Errorf("duration for %q = %v, want the default %v",
				bad, policy.PullBlacklistDuration, defaultPullBlacklistDuration)
		}
	}
}

// TestBlacklist_GuardRunsBeforeRateLimit chain order is behaviour: an already
// blacklisted IP must be answered by the blacklist guard, so it never consumes
// (or is credited with) rate limiter quota.
func TestBlacklist_GuardRunsBeforeRateLimit(t *testing.T) {
	srv, _ := newTestServer(t, pullLogNodes())

	names := make([]string, 0, len(srv.subGuards))
	for _, g := range srv.subGuards {
		names = append(names, g.name())
	}
	blacklistAt, rateLimitAt := -1, -1
	for i, name := range names {
		switch name {
		case "pull_blacklist":
			blacklistAt = i
		case "rate_limit":
			rateLimitAt = i
		}
	}
	if blacklistAt < 0 {
		t.Fatalf("pull_blacklist guard not registered (chain: %s)", strings.Join(names, " -> "))
	}
	if rateLimitAt < 0 {
		t.Fatalf("rate_limit guard not registered (chain: %s)", strings.Join(names, " -> "))
	}
	if blacklistAt != 0 {
		t.Errorf("pull_blacklist is at %d, want first (chain: %s)",
			blacklistAt, strings.Join(names, " -> "))
	}
	if blacklistAt > rateLimitAt {
		t.Errorf("pull_blacklist runs after rate_limit (chain: %s)", strings.Join(names, " -> "))
	}
}
