package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/taliove/proxyhub/internal/store"
)

// IP access rule management surface (pull guard ticket 02).
//
// Ticket 02 ships the handlers but not the route table entry (server.go is being
// edited in parallel), so these tests mount the same adminGuard chain the
// registration lines will use. Keep this mux in sync with the lines reported by
// the ticket: requireAuth + requirePasswordChanged + requireMFAEnrolled +
// requireAdmin, exactly as adminGuard composes them in Handler().

// ipRuleTestHandler mounts the IP rule routes behind the production admin chain.
func ipRuleTestHandler(srv *Server) http.Handler {
	adminGuard := func(h http.HandlerFunc) http.HandlerFunc {
		return srv.requireAuth(srv.requirePasswordChanged(srv.requireMFAEnrolled(srv.requireAdmin(h))))
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/admin/ip-rules", adminGuard(srv.handleListIPRules))
	mux.HandleFunc("POST /api/admin/ip-rules", adminGuard(srv.handleCreateIPRule))
	mux.HandleFunc("DELETE /api/admin/ip-rules/{id}", adminGuard(srv.handleDeleteIPRule))
	mux.HandleFunc("POST /api/admin/ip-rules/{id}/promote", adminGuard(srv.handlePromoteIPRule))
	return mux
}

// serveIPRule issues a request through the guarded mux from a fixed source.
func serveIPRule(t *testing.T, srv *Server, cookie *http.Cookie, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		req = httptest.NewRequest(method, path, bytes.NewReader(buf))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	req.RemoteAddr = "9.9.9.60:4000"
	req.Header.Set("User-Agent", "IPRuleTest/1.0")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	ipRuleTestHandler(srv).ServeHTTP(rec, req)
	return rec
}

// ipRuleListResponse mirrors the GET envelope.
type ipRuleListResponse struct {
	Rules []struct {
		ID        int64      `json:"id"`
		IPOrCIDR  string     `json:"ip_or_cidr"`
		Scope     string     `json:"scope"`
		Source    string     `json:"source"`
		ExpiresAt *time.Time `json:"expires_at"`
		Expired   bool       `json:"expired"`
		Permanent bool       `json:"permanent"`
		Comment   string     `json:"comment"`
		CreatedAt time.Time  `json:"created_at"`
	} `json:"rules"`
}

// newIPRuleServer returns a server whose store carries the rule schema plus an
// admin session cookie.
func newIPRuleServer(t *testing.T) (*Server, *store.Store, *http.Cookie) {
	t.Helper()
	srv, st := newTestServer(t, nil)
	if err := st.EnsureIPAccessRulesSchema(); err != nil {
		t.Fatalf("EnsureIPAccessRulesSchema: %v", err)
	}
	adminID := seedSuperAdmin(t, st)
	return srv, st, adminSession(t, srv, adminID)
}

// decodeIPRuleList reads the list envelope, failing on a non-200.
func decodeIPRuleList(t *testing.T, rec *httptest.ResponseRecorder) ipRuleListResponse {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	var resp ipRuleListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode ip rules: %v (body: %s)", err, rec.Body.String())
	}
	return resp
}

// auditEventDetail returns the newest audit detail for an event type, or "".
func auditEventDetail(t *testing.T, st *store.Store, eventType string) (detail, userAgent string) {
	t.Helper()
	events, _, err := st.ListAuditEvents(store.AuditFilter{EventTypes: []string{eventType}}, 10, 0)
	if err != nil {
		t.Fatalf("ListAuditEvents(%s): %v", eventType, err)
	}
	if len(events) == 0 {
		return "", ""
	}
	return events[0].Detail, events[0].UserAgent
}

