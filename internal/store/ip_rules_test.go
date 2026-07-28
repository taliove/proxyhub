package store

import (
	"errors"
	"testing"
	"time"
)

// Unified IP access rules (pull guard ticket 02).

// newIPRuleStore returns a store with the ip_access_rules schema applied.
// The schema is a standalone EnsureIPAccessRulesSchema call (not yet wired into
// migrate()), so every test applies it explicitly.
func newIPRuleStore(t *testing.T) *Store {
	t.Helper()
	s := newTestStore(t)
	if err := s.EnsureIPAccessRulesSchema(); err != nil {
		t.Fatalf("EnsureIPAccessRulesSchema: %v", err)
	}
	return s
}

// mustAddIPRule adds a permanent rule, failing the test on error.
func mustAddIPRule(t *testing.T, s *Store, target, scope string) *IPAccessRule {
	t.Helper()
	rule, err := s.AddIPAccessRule(target, scope, IPRuleSourceManual, "test", 0)
	if err != nil {
		t.Fatalf("AddIPAccessRule(%s, %s): %v", target, scope, err)
	}
	return rule
}

// assertDenied checks IsDenied for one (ip, scope) pair.
func assertDenied(t *testing.T, s *Store, ip, scope string, want bool) {
	t.Helper()
	got, err := s.IsDenied(ip, scope)
	if err != nil {
		t.Fatalf("IsDenied(%s, %s): %v", ip, scope, err)
	}
	if got != want {
		t.Errorf("IsDenied(%s, %s) = %v, want %v", ip, scope, got, want)
	}
}

// TestEnsureIPAccessRulesSchema_Idempotent verifies the table and index appear
// and that repeated calls neither fail nor drop existing rows.
func TestIPRuleSchema_EnsureIdempotent(t *testing.T) {
	s := newIPRuleStore(t)

	if _, err := s.db.Exec(
		`SELECT id, ip_or_cidr, scope, source, expires_at, comment, created_at FROM ip_access_rules LIMIT 0`,
	); err != nil {
		t.Fatalf("ip_access_rules table missing or malformed: %v", err)
	}
	if !indexExists(t, s, "idx_ip_access_rules_scope") {
		t.Error("idx_ip_access_rules_scope missing")
	}

	mustAddIPRule(t, s, "203.0.113.7", IPRuleScopeGlobal)
	if err := s.EnsureIPAccessRulesSchema(); err != nil {
		t.Fatalf("second EnsureIPAccessRulesSchema: %v", err)
	}
	rules, err := s.ListIPAccessRules("")
	if err != nil {
		t.Fatalf("ListIPAccessRules: %v", err)
	}
	if len(rules) != 1 {
		t.Errorf("rules len = %d, want 1 after re-running the migration", len(rules))
	}
}

// TestIPRuleMatch_ExactAddress covers the /32 case: the listed address matches,
// its neighbour does not.
func TestIPRuleMatch_ExactAddress(t *testing.T) {
	s := newIPRuleStore(t)
	mustAddIPRule(t, s, "203.0.113.7", IPRuleScopeSub)

	assertDenied(t, s, "203.0.113.7", IPRuleScopeSub, true)
	assertDenied(t, s, "203.0.113.8", IPRuleScopeSub, false)
	assertDenied(t, s, "203.0.113.6", IPRuleScopeSub, false)
}

// TestIPRuleMatch_ExplicitSlash32 verifies an explicitly written /32 behaves
// exactly like the bare address.
func TestIPRuleMatch_ExplicitSlash32(t *testing.T) {
	s := newIPRuleStore(t)
	mustAddIPRule(t, s, "203.0.113.7/32", IPRuleScopeSub)

	assertDenied(t, s, "203.0.113.7", IPRuleScopeSub, true)
	assertDenied(t, s, "203.0.113.8", IPRuleScopeSub, false)
}

