package store

import (
	"database/sql"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

// Unified IP access rules (pull guard ticket 02).
//
// One table backs both the site-wide deny list (scope=global, enforced by the
// request middleware) and the subscription pull blacklist (scope=sub, enforced
// by the pull guard chain). Keeping a single table means one matching engine,
// one CRUD surface and one expiry semantic instead of two drifting copies.
//
// A rule is a single address ("203.0.113.7") or a CIDR block
// ("203.0.113.0/24"). expires_at NULL means permanent; a past expires_at is a
// dead rule that simply stops matching (no cleanup job needed).

// IP rule scopes. A rule is only consulted within its own scope: a sub rule
// never blocks the admin surface, and a global rule is not a pull-only ban.
const (
	// IPRuleScopeGlobal denies every request from the source (site-wide).
	IPRuleScopeGlobal = "global"
	// IPRuleScopeSub denies subscription pulls only.
	IPRuleScopeSub = "sub"
)

// IP rule sources. manual = written by an operator, auto = written by the
// escalation chain (repeated rate-limit hits). The distinction exists so the
// UI can show what the system decided on its own, and so an operator can
// promote an auto entry into a permanent global one.
const (
	IPRuleSourceManual = "manual"
	IPRuleSourceAuto   = "auto"
)

// IPAccessRule is one row of ip_access_rules. ExpiresAt is nil for a permanent
// rule; times are UTC.
type IPAccessRule struct {
	ID        int64      `json:"id"`
	IPOrCIDR  string     `json:"ip_or_cidr"`
	Scope     string     `json:"scope"`
	Source    string     `json:"source"`
	ExpiresAt *time.Time `json:"expires_at"`
	Comment   string     `json:"comment"`
	CreatedAt time.Time  `json:"created_at"`
}

// Permanent reports whether the rule never expires.
func (r *IPAccessRule) Permanent() bool { return r.ExpiresAt == nil }

// Expired reports whether the rule's window has already closed. Permanent
// rules are never expired.
func (r *IPAccessRule) Expired() bool { return r.expiredAt(time.Now().UTC()) }

func (r *IPAccessRule) expiredAt(now time.Time) bool {
	if r.ExpiresAt == nil {
		return false
	}
	return !r.ExpiresAt.After(now)
}

// EnsureIPAccessRulesSchema creates ip_access_rules and its indexes.
// Idempotent: safe to call on every startup.
//
// UNIQUE(ip_or_cidr, scope) makes "ban this source in this scope" an upsert
// rather than a growing pile of duplicate rows - the auto escalation chain
// re-bans the same address repeatedly and must slide the window instead of
// stacking entries. The (scope, ip_or_cidr) index serves the cache warm-up
// query.
func (s *Store) EnsureIPAccessRulesSchema() error {
	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS ip_access_rules (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	ip_or_cidr  TEXT NOT NULL,
	scope       TEXT NOT NULL,
	source      TEXT NOT NULL DEFAULT 'manual',
	expires_at  TIMESTAMP,
	comment     TEXT NOT NULL DEFAULT '',
	created_at  TIMESTAMP NOT NULL,
	UNIQUE (ip_or_cidr, scope)
)`); err != nil {
		return fmt.Errorf("create ip_access_rules: %w", err)
	}
	if _, err := s.db.Exec(
		`CREATE INDEX IF NOT EXISTS idx_ip_access_rules_scope ON ip_access_rules(scope, ip_or_cidr)`,
	); err != nil {
		return fmt.Errorf("create idx_ip_access_rules_scope: %w", err)
	}
	return nil
}

// NormalizeIPRuleTarget validates and canonicalizes a rule target. A bare
// address is kept as-is; a CIDR is masked to its network form ("203.0.113.7/24"
// -> "203.0.113.0/24") so the same block cannot be stored under two spellings
// and defeat the UNIQUE constraint.
func NormalizeIPRuleTarget(target string) (string, error) {
	trimmed := strings.TrimSpace(target)
	if trimmed == "" {
		return "", fmt.Errorf("%w: ip_or_cidr is required", ErrInvalidInput)
	}
	if strings.Contains(trimmed, "/") {
		_, network, err := net.ParseCIDR(trimmed)
		if err != nil {
			return "", fmt.Errorf("%w: invalid cidr %q", ErrInvalidInput, trimmed)
		}
		return network.String(), nil
	}
	ip := net.ParseIP(trimmed)
	if ip == nil {
		return "", fmt.Errorf("%w: invalid ip %q", ErrInvalidInput, trimmed)
	}
	return ip.String(), nil
}

// normalizeIPRuleScope validates a scope value.
func normalizeIPRuleScope(scope string) (string, error) {
	switch strings.TrimSpace(scope) {
	case IPRuleScopeGlobal:
		return IPRuleScopeGlobal, nil
	case IPRuleScopeSub:
		return IPRuleScopeSub, nil
	default:
		return "", fmt.Errorf("%w: scope must be %s or %s", ErrInvalidInput, IPRuleScopeGlobal, IPRuleScopeSub)
	}
}

// normalizeIPRuleSource validates a source value, defaulting to manual.
func normalizeIPRuleSource(source string) (string, error) {
	switch strings.TrimSpace(source) {
	case "", IPRuleSourceManual:
		return IPRuleSourceManual, nil
	case IPRuleSourceAuto:
		return IPRuleSourceAuto, nil
	default:
		return "", fmt.Errorf("%w: source must be %s or %s", ErrInvalidInput, IPRuleSourceManual, IPRuleSourceAuto)
	}
}

// AddIPAccessRule writes (or refreshes) a rule. ttl <= 0 means permanent.
// Re-adding an existing (target, scope) pair slides the window and overwrites
// source/comment: the caller's intent is "this is the rule now".
func (s *Store) AddIPAccessRule(target, scope, source, comment string, ttl time.Duration) (*IPAccessRule, error) {
	return s.addIPAccessRuleAt(target, scope, source, comment, ttl, time.Now().UTC())
}

// addIPAccessRuleAt is AddIPAccessRule with an injectable clock so tests can
// seed already-expired rules without sleeping.
func (s *Store) addIPAccessRuleAt(target, scope, source, comment string, ttl time.Duration, now time.Time) (*IPAccessRule, error) {
	addr, err := NormalizeIPRuleTarget(target)
	if err != nil {
		return nil, err
	}
	sc, err := normalizeIPRuleScope(scope)
	if err != nil {
		return nil, err
	}
	src, err := normalizeIPRuleSource(source)
	if err != nil {
		return nil, err
	}

	base := now.UTC()
	var expires any
	if ttl > 0 {
		expires = base.Add(ttl).Format(sqliteTimeLayout)
	}
	if _, err := s.db.Exec(`
		INSERT INTO ip_access_rules (ip_or_cidr, scope, source, expires_at, comment, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(ip_or_cidr, scope) DO UPDATE SET
			source = excluded.source,
			expires_at = excluded.expires_at,
			comment = excluded.comment`,
		addr, sc, src, expires, strings.TrimSpace(comment), base.Format(sqliteTimeLayout),
	); err != nil {
		return nil, fmt.Errorf("add ip access rule: %w", err)
	}
	s.invalidateIPRuleCache()
	return s.GetIPAccessRuleByTarget(addr, sc)
}

// ListIPAccessRules returns rules newest first. scope filters to one scope;
// an empty scope returns everything. Expired rules are included so the UI can
// show and clean up a lapsed entry - callers gate on IsDenied or Expired.
func (s *Store) ListIPAccessRules(scope string) ([]*IPAccessRule, error) {
	query := `
		SELECT id, ip_or_cidr, scope, source, expires_at, comment, created_at
		FROM ip_access_rules`
	args := []any{}
	if strings.TrimSpace(scope) != "" {
		sc, err := normalizeIPRuleScope(scope)
		if err != nil {
			return nil, err
		}
		query += ` WHERE scope = ?`
		args = append(args, sc)
	}
	query += ` ORDER BY datetime(created_at) DESC, id DESC`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list ip access rules: %w", err)
	}
	defer rows.Close()

	out := []*IPAccessRule{}
	for rows.Next() {
		rule, err := scanIPAccessRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rule)
	}
	return out, rows.Err()
}

// GetIPAccessRule reads one rule by id, ErrNotFound when absent.
func (s *Store) GetIPAccessRule(id int64) (*IPAccessRule, error) {
	if id <= 0 {
		return nil, fmt.Errorf("%w: id is required", ErrInvalidInput)
	}
	row := s.db.QueryRow(`
		SELECT id, ip_or_cidr, scope, source, expires_at, comment, created_at
		FROM ip_access_rules WHERE id = ?`, id)
	rule, err := scanIPAccessRule(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return rule, err
}

// GetIPAccessRuleByTarget reads one rule by its (target, scope) key.
func (s *Store) GetIPAccessRuleByTarget(target, scope string) (*IPAccessRule, error) {
	addr, err := NormalizeIPRuleTarget(target)
	if err != nil {
		return nil, err
	}
	sc, err := normalizeIPRuleScope(scope)
	if err != nil {
		return nil, err
	}
	row := s.db.QueryRow(`
		SELECT id, ip_or_cidr, scope, source, expires_at, comment, created_at
		FROM ip_access_rules WHERE ip_or_cidr = ? AND scope = ?`, addr, sc)
	rule, err := scanIPAccessRule(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return rule, err
}

// DeleteIPAccessRule drops a rule by id. ErrNotFound when the row is already
// gone, so the HTTP layer can answer 404 instead of pretending it deleted
// something.
func (s *Store) DeleteIPAccessRule(id int64) error {
	if id <= 0 {
		return fmt.Errorf("%w: id is required", ErrInvalidInput)
	}
	res, err := s.db.Exec(`DELETE FROM ip_access_rules WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete ip access rule: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	s.invalidateIPRuleCache()
	return nil
}

// PromoteIPAccessRule lifts a sub rule to global: the operator decided a pull
// abuser deserves a site-wide block. The row keeps its id, expiry and comment
// so the audit trail stays continuous.
//
// A pre-existing global rule for the same target is absorbed (deleted) rather
// than making the promotion fail on the UNIQUE constraint: both rows express
// the same intent, and keeping the promoted row preserves its history.
// Promoting an already-global rule is ErrInvalidInput - there is nothing above
// global, and silently succeeding would hide a UI bug.
func (s *Store) PromoteIPAccessRule(id int64) (*IPAccessRule, error) {
	rule, err := s.GetIPAccessRule(id)
	if err != nil {
		return nil, err
	}
	if rule.Scope != IPRuleScopeSub {
		return nil, fmt.Errorf("%w: only %s rules can be promoted", ErrInvalidInput, IPRuleScopeSub)
	}
	if _, err := s.db.Exec(
		`DELETE FROM ip_access_rules WHERE ip_or_cidr = ? AND scope = ? AND id <> ?`,
		rule.IPOrCIDR, IPRuleScopeGlobal, id,
	); err != nil {
		return nil, fmt.Errorf("absorb existing global rule: %w", err)
	}
	if _, err := s.db.Exec(
		`UPDATE ip_access_rules SET scope = ? WHERE id = ?`, IPRuleScopeGlobal, id,
	); err != nil {
		return nil, fmt.Errorf("promote ip access rule: %w", err)
	}
	s.invalidateIPRuleCache()
	return s.GetIPAccessRule(id)
}

// IsDenied reports whether ip is currently denied inside scope. Both exact
// addresses and CIDR blocks are consulted; expired rules never match.
//
// Loopback is never denied. The SSH-tunnel/localhost path is the operator's
// escape hatch: a bad rule (or an auto-escalation misfire behind an untrusted
// reverse proxy, where every request looks like 127.0.0.1) must not be able to
// lock the operator out of their own box.
//
// A malformed ip is not denied: the caller has no better information than the
// address it was handed, and failing open here only costs a request that other
// guards still see, while failing closed would 404 the whole site on a parse
// quirk.
func (s *Store) IsDenied(ip, scope string) (bool, error) {
	return s.isDeniedAt(ip, scope, time.Now().UTC())
}

// isDeniedAt is IsDenied with an injectable clock (expiry boundary tests).
func (s *Store) isDeniedAt(ip, scope string, now time.Time) (bool, error) {
	sc, err := normalizeIPRuleScope(scope)
	if err != nil {
		return false, err
	}
	addr := net.ParseIP(strings.TrimSpace(ip))
	if addr == nil {
		return false, nil
	}
	if addr.IsLoopback() {
		return false, nil
	}
	rules, err := s.loadIPRuleCache()
	if err != nil {
		return false, err
	}
	for _, r := range rules[sc] {
		if r.expired(now) {
			continue
		}
		if r.network.Contains(addr) {
			return true, nil
		}
	}
	return false, nil
}

// scanner is the shared surface of *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

// scanIPAccessRule reads one row into an IPAccessRule.
func scanIPAccessRule(src scanner) (*IPAccessRule, error) {
	var (
		rule      IPAccessRule
		expiresAt sql.NullString
		createdAt string
	)
	if err := src.Scan(&rule.ID, &rule.IPOrCIDR, &rule.Scope, &rule.Source,
		&expiresAt, &rule.Comment, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		return nil, fmt.Errorf("scan ip access rule: %w", err)
	}
	if expiresAt.Valid && strings.TrimSpace(expiresAt.String) != "" {
		parsed := parseSQLiteTime(expiresAt.String).UTC()
		rule.ExpiresAt = &parsed
	}
	rule.CreatedAt = parseSQLiteTime(createdAt).UTC()
	return &rule, nil
}

// compiledIPRule is a rule reduced to what matching needs: a network (a bare
// address becomes a /32 or /128) plus its expiry.
type compiledIPRule struct {
	network   *net.IPNet
	expiresAt *time.Time
}

func (c compiledIPRule) expired(now time.Time) bool {
	return c.expiresAt != nil && !c.expiresAt.After(now)
}

// ipRuleCache is the full in-memory rule set, grouped by scope. Rule volume is
// small (operator-authored plus auto escalations), so every request matching
// against the whole set in memory is cheaper than one SQLite query per request.
// Invalidated on write, never on a timer: expiry is evaluated per match, so a
// stale-but-loaded cache still stops honoring a lapsed rule.
type ipRuleCache struct {
	mu      sync.RWMutex
	loaded  bool
	byScope map[string][]compiledIPRule
}

// ipRuleCaches maps a Store to its rule cache. Kept out of the Store struct so
// this feature owns its own state; entries are tiny and live as long as their
// Store.
var ipRuleCaches sync.Map // *Store -> *ipRuleCache

func (s *Store) ipRuleCache() *ipRuleCache {
	if c, ok := ipRuleCaches.Load(s); ok {
		return c.(*ipRuleCache)
	}
	c, _ := ipRuleCaches.LoadOrStore(s, &ipRuleCache{})
	return c.(*ipRuleCache)
}

// invalidateIPRuleCache drops the cached rule set. Every write path calls this,
// so a deleted or promoted rule takes effect on the next match.
func (s *Store) invalidateIPRuleCache() {
	c := s.ipRuleCache()
	c.mu.Lock()
	c.loaded = false
	c.byScope = nil
	c.mu.Unlock()
}

// loadIPRuleCache returns the compiled rule set, reading from SQLite only when
// the cache is cold.
func (s *Store) loadIPRuleCache() (map[string][]compiledIPRule, error) {
	c := s.ipRuleCache()
	c.mu.RLock()
	if c.loaded {
		byScope := c.byScope
		c.mu.RUnlock()
		return byScope, nil
	}
	c.mu.RUnlock()

	byScope, err := s.compileIPRules()
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	// A concurrent write may have invalidated while we were reading; publishing
	// anyway is safe (the writer's own invalidate ran after its commit, so the
	// worst case is one extra reload on the next miss).
	c.byScope = byScope
	c.loaded = true
	c.mu.Unlock()
	return byScope, nil
}

// compileIPRules reads every rule and turns it into matchable form. A row that
// fails to parse is skipped rather than failing the whole load: one bad hand
// edit must not take down matching for every other rule.
func (s *Store) compileIPRules() (map[string][]compiledIPRule, error) {
	rules, err := s.ListIPAccessRules("")
	if err != nil {
		return nil, err
	}
	byScope := make(map[string][]compiledIPRule, 2)
	for _, r := range rules {
		network, err := ruleNetwork(r.IPOrCIDR)
		if err != nil {
			continue
		}
		byScope[r.Scope] = append(byScope[r.Scope], compiledIPRule{
			network:   network,
			expiresAt: r.ExpiresAt,
		})
	}
	return byScope, nil
}

// ruleNetwork turns a stored target into a network. A bare address becomes a
// full-length mask (/32 for IPv4, /128 for IPv6) so exact and CIDR rules share
// one Contains-based match path.
func ruleNetwork(target string) (*net.IPNet, error) {
	if strings.Contains(target, "/") {
		_, network, err := net.ParseCIDR(target)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid cidr %q", ErrInvalidInput, target)
		}
		return network, nil
	}
	ip := net.ParseIP(target)
	if ip == nil {
		return nil, fmt.Errorf("%w: invalid ip %q", ErrInvalidInput, target)
	}
	if v4 := ip.To4(); v4 != nil {
		return &net.IPNet{IP: v4, Mask: net.CIDRMask(32, 32)}, nil
	}
	return &net.IPNet{IP: ip, Mask: net.CIDRMask(128, 128)}, nil
}
