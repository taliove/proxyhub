package store

import (
	"fmt"
	"strings"
	"time"
)

// Trust recommendation candidate enumeration (login hardening ticket 10).
//
// GetTrustRecommendationCount answers "how often did this user pass a real MFA
// challenge from this address", but the management UI has to discover the
// addresses first. This file owns that enumeration; the counting rule stays in
// GetTrustRecommendationCount so both paths agree on what a "real MFA login"
// is.

// ListRecentMFALoginIPs returns the distinct source addresses from which
// username completed a real MFA challenge inside TrustRecommendationWindow
// (30 days), most recent first.
//
// Only login_success rows carrying an "mfa=" marker count: trusted-IP logins
// record "mfa_skipped=trusted_ip" and must not feed the recommendation engine
// back into itself (same predicate as GetTrustRecommendationCount).
//
// Backed by idx_audit_logs_user_ip(username, ip, created_at).
func (s *Store) ListRecentMFALoginIPs(username string) ([]string, error) {
	name := strings.TrimSpace(username)
	if name == "" {
		return nil, fmt.Errorf("%w: username is required", ErrInvalidInput)
	}
	since := time.Now().UTC().Add(-TrustRecommendationWindow).Format(sqliteTimeLayout)

	rows, err := s.db.Query(`
		SELECT ip, MAX(datetime(created_at)) AS last_seen
		FROM audit_logs
		WHERE username = ?
		  AND event_type = 'login_success'
		  AND detail LIKE '%mfa=%'
		  AND datetime(created_at) >= datetime(?)
		GROUP BY ip
		ORDER BY last_seen DESC, ip`,
		name, since)
	if err != nil {
		return nil, fmt.Errorf("list recent mfa login ips: %w", err)
	}
	defer rows.Close()

	out := []string{}
	for rows.Next() {
		var ip, lastSeen string
		if err := rows.Scan(&ip, &lastSeen); err != nil {
			return nil, fmt.Errorf("scan recent mfa login ip: %w", err)
		}
		if ip = strings.TrimSpace(ip); ip != "" {
			out = append(out, ip)
		}
	}
	return out, rows.Err()
}