// TestIPRuleMatch_CIDRBoundaries covers block membership including the first
// and last address of the range and the addresses just outside it.
func TestIPRuleMatch_CIDRBoundaries(t *testing.T) {
	s := newIPRuleStore(t)
	mustAddIPRule(t, s, "203.0.113.0/24", IPRuleScopeGlobal)

	for _, ip := range []string{"203.0.113.0", "203.0.113.1", "203.0.113.128", "203.0.113.255"} {
		assertDenied(t, s, ip, IPRuleScopeGlobal, true)
	}
	for _, ip := range []string{"203.0.112.255", "203.0.114.0", "198.51.100.7"} {
		assertDenied(t, s, ip, IPRuleScopeGlobal, false)
	}
}

// TestIPRuleTarget_CIDRNormalizedToNetwork verifies a host-bit-carrying CIDR is
// stored in network form, so the same block cannot be added twice under two
// spellings.
func TestIPRuleTarget_CIDRNormalizedToNetwork(t *testing.T) {
	s := newIPRuleStore(t)
	rule := mustAddIPRule(t, s, "203.0.113.77/24", IPRuleScopeGlobal)
	if rule.IPOrCIDR != "203.0.113.0/24" {
		t.Errorf("ip_or_cidr = %q, want 203.0.113.0/24", rule.IPOrCIDR)
	}

	again := mustAddIPRule(t, s, "203.0.113.0/24", IPRuleScopeGlobal)
	if again.ID != rule.ID {
		t.Errorf("re-adding the same block created id %d (first %d), want an upsert", again.ID, rule.ID)
	}
}

// TestIPRuleMatch_ScopeIsolation verifies a rule only bites inside its scope.
func TestIPRuleMatch_ScopeIsolation(t *testing.T) {
	s := newIPRuleStore(t)
	mustAddIPRule(t, s, "203.0.113.7", IPRuleScopeSub)

	assertDenied(t, s, "203.0.113.7", IPRuleScopeSub, true)
	assertDenied(t, s, "203.0.113.7", IPRuleScopeGlobal, false)
}

