package server

import (
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/taliove/proxyhub/internal/store"
)

// Pull blacklist guard and its auto escalation chain (pull-guard ticket 05).
//
// Two halves of one feature:
//   - the guard rejects a source that already has a live scope=sub rule;
//   - the escalation chain is what puts most of those rules there, by turning
//     repeated rate-limit rejections from one source into a bounded auto rule.
//
// The guard is registered first in the chain (see newSubGuardChain) so a banned
// source is answered before it can consume rate limiter quota, and so the
// escalation cannot keep re-counting a source it has already dealt with.

const (
	// defaultPullBlacklistEscalationCount is the shipped escalation threshold:
	// 10 rate-limit rejections from one IP inside pullEscalationWindow. At the
	// default 60 pulls/hour a client has to be well past "misconfigured client"
	// territory to get here.
	defaultPullBlacklistEscalationCount = 10
	// defaultPullBlacklistDuration is how long an auto rule holds. A day is long
	// enough to make scripted hammering pointless and short enough that a NAT
	// address shared with innocent users heals on its own - the operator can
	// promote it to a permanent global rule if it deserves one.
	defaultPullBlacklistDuration = 24 * time.Hour
	// pullEscalationWindow is the lookback for counting rate_limited rows. Same
	// hour as the rate limiter's window, so "10 rejections in the window the
	// limit is expressed in" is one consistent statement.
	pullEscalationWindow = time.Hour
	// pullEscalationPolicyTTL caches the two escalation settings for this long,
	// for the same reason as pullRateThresholdTTL: the chain runs on rejected
	// pulls, which is the path an abusive client controls, so a per-request
	// settings read would let it amplify its own load. Operator changes take
	// effect within this window (immediately on a settings save, which
	// invalidates).
	pullEscalationPolicyTTL = 5 * time.Second
)

// pullBlacklistGuard rejects pulls from a source with a live scope=sub rule.
//
// The store's IsDenied is served from an in-memory rule cache (invalidated on
// write), so this runs without a DB query per pull despite being DB-backed
// state. escalation is carried here only so the wiring in newSubGuardChain can
// build both halves from one line; the guard itself does not escalate.
type pullBlacklistGuard struct {
	st        *store.Store
	logger    *slog.Logger
	escalator *pullEscalator
}

// newPullBlacklistGuard wires the guard to the rule store.
func newPullBlacklistGuard(st *store.Store, logger *slog.Logger, policy *cachedEscalationPolicy) *pullBlacklistGuard {
	return &pullBlacklistGuard{st: st, logger: logger, escalator: newPullEscalator(st, logger, policy)}
}

func (g *pullBlacklistGuard) name() string { return "pull_blacklist" }

// check blocks the pull when a live scope=sub rule matches the client.
//
// 403 rather than the uniform 404: the request carries a valid path+token pair,
// so the client already knows the address exists and hiding the ban only makes
// a legitimately-banned user (a shared NAT address, an over-eager script)
// unable to tell a ban from an outage. Nothing is leaked that the token holder
// does not already know.
//
// A store error fails open. The alternative - refusing pulls when the rule
// cache cannot load - would turn a database hiccup into a total subscription
// outage for every user, which is a worse failure than briefly serving a banned
// source that the very next request will catch.
func (g *pullBlacklistGuard) check(gr subGuardRequest) subGuardVerdict {
	denied, err := g.st.IsDenied(gr.ip, store.IPRuleScopeSub)
	if err != nil {
		g.logger.Error("pull blacklist lookup failed", "ip", gr.ip, "error", err)
		return allowPull()
	}
	if !denied {
		return allowPull()
	}
	return blockPull(store.PullStatusBlacklisted, func(w http.ResponseWriter) {
		http.Error(w, "forbidden", http.StatusForbidden)
	})
}

// pullEscalator turns repeated rate-limit rejections from one source into an
// auto scope=sub rule.
type pullEscalator struct {
	st     *store.Store
	logger *slog.Logger
	policy *cachedEscalationPolicy
}

// newPullEscalator wires an escalator to the rule store and settings cache.
// No clock seam: the rule's expiry is computed by the store from the TTL this
// passes in, so there is only one clock involved and it already has one.
func newPullEscalator(st *store.Store, logger *slog.Logger, policy *cachedEscalationPolicy) *pullEscalator {
	return &pullEscalator{st: st, logger: logger, policy: policy}
}

