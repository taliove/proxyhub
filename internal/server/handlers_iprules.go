package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/taliove/proxyhub/internal/store"
)

// IP access rule management surface (pull guard ticket 02).
//
// Super admin only: a rule here can 404 the whole site for a source address
// (scope=global) or cut off subscription pulls (scope=sub), so it belongs behind
// adminGuard alongside the ban list, not on any per-user surface.
//
// Every write records an audit event with a Chinese detail and the caller's user
// agent, matching the batch-one audit convention (see handleBanIP).

// ipRulePermanentDuration is the request value that means "never expires".
const ipRulePermanentDuration = "permanent"

// ipRuleView is one row of the management table. ExpiresAt is nil for a
// permanent rule; Expired lets the UI grey out a lapsed entry instead of
// implying it is still enforced.
type ipRuleView struct {
	ID        int64      `json:"id"`
	IPOrCIDR  string     `json:"ip_or_cidr"`
	Scope     string     `json:"scope"`
	Source    string     `json:"source"`
	ExpiresAt *time.Time `json:"expires_at"`
	Expired   bool       `json:"expired"`
	Permanent bool       `json:"permanent"`
	Comment   string     `json:"comment"`
	CreatedAt time.Time  `json:"created_at"`
}

// newIPRuleView projects a store rule onto the wire shape.
func newIPRuleView(rule *store.IPAccessRule) ipRuleView {
	return ipRuleView{
		ID:        rule.ID,
		IPOrCIDR:  rule.IPOrCIDR,
		Scope:     rule.Scope,
		Source:    rule.Source,
		ExpiresAt: rule.ExpiresAt,
		Expired:   rule.Expired(),
		Permanent: rule.Permanent(),
		Comment:   rule.Comment,
		CreatedAt: rule.CreatedAt,
	}
}

// handleListIPRules serves GET /api/admin/ip-rules.
//
// Optional ?scope=global|sub filters the list. Expired rules are included and
// flagged so an operator can see why access came back and clean up the row.
func (s *Server) handleListIPRules(w http.ResponseWriter, r *http.Request) {
	scope := strings.TrimSpace(r.URL.Query().Get("scope"))
	rules, err := s.st.ListIPAccessRules(scope)
	if err != nil {
		if errors.Is(err, store.ErrInvalidInput) {
			writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "invalid scope"})
			return
		}
		s.logger.Error("list ip rules failed", "scope", scope, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	views := make([]ipRuleView, 0, len(rules))
	for _, rule := range rules {
		views = append(views, newIPRuleView(rule))
	}
	writeJSON(w, map[string]any{"rules": views})
}