// TestIPRuleMatch_ExpiredRuleNeverMatches verifies expiry is evaluated at match
// time: a lapsed rule stops denying without any cleanup job.
func TestIPRuleMatch_ExpiredRuleNeverMatches(t *testing.T) {
	s := newIPRuleStore(t)
	// Seeded two hours in the past with a one hour TTL: expires_at is an hour ago.
	past := time.Now().UTC().Add(-2 * time.Hour)
	if _, err := s.addIPAccessRuleAt("203.0.113.7", IPRuleScopeSub, IPRuleSourceAuto, "lapsed", time.Hour, past); err != nil {
		t.Fatalf("addIPAccessRuleAt: %v", err)
	}

	assertDenied(t, s, "203.0.113.7", IPRuleScopeSub, false)

	rules, err := s.ListIPAccessRules(IPRuleScopeSub)
	if err != nil {
		t.Fatalf("ListIPAccessRules: %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("rules len = %d, want the lapsed rule to remain listed", len(rules))
	}
	if !rules[0].Expired() {
		t.Error("Expired() = false for a rule whose window closed an hour ago")
	}
	if rules[0].Permanent() {
		t.Error("Permanent() = true for a TTL rule")
	}
}

// TestIPRuleMatch_ExpiryBoundary pins the boundary: a rule expiring exactly now
// is already dead, one expiring a second later still bites.
func TestIPRuleMatch_ExpiryBoundary(t *testing.T) {
	s := newIPRuleStore(t)
	now := time.Now().UTC()
	if _, err := s.addIPAccessRuleAt("203.0.113.7", IPRuleScopeSub, IPRuleSourceAuto, "", time.Hour, now.Add(-time.Hour)); err != nil {
		t.Fatalf("addIPAccessRuleAt: %v", err)
	}

	denied, err := s.isDeniedAt("203.0.113.7", IPRuleScopeSub, now)
	if err != nil {
		t.Fatalf("isDeniedAt: %v", err)
	}
	if denied {
		t.Error("rule expiring exactly at now still denies, want expired")
	}

	denied, err = s.isDeniedAt("203.0.113.7", IPRuleScopeSub, now.Add(-time.Second))
	if err != nil {
		t.Fatalf("isDeniedAt: %v", err)
	}
	if !denied {
		t.Error("rule with a second left does not deny, want a hit")
	}
}

// TestIPRuleMatch_PermanentRuleNeverExpires verifies a nil expires_at rule keeps
// matching far into the future.
func TestIPRuleMatch_PermanentRuleNeverExpires(t *testing.T) {
	s := newIPRuleStore(t)
	rule := mustAddIPRule(t, s, "203.0.113.7", IPRuleScopeGlobal)
	if !rule.Permanent() {
		t.Fatal("Permanent() = false for a zero-TTL rule")
	}

	denied, err := s.isDeniedAt("203.0.113.7", IPRuleScopeGlobal, time.Now().UTC().AddDate(10, 0, 0))
	if err != nil {
		t.Fatalf("isDeniedAt: %v", err)
	}
	if !denied {
		t.Error("permanent rule stopped matching ten years out")
	}
}

// TestIPRuleMatch_LoopbackDeniedLikeAnyAddress pins the post-C1 semantics:
// the store no longer special-cases loopback. The escape hatch moved to the
// HTTP caller layer (isDirectLoopback in internal/server), because the store
// cannot distinguish a real local connection from a forged
// X-Forwarded-For: 127.0.0.1. Rules match loopback like any other address.
func TestIPRuleMatch_LoopbackDeniedLikeAnyAddress(t *testing.T) {
	s := newIPRuleStore(t)
	for _, target := range []string{"127.0.0.1", "127.0.0.0/8", "0.0.0.0/0", "::1"} {
		if _, err := s.AddIPAccessRule(target, IPRuleScopeGlobal, IPRuleSourceManual, "", 0); err != nil {
			t.Fatalf("AddIPAccessRule(%s): %v", target, err)
		}
	}

	for _, ip := range []string{"127.0.0.1", "127.0.0.53", "::1"} {
		assertDenied(t, s, ip, IPRuleScopeGlobal, true)
	}
	// The 0.0.0.0/0 rule must still catch a real remote address.
	assertDenied(t, s, "203.0.113.7", IPRuleScopeGlobal, true)
}

// TestIPRuleMatch_MalformedInputFailsOpen verifies an unparsable source address
// is not denied (and does not error).
func TestIPRuleMatch_MalformedInputFailsOpen(t *testing.T) {
	s := newIPRuleStore(t)
	mustAddIPRule(t, s, "0.0.0.0/0", IPRuleScopeGlobal)

	assertDenied(t, s, "not-an-ip", IPRuleScopeGlobal, false)
	assertDenied(t, s, "", IPRuleScopeGlobal, false)
}

// TestIPRuleMatch_InvalidScopeRejected verifies the scope argument is validated
// rather than silently matching nothing.
func TestIPRuleMatch_InvalidScopeRejected(t *testing.T) {
	s := newIPRuleStore(t)
	if _, err := s.IsDenied("203.0.113.7", "everything"); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("IsDenied with a bogus scope error = %v, want ErrInvalidInput", err)
	}
}

// TestIPRuleCache_InvalidatedOnAdd verifies a match after a warm cache picks up
// a newly added rule (no stale miss).
func TestIPRuleCache_InvalidatedOnAdd(t *testing.T) {
	s := newIPRuleStore(t)
	assertDenied(t, s, "203.0.113.7", IPRuleScopeSub, false) // warms the cache

	mustAddIPRule(t, s, "203.0.113.7", IPRuleScopeSub)
	assertDenied(t, s, "203.0.113.7", IPRuleScopeSub, true)
}