// observeRateLimited books one rate-limit rejection from ip and escalates when
// the source has reached the threshold inside pullEscalationWindow.
//
// Called from the rate limiter's rejection path (see escalatingGuard), i.e.
// *before* runSubGuards has written this rejection's own pull_logs row. The
// count is therefore short by one and the comparison adds it back, which keeps
// the semantics "the Nth rejection is the one that escalates" independent of
// where in the request the trace is written.
//
// Loopback never escalates: it is the operator's escape hatch (SSH tunnel,
// localhost admin), and behind an untrusted reverse proxy every request looks
// like 127.0.0.1, so escalating it could lock an operator out of their own box
// on a misconfiguration. store.IsDenied refuses to match loopback for the same
// reason; skipping it here means no useless row is written either.
//
// Errors are logged and swallowed: escalation is a hardening bonus on top of
// the rate limit, and failing it must not change the answer the client already
// has coming (429).
//
// Re-escalation after expiry is intentional: the count reads pull_logs, which
// outlives the rule, so a source that resumes hammering within the window of
// its old rejections is re-banned on its first new one rather than getting a
// fresh allowance of threshold attempts. At the shipped 24h duration the old
// rows have long left the 1h window, so this only bites the operator who
// configures a duration shorter than the window - for whom "the repeat offender
// comes straight back" is the wanted behaviour.
func (e *pullEscalator) observeRateLimited(ip string) {
	// isLoopbackAddr (handlers_trusted_ips.go) is the parse-based check, not the
	// literal "127.0.0.1" one: ::1 and 127.0.0.2 are loopback too.
	if isLoopbackAddr(ip) {
		return
	}
	threshold, ttl := e.policy.get()
	count, err := e.st.CountRecentPullStatus(ip, store.PullStatusRateLimited, pullEscalationWindow)
	if err != nil {
		e.logger.Error("pull escalation count failed", "ip", ip, "error", err)
		return
	}
	if count+1 < threshold {
		return
	}

	comment := fmt.Sprintf("自动封禁:1 小时内拉取超限 %d 次(阈值 %d),封禁 %s",
		count+1, threshold, ttl)
	rule, err := e.st.AddIPAccessRule(ip, store.IPRuleScopeSub, store.IPRuleSourceAuto, comment, ttl)
	if err != nil {
		e.logger.Error("pull escalation rule write failed", "ip", ip, "error", err)
		return
	}
	e.logger.Warn("pull source auto-blacklisted",
		"ip", ip, "rate_limited_count", count+1, "threshold", threshold,
		"duration", ttl.String(), "rule_id", rule.ID)
}

// escalatingGuard wraps a guard and feeds its rate-limit rejections to the
// escalator. A decorator rather than a callback inside rateLimitGuard: the
// escalation chain is this ticket's concern, so the rate limiter stays a pure
// "is this over quota" decision with no knowledge of what happens next.
//
// The verdict is passed through untouched: an escalated source still gets the
// 429 it earned on this request, and only the *next* request meets the
// blacklist guard's 403.
type escalatingGuard struct {
	inner     subGuard
	escalator *pullEscalator
}

// newEscalatingGuard wraps inner so its rate_limited verdicts feed escalator.
func newEscalatingGuard(inner subGuard, escalator *pullEscalator) *escalatingGuard {
	return &escalatingGuard{inner: inner, escalator: escalator}
}

// name reports the wrapped guard's name, so logs and the chain-order contract
// see the guard the operator configured, not the wrapper.
func (g *escalatingGuard) name() string { return g.inner.name() }

func (g *escalatingGuard) check(gr subGuardRequest) subGuardVerdict {
	verdict := g.inner.check(gr)
	if verdict.block && verdict.status == store.PullStatusRateLimited {
		g.escalator.observeRateLimited(gr.ip)
	}
	return verdict
}

// cachedEscalationPolicy memoises the two escalation settings for a TTL. Same
// role as cachedThreshold, but it carries a (count, duration) pair: reading them
// separately would let one refresh land between the two and mix an old count
// with a new duration.
type cachedEscalationPolicy struct {
	mu     sync.Mutex
	load   func() (int, time.Duration)
	ttl    time.Duration
	now    func() time.Time
	count  int
	dur    time.Duration
	loaded time.Time
}

// newCachedEscalationPolicy wraps load with a ttl-bounded cache.
func newCachedEscalationPolicy(ttl time.Duration, load func() (int, time.Duration)) *cachedEscalationPolicy {
	return &cachedEscalationPolicy{load: load, ttl: ttl, now: time.Now}
}

// get returns the cached pair, refreshing it when the TTL has elapsed. The load
// happens under the mutex so a burst of misses collapses into one DB read.
func (c *cachedEscalationPolicy) get() (int, time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	if c.loaded.IsZero() || now.Sub(c.loaded) >= c.ttl {
		c.count, c.dur = c.load()
		c.loaded = now
	}
	return c.count, c.dur
}

// invalidate forces the next get to reload, called when settings are saved.
func (c *cachedEscalationPolicy) invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.loaded = time.Time{}
}

// pullBlacklistPolicy resolves the escalation threshold and auto-rule duration
// from the super admin settings, through the same fallback chain as every other
// security setting so there is one source of truth.
//
// Uncached: the guard reads through pullEscalation() (see
// pullEscalationPolicyTTL), because this hits the DB.
func (s *Server) pullBlacklistPolicy() (int, time.Duration) {
	policy := s.loadSecurityPolicy()
	return policy.PullBlacklistEscalationCount, policy.PullBlacklistDuration
}

// pullEscalationPolicies maps a Server to its escalation settings cache. Kept
// out of the Server struct (same pattern as store.ipRuleCaches) so this feature
// owns its own state: one entry per Server, created on first use, tiny, and a
// Server is a process singleton.
var pullEscalationPolicies sync.Map // *Server -> *cachedEscalationPolicy

// pullEscalation returns this server's escalation settings cache, creating it on
// first call. LoadOrStore keeps concurrent first calls on one instance, so the
// TTL is never split across two caches.
func (s *Server) pullEscalation() *cachedEscalationPolicy {
	if c, ok := pullEscalationPolicies.Load(s); ok {
		return c.(*cachedEscalationPolicy)
	}
	c, _ := pullEscalationPolicies.LoadOrStore(s,
		newCachedEscalationPolicy(pullEscalationPolicyTTL, s.pullBlacklistPolicy))
	return c.(*cachedEscalationPolicy)
}