// handleCreateIPRule serves POST /api/admin/ip-rules.
//
// Body: {ip_or_cidr, scope, duration|permanent, comment}. duration accepts a
// Go duration string ("24h"); "permanent": true (or duration "permanent") means
// no expiry. Re-adding an existing (target, scope) pair slides its window
// instead of failing, so the operator can extend a ban by re-submitting it.
func (s *Server) handleCreateIPRule(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IPOrCIDR  string `json:"ip_or_cidr"`
		Scope     string `json:"scope"`
		Duration  string `json:"duration"`
		Permanent bool   `json:"permanent"`
		Comment   string `json:"comment"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if strings.TrimSpace(req.IPOrCIDR) == "" {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "ip_or_cidr is required"})
		return
	}

	ttl, windowLabel, err := parseIPRuleWindow(req.Duration, req.Permanent)
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	rule, err := s.st.AddIPAccessRule(req.IPOrCIDR, req.Scope, store.IPRuleSourceManual, req.Comment, ttl)
	if err != nil {
		if errors.Is(err, store.ErrInvalidInput) {
			writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		s.logger.Error("add ip rule failed", "target", req.IPOrCIDR, "scope", req.Scope, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	s.recordAudit("ip_rule_added", clientIP(r), "",
		fmt.Sprintf("新增%s规则 %s，%s%s",
			ipRuleScopeLabel(rule.Scope), rule.IPOrCIDR, windowLabel, ipRuleCommentSuffix(rule.Comment)),
		r.UserAgent())

	writeJSON(w, newIPRuleView(rule))
}

// handleDeleteIPRule serves DELETE /api/admin/ip-rules/{id}. Deleting a rule
// restores access on the next request (the match cache is invalidated on write).
func (s *Server) handleDeleteIPRule(w http.ResponseWriter, r *http.Request) {
	id, ok := s.ipRuleIDFromPath(w, r)
	if !ok {
		return
	}
	// Read before deleting: the audit detail needs the target, and the row is
	// gone afterwards.
	rule, err := s.st.GetIPAccessRule(id)
	if err != nil {
		s.writeIPRuleLookupError(w, id, err)
		return
	}
	if err := s.st.DeleteIPAccessRule(id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeJSONStatus(w, http.StatusNotFound, map[string]string{"error": "rule not found"})
			return
		}
		s.logger.Error("delete ip rule failed", "id", id, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	s.recordAudit("ip_rule_deleted", clientIP(r), "",
		fmt.Sprintf("删除%s规则 %s", ipRuleScopeLabel(rule.Scope), rule.IPOrCIDR),
		r.UserAgent())

	writeJSON(w, map[string]bool{"success": true})
}

// handlePromoteIPRule serves POST /api/admin/ip-rules/{id}/promote: lift a pull
// blacklist entry (scope=sub) to a site-wide block (scope=global). Only sub
// rules can be promoted; anything else is a 400 rather than a silent no-op.
func (s *Server) handlePromoteIPRule(w http.ResponseWriter, r *http.Request) {
	id, ok := s.ipRuleIDFromPath(w, r)
	if !ok {
		return
	}
	rule, err := s.st.PromoteIPAccessRule(id)
	if err != nil {
		s.writeIPRuleLookupError(w, id, err)
		return
	}

	s.recordAudit("ip_rule_promoted", clientIP(r), "",
		fmt.Sprintf("规则 %s 由拉取黑名单升级为整站拒止%s",
			rule.IPOrCIDR, ipRuleCommentSuffix(rule.Comment)),
		r.UserAgent())

	writeJSON(w, newIPRuleView(rule))
}

// ipRuleIDFromPath reads the {id} path value, answering 400 on a bad value.
func (s *Server) ipRuleIDFromPath(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "invalid rule id"})
		return 0, false
	}
	return id, true
}

// writeIPRuleLookupError maps a store error onto the HTTP surface: a missing
// row is 404, bad input is 400, everything else is a logged 500.
func (s *Server) writeIPRuleLookupError(w http.ResponseWriter, id int64, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeJSONStatus(w, http.StatusNotFound, map[string]string{"error": "rule not found"})
	case errors.Is(err, store.ErrInvalidInput):
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	default:
		s.logger.Error("ip rule operation failed", "id", id, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// parseIPRuleWindow turns the request's duration/permanent pair into a TTL plus
// a Chinese label for the audit detail. A zero TTL means permanent.
func parseIPRuleWindow(duration string, permanent bool) (time.Duration, string, error) {
	trimmed := strings.TrimSpace(duration)
	if permanent || trimmed == ipRulePermanentDuration || trimmed == "" {
		// An absent duration is only permanent when that is what the caller asked
		// for; an empty body with permanent=false is ambiguous, so require one.
		if !permanent && trimmed == "" {
			return 0, "", errors.New("duration or permanent is required")
		}
		return 0, "永久生效", nil
	}
	d, err := time.ParseDuration(trimmed)
	if err != nil {
		return 0, "", errors.New("invalid duration format")
	}
	if d <= 0 {
		return 0, "", errors.New("duration must be positive")
	}
	return d, "有效期 " + trimmed, nil
}

// ipRuleScopeLabel renders a scope for audit details.
func ipRuleScopeLabel(scope string) string {
	if scope == store.IPRuleScopeGlobal {
		return "整站拒止"
	}
	return "拉取黑名单"
}

// ipRuleCommentSuffix appends the operator's note to an audit detail when there
// is one.
func ipRuleCommentSuffix(comment string) string {
	if strings.TrimSpace(comment) == "" {
		return ""
	}
	return "，备注：" + comment
}
