package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/taliove/proxyhub/internal/geoip"
	"github.com/taliove/proxyhub/internal/store"
)

// Trusted IP management surface (login hardening ticket 10).
//
// A trust grant is a property of the account that logged in, not of the view a
// super admin happens to be impersonating: revoking "my" trusted IP must never
// silently revoke someone else's. Every /api/me route below therefore keys off
// scope.UserID (the authenticated identity), the same rule handleChangeMyPassword
// follows. The super admin direction is an explicit, audited route
// (/api/admin/users/{id}/trusted-ips/clear).

// autoTrustIPSettingKey is the tenant-level setting that lets the trust
// recommendation engine act on its own conclusion. Default off: turning a
// repeated login into a standing MFA exemption without the user asking is a
// security decision the operator has to opt into.
const autoTrustIPSettingKey = "auto_trust_ip"

// trustRecommendationThreshold is how many real MFA logins from one address
// inside store.TrustRecommendationWindow qualify it as "familiar".
const trustRecommendationThreshold = 3

// trustedIPView is one row of the management table. Geo fields carry the
// offline GeoIP verdict and stay empty when the address has no country record
// (private ranges, reserved space) - the UI renders that as unknown rather than
// failing the whole request.
type trustedIPView struct {
	IP         string    `json:"ip"`
	ExpiresAt  time.Time `json:"expires_at"`
	LastUsedAt time.Time `json:"last_used_at"`
	Expired    bool      `json:"expired"`
	RegionCode string    `json:"region_code"`
	RegionName string    `json:"region_name"`
}

// trustRecommendationView is one candidate address: seen often enough to be
// familiar, but not currently trusted.
type trustRecommendationView struct {
	IP           string `json:"ip"`
	MFASuccesses int    `json:"mfa_successes"`
	RegionCode   string `json:"region_code"`
	RegionName   string `json:"region_name"`
}

// geoForIP resolves an address to (country code, Chinese country name) using
// the embedded offline database. Failures degrade to empty strings: geo is
// decoration on this surface, never a gate.
func (s *Server) geoForIP(ip string) (code, name string) {
	lookup := s.countryLookup
	if lookup == nil {
		lookup = geoip.LookupCountry
	}
	code, err := lookup(ip)
	if err != nil || code == "" {
		return "", ""
	}
	return code, geoip.CountryName(code)
}