// TestIPRuleCache_InvalidatedOnDelete verifies deletion takes effect on the very
// next match (no stale hit).
func TestIPRuleCache_InvalidatedOnDelete(t *testing.T) {
	s := newIPRuleStore(t)
	rule := mustAddIPRule(t, s, "203.0.113.0/24", IPRuleScopeGlobal)
	assertDenied(t, s, "203.0.113.7", IPRuleScopeGlobal, true) // warms the cache

	if err := s.DeleteIPAccessRule(rule.ID); err != nil {
		t.Fatalf("DeleteIPAccessRule: %v", err)
	}
	assertDenied(t, s, "203.0.113.7", IPRuleScopeGlobal, false)
}

// TestIPRuleCache_InvalidatedOnPromote verifies a promoted rule immediately
// denies in its new scope and stops denying in the old one.
func TestIPRuleCache_InvalidatedOnPromote(t *testing.T) {
	s := newIPRuleStore(t)
	rule := mustAddIPRule(t, s, "203.0.113.7", IPRuleScopeSub)
	assertDenied(t, s, "203.0.113.7", IPRuleScopeSub, true)
	assertDenied(t, s, "203.0.113.7", IPRuleScopeGlobal, false)

	promoted, err := s.PromoteIPAccessRule(rule.ID)
	if err != nil {
		t.Fatalf("PromoteIPAccessRule: %v", err)
	}
	if promoted.Scope != IPRuleScopeGlobal {
		t.Errorf("scope = %q, want global", promoted.Scope)
	}
	if promoted.ID != rule.ID {
		t.Errorf("id changed on promote: %d -> %d", rule.ID, promoted.ID)
	}
	assertDenied(t, s, "203.0.113.7", IPRuleScopeGlobal, true)
	assertDenied(t, s, "203.0.113.7", IPRuleScopeSub, false)
}

// TestPromoteIPAccessRule_AbsorbsExistingGlobal verifies promotion does not trip
// the UNIQUE(target, scope) constraint when a global rule already covers the
// same target.
func TestPromoteIPAccessRule_AbsorbsExistingGlobal(t *testing.T) {
	s := newIPRuleStore(t)
	mustAddIPRule(t, s, "203.0.113.7", IPRuleScopeGlobal)
	sub := mustAddIPRule(t, s, "203.0.113.7", IPRuleScopeSub)

	if _, err := s.PromoteIPAccessRule(sub.ID); err != nil {
		t.Fatalf("PromoteIPAccessRule: %v", err)
	}
	rules, err := s.ListIPAccessRules(IPRuleScopeGlobal)
	if err != nil {
		t.Fatalf("ListIPAccessRules: %v", err)
	}
	if len(rules) != 1 || rules[0].ID != sub.ID {
		t.Errorf("global rules = %+v, want only the promoted row (id %d)", rules, sub.ID)
	}
}

// TestPromoteIPAccessRule_RejectsNonSub verifies promoting a global rule is an
// input error rather than a silent no-op.
func TestPromoteIPAccessRule_RejectsNonSub(t *testing.T) {
	s := newIPRuleStore(t)
	rule := mustAddIPRule(t, s, "203.0.113.7", IPRuleScopeGlobal)

	if _, err := s.PromoteIPAccessRule(rule.ID); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("PromoteIPAccessRule(global) error = %v, want ErrInvalidInput", err)
	}
	if _, err := s.PromoteIPAccessRule(999999); !errors.Is(err, ErrNotFound) {
		t.Errorf("PromoteIPAccessRule(missing) error = %v, want ErrNotFound", err)
	}
}