// TestIPRules_CreatePermanentGlobalRuleAndList covers the happy path: a
// permanent global rule is created, listed with its flags, and audited in
// Chinese with the caller's user agent.
func TestIPRules_CreatePermanentGlobalRuleAndList(t *testing.T) {
	srv, st, cookie := newIPRuleServer(t)

	rec := serveIPRule(t, srv, cookie, http.MethodPost, "/api/admin/ip-rules", map[string]any{
		"ip_or_cidr": "203.0.113.7",
		"scope":      store.IPRuleScopeGlobal,
		"permanent":  true,
		"comment":    "扫描器",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("create status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}

	resp := decodeIPRuleList(t, serveIPRule(t, srv, cookie, http.MethodGet, "/api/admin/ip-rules", nil))
	if len(resp.Rules) != 1 {
		t.Fatalf("rules len = %d, want 1", len(resp.Rules))
	}
	row := resp.Rules[0]
	if row.IPOrCIDR != "203.0.113.7" || row.Scope != store.IPRuleScopeGlobal {
		t.Errorf("row = %+v, want the global rule for 203.0.113.7", row)
	}
	if row.Source != store.IPRuleSourceManual {
		t.Errorf("source = %q, want manual for an operator-created rule", row.Source)
	}
	if !row.Permanent || row.ExpiresAt != nil || row.Expired {
		t.Errorf("expiry flags = (permanent=%v expires=%v expired=%v), want a permanent rule",
			row.Permanent, row.ExpiresAt, row.Expired)
	}
	if row.Comment != "扫描器" || row.CreatedAt.IsZero() {
		t.Errorf("comment/created_at not returned: %+v", row)
	}

	// The rule is immediately enforceable through the matching engine.
	denied, err := st.IsDenied("203.0.113.7", store.IPRuleScopeGlobal)
	if err != nil {
		t.Fatalf("IsDenied: %v", err)
	}
	if !denied {
		t.Error("IsDenied = false right after the rule was created")
	}

	detail, ua := auditEventDetail(t, st, "ip_rule_added")
	if detail == "" {
		t.Fatal("ip_rule_added audit event not recorded")
	}
	for _, want := range []string{"整站拒止", "203.0.113.7", "永久生效", "扫描器"} {
		if !bytes.Contains([]byte(detail), []byte(want)) {
			t.Errorf("audit detail %q missing %q", detail, want)
		}
	}
	if ua != "IPRuleTest/1.0" {
		t.Errorf("audit user_agent = %q, want IPRuleTest/1.0", ua)
	}
}

// TestIPRules_CreateSubRuleWithDuration covers the timed sub rule: expiry is
// returned and the audit detail names the window.
func TestIPRules_CreateSubRuleWithDuration(t *testing.T) {
	srv, st, cookie := newIPRuleServer(t)

	rec := serveIPRule(t, srv, cookie, http.MethodPost, "/api/admin/ip-rules", map[string]any{
		"ip_or_cidr": "198.51.100.0/24",
		"scope":      store.IPRuleScopeSub,
		"duration":   "24h",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("create status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	var created ipRuleView
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created rule: %v", err)
	}
	if created.ExpiresAt == nil || created.Permanent {
		t.Errorf("created = %+v, want a rule with an expiry", created)
	}
	if created.Expired {
		t.Error("a 24h rule is already expired")
	}
	if created.Scope != store.IPRuleScopeSub {
		t.Errorf("scope = %q, want sub", created.Scope)
	}

	detail, _ := auditEventDetail(t, st, "ip_rule_added")
	for _, want := range []string{"拉取黑名单", "198.51.100.0/24", "有效期 24h"} {
		if !bytes.Contains([]byte(detail), []byte(want)) {
			t.Errorf("audit detail %q missing %q", detail, want)
		}
	}
}

// TestIPRules_DeleteRestoresAccess verifies deletion returns 200, drops the row
// and makes the source pass the matching engine again.
func TestIPRules_DeleteRestoresAccess(t *testing.T) {
	srv, st, cookie := newIPRuleServer(t)
	rule, err := st.AddIPAccessRule("203.0.113.7", store.IPRuleScopeGlobal, store.IPRuleSourceManual, "临时", 0)
	if err != nil {
		t.Fatalf("AddIPAccessRule: %v", err)
	}
	if denied, _ := st.IsDenied("203.0.113.7", store.IPRuleScopeGlobal); !denied {
		t.Fatal("precondition: rule does not deny")
	}

	rec := serveIPRule(t, srv, cookie, http.MethodDelete, ipRulePath(rule.ID, ""), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}

	denied, err := st.IsDenied("203.0.113.7", store.IPRuleScopeGlobal)
	if err != nil {
		t.Fatalf("IsDenied: %v", err)
	}
	if denied {
		t.Error("IsDenied = true after the rule was deleted (stale cache)")
	}

	resp := decodeIPRuleList(t, serveIPRule(t, srv, cookie, http.MethodGet, "/api/admin/ip-rules", nil))
	if len(resp.Rules) != 0 {
		t.Errorf("rules len = %d after delete, want 0", len(resp.Rules))
	}

	detail, ua := auditEventDetail(t, st, "ip_rule_deleted")
	for _, want := range []string{"删除", "整站拒止", "203.0.113.7"} {
		if !bytes.Contains([]byte(detail), []byte(want)) {
			t.Errorf("audit detail %q missing %q", detail, want)
		}
	}
	if ua != "IPRuleTest/1.0" {
		t.Errorf("audit user_agent = %q, want IPRuleTest/1.0", ua)
	}
}

// TestIPRules_PromoteSubToGlobal verifies the upgrade endpoint moves a pull
// blacklist entry into the site-wide scope and audits it.
func TestIPRules_PromoteSubToGlobal(t *testing.T) {
	srv, st, cookie := newIPRuleServer(t)
	rule, err := st.AddIPAccessRule("203.0.113.7", store.IPRuleScopeSub, store.IPRuleSourceAuto, "自动封禁", time.Hour)
	if err != nil {
		t.Fatalf("AddIPAccessRule: %v", err)
	}

	rec := serveIPRule(t, srv, cookie, http.MethodPost, ipRulePath(rule.ID, "/promote"), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("promote status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	var promoted ipRuleView
	if err := json.Unmarshal(rec.Body.Bytes(), &promoted); err != nil {
		t.Fatalf("decode promoted rule: %v", err)
	}
	if promoted.Scope != store.IPRuleScopeGlobal || promoted.ID != rule.ID {
		t.Errorf("promoted = %+v, want id %d in the global scope", promoted, rule.ID)
	}

	globalDenied, err := st.IsDenied("203.0.113.7", store.IPRuleScopeGlobal)
	if err != nil {
		t.Fatalf("IsDenied(global): %v", err)
	}
	if !globalDenied {
		t.Error("promotion did not take effect in the global scope")
	}

	detail, _ := auditEventDetail(t, st, "ip_rule_promoted")
	for _, want := range []string{"203.0.113.7", "升级", "整站拒止"} {
		if !bytes.Contains([]byte(detail), []byte(want)) {
			t.Errorf("audit detail %q missing %q", detail, want)
		}
	}
}

// TestIPRules_PromoteGlobalRejected verifies promoting an already-global rule is
// a 400 (nothing sits above global).
func TestIPRules_PromoteGlobalRejected(t *testing.T) {
	srv, st, cookie := newIPRuleServer(t)
	rule, err := st.AddIPAccessRule("203.0.113.7", store.IPRuleScopeGlobal, store.IPRuleSourceManual, "", 0)
	if err != nil {
		t.Fatalf("AddIPAccessRule: %v", err)
	}

	rec := serveIPRule(t, srv, cookie, http.MethodPost, ipRulePath(rule.ID, "/promote"), nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("promote(global) status = %d, want 400 (body: %s)", rec.Code, rec.Body.String())
	}
}

// TestIPRules_MissingRuleIsNotFound verifies delete and promote answer 404 for a
// row that is not there.
func TestIPRules_MissingRuleIsNotFound(t *testing.T) {
	srv, _, cookie := newIPRuleServer(t)

	if rec := serveIPRule(t, srv, cookie, http.MethodDelete, ipRulePath(999999, ""), nil); rec.Code != http.StatusNotFound {
		t.Errorf("delete(missing) status = %d, want 404", rec.Code)
	}
	if rec := serveIPRule(t, srv, cookie, http.MethodPost, ipRulePath(999999, "/promote"), nil); rec.Code != http.StatusNotFound {
		t.Errorf("promote(missing) status = %d, want 404", rec.Code)
	}
	if rec := serveIPRule(t, srv, cookie, http.MethodDelete, "/api/admin/ip-rules/abc", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("delete(bad id) status = %d, want 400", rec.Code)
	}
}

// TestIPRules_CreateValidatesInput verifies malformed targets, unknown scopes and
// missing windows are rejected with 400 and leave no rule behind.
func TestIPRules_CreateValidatesInput(t *testing.T) {
	srv, st, cookie := newIPRuleServer(t)

	cases := []struct {
		name string
		body map[string]any
	}{
		{"empty target", map[string]any{"ip_or_cidr": "", "scope": store.IPRuleScopeSub, "permanent": true}},
		{"bad ip", map[string]any{"ip_or_cidr": "nope", "scope": store.IPRuleScopeSub, "permanent": true}},
		{"bad cidr", map[string]any{"ip_or_cidr": "203.0.113.0/33", "scope": store.IPRuleScopeSub, "permanent": true}},
		{"bad scope", map[string]any{"ip_or_cidr": "203.0.113.7", "scope": "everywhere", "permanent": true}},
		{"no window", map[string]any{"ip_or_cidr": "203.0.113.7", "scope": store.IPRuleScopeSub}},
		{"bad duration", map[string]any{"ip_or_cidr": "203.0.113.7", "scope": store.IPRuleScopeSub, "duration": "soon"}},
		{"zero duration", map[string]any{"ip_or_cidr": "203.0.113.7", "scope": store.IPRuleScopeSub, "duration": "0s"}},
	}
	for _, c := range cases {
		rec := serveIPRule(t, srv, cookie, http.MethodPost, "/api/admin/ip-rules", c.body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400 (body: %s)", c.name, rec.Code, rec.Body.String())
		}
	}

	rules, err := st.ListIPAccessRules("")
	if err != nil {
		t.Fatalf("ListIPAccessRules: %v", err)
	}
	if len(rules) != 0 {
		t.Errorf("rules len = %d after rejected creates, want 0", len(rules))
	}
}

// TestIPRules_ListScopeFilter verifies ?scope= narrows the list and a bogus scope
// is a 400.
func TestIPRules_ListScopeFilter(t *testing.T) {
	srv, st, cookie := newIPRuleServer(t)
	if _, err := st.AddIPAccessRule("203.0.113.7", store.IPRuleScopeGlobal, store.IPRuleSourceManual, "", 0); err != nil {
		t.Fatalf("AddIPAccessRule(global): %v", err)
	}
	if _, err := st.AddIPAccessRule("198.51.100.9", store.IPRuleScopeSub, store.IPRuleSourceAuto, "", time.Hour); err != nil {
		t.Fatalf("AddIPAccessRule(sub): %v", err)
	}

	subOnly := decodeIPRuleList(t, serveIPRule(t, srv, cookie, http.MethodGet, "/api/admin/ip-rules?scope=sub", nil))
	if len(subOnly.Rules) != 1 || subOnly.Rules[0].IPOrCIDR != "198.51.100.9" {
		t.Errorf("sub-filtered rules = %+v, want only 198.51.100.9", subOnly.Rules)
	}
	all := decodeIPRuleList(t, serveIPRule(t, srv, cookie, http.MethodGet, "/api/admin/ip-rules", nil))
	if len(all.Rules) != 2 {
		t.Errorf("unfiltered rules len = %d, want 2", len(all.Rules))
	}
	if rec := serveIPRule(t, srv, cookie, http.MethodGet, "/api/admin/ip-rules?scope=elsewhere", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("bogus scope status = %d, want 400", rec.Code)
	}
}

// TestIPRules_ExpiredRuleListedButNotEnforced verifies a lapsed rule is visible
// with expired=true while no longer denying.
func TestIPRules_ExpiredRuleListedButNotEnforced(t *testing.T) {
	srv, st, cookie := newIPRuleServer(t)
	// Sub-second TTL: expires_at is stored at second precision, so it lands on
	// (or before) the current second and is already past by the time it is read.
	if _, err := st.AddIPAccessRule("203.0.113.7", store.IPRuleScopeSub, store.IPRuleSourceAuto, "", time.Nanosecond); err != nil {
		t.Fatalf("AddIPAccessRule: %v", err)
	}

	resp := decodeIPRuleList(t, serveIPRule(t, srv, cookie, http.MethodGet, "/api/admin/ip-rules", nil))
	if len(resp.Rules) != 1 {
		t.Fatalf("rules len = %d, want the lapsed rule to stay listed", len(resp.Rules))
	}
	if !resp.Rules[0].Expired {
		t.Error("expired = false for a rule whose window has closed")
	}
	denied, err := st.IsDenied("203.0.113.7", store.IPRuleScopeSub)
	if err != nil {
		t.Fatalf("IsDenied: %v", err)
	}
	if denied {
		t.Error("an expired rule still denies")
	}
}

// TestIPRules_RequireSuperAdmin verifies the whole surface is admin-only:
// an ordinary member gets 403, an anonymous caller 401.
func TestIPRules_RequireSuperAdmin(t *testing.T) {
	srv, st, adminCookie := newIPRuleServer(t)
	rule, err := st.AddIPAccessRule("203.0.113.7", store.IPRuleScopeSub, store.IPRuleSourceManual, "", 0)
	if err != nil {
		t.Fatalf("AddIPAccessRule: %v", err)
	}
	memberCookie := memberSession(t, srv, seedRegularUser(t, st, "alice", "init-pass-1"))

	requests := []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodGet, "/api/admin/ip-rules", nil},
		{http.MethodPost, "/api/admin/ip-rules", map[string]any{
			"ip_or_cidr": "198.51.100.9", "scope": store.IPRuleScopeSub, "permanent": true,
		}},
		{http.MethodDelete, ipRulePath(rule.ID, ""), nil},
		{http.MethodPost, ipRulePath(rule.ID, "/promote"), nil},
	}
	for _, req := range requests {
		if rec := serveIPRule(t, srv, memberCookie, req.method, req.path, req.body); rec.Code != http.StatusForbidden {
			t.Errorf("%s %s as member: status = %d, want 403", req.method, req.path, rec.Code)
		}
		if rec := serveIPRule(t, srv, nil, req.method, req.path, req.body); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s anonymous: status = %d, want 401", req.method, req.path, rec.Code)
		}
	}

	// The rejected writes changed nothing.
	rules, err := st.ListIPAccessRules("")
	if err != nil {
		t.Fatalf("ListIPAccessRules: %v", err)
	}
	if len(rules) != 1 || rules[0].ID != rule.ID || rules[0].Scope != store.IPRuleScopeSub {
		t.Errorf("rules = %+v, want the seeded sub rule untouched", rules)
	}
	if rec := serveIPRule(t, srv, adminCookie, http.MethodGet, "/api/admin/ip-rules", nil); rec.Code != http.StatusOK {
		t.Errorf("admin list status = %d, want 200", rec.Code)
	}
}

// ipRulePath builds /api/admin/ip-rules/{id}{suffix}.
func ipRulePath(id int64, suffix string) string {
	return "/api/admin/ip-rules/" + strconv.FormatInt(id, 10) + suffix
}