// handleListMyTrustedIPs serves GET /api/me/trusted-ips.
//
// Response: {trusted: [...], recommendations: [...], auto_trust_ip: bool}.
// Expired grants are listed (marked expired) so the user can see and clean up
// a lapsed entry instead of wondering why an address vanished.
func (s *Server) handleListMyTrustedIPs(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	user, err := s.st.GetUserByID(scope.UserID)
	if err != nil {
		s.writeMFAUserError(w, scope.UserID, err)
		return
	}

	grants, err := s.st.ListTrustedIPs(user.ID)
	if err != nil {
		s.logger.Error("list trusted ips failed", "user_id", user.ID, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	trusted := make([]trustedIPView, 0, len(grants))
	active := make(map[string]bool, len(grants))
	for _, g := range grants {
		code, name := s.geoForIP(g.IP)
		expired := g.Expired()
		if !expired {
			active[g.IP] = true
		}
		trusted = append(trusted, trustedIPView{
			IP:         g.IP,
			ExpiresAt:  g.ExpiresAt.UTC(),
			LastUsedAt: g.LastUsedAt.UTC(),
			Expired:    expired,
			RegionCode: code,
			RegionName: name,
		})
	}

	recommendations := s.trustRecommendations(user.Username, active)

	writeJSON(w, map[string]any{
		"trusted":         trusted,
		"recommendations": recommendations,
		"auto_trust_ip":   s.autoTrustEnabled(user.ID),
		"threshold":       trustRecommendationThreshold,
	})
}

// trustRecommendations lists addresses that cleared the threshold but are not
// currently trusted. A read failure on one candidate is logged and skipped
// rather than failing the page: a partial recommendation list is strictly more
// useful than none.
func (s *Server) trustRecommendations(username string, active map[string]bool) []trustRecommendationView {
	out := []trustRecommendationView{}
	candidates, err := s.st.ListRecentMFALoginIPs(username)
	if err != nil {
		s.logger.Warn("list recent mfa login ips failed", "username", username, "error", err)
		return out
	}
	for _, ip := range candidates {
		if active[ip] {
			continue
		}
		count, err := s.st.GetTrustRecommendationCount(username, ip)
		if err != nil {
			s.logger.Warn("trust recommendation count failed", "ip", ip, "error", err)
			continue
		}
		if count < trustRecommendationThreshold {
			continue
		}
		code, name := s.geoForIP(ip)
		out = append(out, trustRecommendationView{
			IP:           ip,
			MFASuccesses: count,
			RegionCode:   code,
			RegionName:   name,
		})
	}
	return out
}

// handleTrustMyIP serves POST /api/me/trusted-ips - the "adopt this
// recommendation" action. The body carries {ip}; an absent ip falls back to the
// caller's current address so the UI can offer "trust this device" directly.
//
// Adoption is gated on the same threshold the recommendation engine uses:
// otherwise this endpoint would be a self-service way to permanently skip MFA
// from an arbitrary address, which is exactly the property the second factor
// exists to deny. The caller's own current address is exempt from the gate:
// it already proved possession of the session.
func (s *Server) handleTrustMyIP(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	user, err := s.st.GetUserByID(scope.UserID)
	if err != nil {
		s.writeMFAUserError(w, scope.UserID, err)
		return
	}

	var req struct {
		IP string `json:"ip"`
	}
	if err := decodeOptionalJSON(r, &req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	ip := strings.TrimSpace(req.IP)
	current := clientIP(r)
	if ip == "" {
		ip = current
	}
	if ip == "" {
		http.Error(w, "ip is required", http.StatusBadRequest)
		return
	}

	if ip != current {
		count, cerr := s.st.GetTrustRecommendationCount(user.Username, ip)
		if cerr != nil {
			s.logger.Error("trust recommendation count failed", "ip", ip, "error", cerr)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if count < trustRecommendationThreshold {
			writeJSONStatus(w, http.StatusBadRequest, map[string]any{
				"error":     "ip is not eligible for trust yet",
				"successes": count,
				"threshold": trustRecommendationThreshold,
			})
			return
		}
	}

	if err := s.st.AddTrustedIP(user.ID, ip); err != nil {
		if errors.Is(err, store.ErrInvalidInput) {
			http.Error(w, "invalid ip", http.StatusBadRequest)
			return
		}
		s.logger.Error("add trusted ip failed", "user_id", user.ID, "ip", ip, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.recordAudit("trusted_ip_added", current, user.Username,
		fmt.Sprintf("trusted %s for %d days from recommendation list", ip, trustedIPTTLDays()))
	writeJSON(w, map[string]any{"ok": true, "ip": ip})
}

// handleRevokeMyTrustedIP serves DELETE /api/me/trusted-ips/{ip}. Revoking is
// idempotent (the store treats a missing row as success): the caller's intent
// is "this address must not be trusted", which already holds.
func (s *Server) handleRevokeMyTrustedIP(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	user, err := s.st.GetUserByID(scope.UserID)
	if err != nil {
		s.writeMFAUserError(w, scope.UserID, err)
		return
	}

	ip := strings.TrimSpace(r.PathValue("ip"))
	if ip == "" {
		http.Error(w, "ip is required", http.StatusBadRequest)
		return
	}
	if err := s.st.RevokeTrustedIP(user.ID, ip); err != nil {
		if errors.Is(err, store.ErrInvalidInput) {
			http.Error(w, "invalid ip", http.StatusBadRequest)
			return
		}
		s.logger.Error("revoke trusted ip failed", "user_id", user.ID, "ip", ip, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.recordAudit("trusted_ip_revoked", clientIP(r), user.Username,
		fmt.Sprintf("revoked trust for %s", ip))
	writeJSON(w, map[string]any{"ok": true, "ip": ip})
}

// handleSetMyAutoTrust serves PUT /api/me/trusted-ips/auto - the tenant-level
// auto_trust_ip toggle. Written to user_settings for the authenticated account
// (reads fall back to the global default via GetSettingForUser), matching the
// tenant-setting convention in handleSaveSettings.
func (s *Server) handleSetMyAutoTrust(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	user, err := s.st.GetUserByID(scope.UserID)
	if err != nil {
		s.writeMFAUserError(w, scope.UserID, err)
		return
	}

	var req struct {
		Enabled *bool `json:"enabled"`
	}
	if err := decodeOptionalJSON(r, &req); err != nil || req.Enabled == nil {
		http.Error(w, "enabled is required", http.StatusBadRequest)
		return
	}
	value := "false"
	if *req.Enabled {
		value = "true"
	}
	if err := s.st.SetUserSetting(user.ID, autoTrustIPSettingKey, value); err != nil {
		s.logger.Error("save auto trust setting failed", "user_id", user.ID, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.recordAudit("trusted_ip_auto_toggle", clientIP(r), user.Username,
		fmt.Sprintf("%s=%s", autoTrustIPSettingKey, value))
	writeJSON(w, map[string]any{"ok": true, "auto_trust_ip": *req.Enabled})
}

// handleAdminClearTrustedIPs serves POST /api/admin/users/{id}/trusted-ips/clear
// (super admin only, via adminGuard). Clearing forces the target back through a
// full MFA challenge from every address - the operator escape hatch for a user
// whose device or network is believed compromised.
func (s *Server) handleAdminClearTrustedIPs(w http.ResponseWriter, r *http.Request) {
	id, err := parseAdminUserID(r)
	if err != nil {
		http.Error(w, "invalid user id", http.StatusBadRequest)
		return
	}
	user, err := s.st.GetUserByID(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		s.logger.Error("get user failed", "user_id", id, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	removed, err := s.st.RevokeAllTrustedIPs(id)
	if err != nil {
		s.logger.Error("clear trusted ips failed", "user_id", id, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.recordAudit("trusted_ip_cleared", clientIP(r), user.Username,
		fmt.Sprintf("cleared %d trusted ip(s) for user id=%d by super admin", removed, id))
	writeJSON(w, map[string]any{
		"ok":       true,
		"user_id":  id,
		"username": user.Username,
		"removed":  removed,
	})
}

// autoTrustEnabled reads the auto_trust_ip setting through the tenant fallback
// chain (user override -> global default -> built-in default off). A read error
// is treated as off: failing closed here only costs an extra MFA prompt, while
// failing open would hand out a standing exemption.
func (s *Server) autoTrustEnabled(userID int64) bool {
	val, err := s.st.GetSettingForUser(userID, autoTrustIPSettingKey)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			s.logger.Warn("read auto trust setting failed", "user_id", userID, "error", err)
		}
		return false
	}
	return strings.EqualFold(strings.TrimSpace(val), "true")
}

// maybeAutoTrustLoginIP is the hook the second login stage calls after a
// successful MFA challenge. When auto_trust_ip is on for the account and the
// address has already cleared the recommendation threshold, the grant is
// written without asking. Every failure path is a no-op: the login already
// succeeded, and the worst case is one more challenge next time.
func (s *Server) maybeAutoTrustLoginIP(user *store.User, ip string) {
	if user == nil || strings.TrimSpace(ip) == "" {
		return
	}
	// Loopback is never auto-trusted (CONTEXT.md "受信 IP"): behind an untrusted
	// reverse proxy every request looks like 127.0.0.1, so auto-granting it
	// would hand a standing MFA exemption to everyone sharing that hop. An
	// explicit decision may still trust it.
	if isLoopbackAddr(ip) {
		return
	}
	if !s.autoTrustEnabled(user.ID) {
		return
	}
	// The just-recorded login is not visible here yet (issueLoginSession writes
	// the audit row after this hook), so the count reflects prior logins only:
	// the threshold is reached on the Nth+1 login, which is the conservative
	// direction.
	count, err := s.st.GetTrustRecommendationCount(user.Username, ip)
	if err != nil {
		s.logger.Warn("auto trust count failed", "user_id", user.ID, "ip", ip, "error", err)
		return
	}
	if count < trustRecommendationThreshold {
		return
	}
	if err := s.st.AddTrustedIP(user.ID, ip); err != nil {
		s.logger.Warn("auto trust login ip failed", "user_id", user.ID, "ip", ip, "error", err)
		return
	}
	s.recordAudit("trusted_ip_added", ip, user.Username,
		fmt.Sprintf("auto trusted after %d mfa logins, valid %d days", count, trustedIPTTLDays()))
}

// trustedIPTTLDays renders the grant window in days for audit details.
func trustedIPTTLDays() int {
	return int(store.TrustedIPTTL.Hours() / 24)
}

// isLoopbackAddr reports whether ip is any loopback literal. Broader than
// isLoopbackIP in handlers_captcha.go (which only matches the 127.0.0.1 string):
// the auto-trust decision has to cover ::1 and the rest of 127/8 too, since a
// grant there is standing rather than per-request.
func isLoopbackAddr(ip string) bool {
	parsed := net.ParseIP(strings.TrimSpace(ip))
	return parsed != nil && parsed.IsLoopback()
}

// decodeOptionalJSON decodes an optional JSON body into dst. An empty body is
// not an error (the caller supplies defaults); a malformed one is.
func decodeOptionalJSON(r *http.Request, dst any) error {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}