// TestIPRuleCRUD_ListFilterAndUpsert covers the listing surface: scope filter,
// source/comment/TTL round trip, and the upsert sliding an existing window.
func TestIPRuleCRUD_ListFilterAndUpsert(t *testing.T) {
	s := newIPRuleStore(t)
	mustAddIPRule(t, s, "203.0.113.7", IPRuleScopeGlobal)
	subRule, err := s.AddIPAccessRule("198.51.100.0/24", IPRuleScopeSub, IPRuleSourceAuto, "限频超限自动封禁", 24*time.Hour)
	if err != nil {
		t.Fatalf("AddIPAccessRule: %v", err)
	}
	if subRule.Source != IPRuleSourceAuto {
		t.Errorf("source = %q, want auto", subRule.Source)
	}
	if subRule.Comment != "限频超限自动封禁" {
		t.Errorf("comment = %q, want the Chinese note round-tripped", subRule.Comment)
	}
	if subRule.ExpiresAt == nil {
		t.Fatal("expires_at is nil for a 24h rule")
	}
	if subRule.CreatedAt.IsZero() {
		t.Error("created_at not persisted")
	}

	globals, err := s.ListIPAccessRules(IPRuleScopeGlobal)
	if err != nil {
		t.Fatalf("ListIPAccessRules(global): %v", err)
	}
	if len(globals) != 1 || globals[0].IPOrCIDR != "203.0.113.7" {
		t.Errorf("global rules = %+v, want only 203.0.113.7", globals)
	}
	all, err := s.ListIPAccessRules("")
	if err != nil {
		t.Fatalf("ListIPAccessRules(all): %v", err)
	}
	if len(all) != 2 {
		t.Errorf("all rules len = %d, want 2", len(all))
	}

	// Upsert: same (target, scope) slides the window and rewrites the metadata.
	updated, err := s.AddIPAccessRule("198.51.100.0/24", IPRuleScopeSub, IPRuleSourceManual, "改为永久", 0)
	if err != nil {
		t.Fatalf("AddIPAccessRule(upsert): %v", err)
	}
	if updated.ID != subRule.ID {
		t.Errorf("upsert created a new row (%d vs %d)", updated.ID, subRule.ID)
	}
	if !updated.Permanent() {
		t.Error("upsert to permanent left expires_at set")
	}
	if updated.Source != IPRuleSourceManual || updated.Comment != "改为永久" {
		t.Errorf("upsert did not rewrite metadata: %+v", updated)
	}
}

// TestIPRuleCRUD_DeleteMissingIsNotFound verifies a repeated delete reports
// ErrNotFound instead of pretending to have removed a row.
func TestIPRuleCRUD_DeleteMissingIsNotFound(t *testing.T) {
	s := newIPRuleStore(t)
	rule := mustAddIPRule(t, s, "203.0.113.7", IPRuleScopeSub)

	if err := s.DeleteIPAccessRule(rule.ID); err != nil {
		t.Fatalf("DeleteIPAccessRule: %v", err)
	}
	if err := s.DeleteIPAccessRule(rule.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete error = %v, want ErrNotFound", err)
	}
	if _, err := s.GetIPAccessRule(rule.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetIPAccessRule after delete = %v, want ErrNotFound", err)
	}
}

// TestIPRuleCRUD_RejectsInvalidInput verifies boundary validation on writes.
func TestIPRuleCRUD_RejectsInvalidInput(t *testing.T) {
	s := newIPRuleStore(t)
	cases := []struct{ target, scope, source string }{
		{"", IPRuleScopeSub, IPRuleSourceManual},
		{"not-an-ip", IPRuleScopeSub, IPRuleSourceManual},
		{"203.0.113.0/33", IPRuleScopeSub, IPRuleSourceManual},
		{"203.0.113.7", "nowhere", IPRuleSourceManual},
		{"203.0.113.7", IPRuleScopeSub, "guesswork"},
	}
	for _, c := range cases {
		if _, err := s.AddIPAccessRule(c.target, c.scope, c.source, "", 0); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("AddIPAccessRule(%q, %q, %q) error = %v, want ErrInvalidInput",
				c.target, c.scope, c.source, err)
		}
	}
}
