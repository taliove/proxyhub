package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Trusted IP lifecycle constants (login hardening, plan decision table).
const (
	// TrustedIPTTL is how long a "trust this IP" grant stays valid.
	TrustedIPTTL = 30 * 24 * time.Hour

	// TrustedIPRenewInterval is the minimum age of last_used_at before a
	// successful trusted login writes to the row. Keeping this above zero
	// turns every login into a read instead of a write.
	TrustedIPRenewInterval = 24 * time.Hour

	// TrustRecommendationWindow is the lookback window used when counting
	// MFA successes for the trust recommendation engine.
	TrustRecommendationWindow = 30 * 24 * time.Hour
)

// sqliteTimeLayout matches the format audit.go writes and parseSQLiteTime
// reads. Timestamps are stored as UTC strings so SQLite's datetime() can
// compare them (the driver's CURRENT_TIMESTAMP writes RFC3339, which
// datetime() cannot parse).
const sqliteTimeLayout = "2006-01-02 15:04:05"

// TrustedIP is one per-user trusted source address grant.
type TrustedIP struct {
	UserID     int64     `json:"user_id"`
	IP         string    `json:"ip"`
	ExpiresAt  time.Time `json:"expires_at"`
	LastUsedAt time.Time `json:"last_used_at"`
}

// Expired reports whether the grant's 30 day window has closed.
func (t *TrustedIP) Expired() bool {
	return !t.ExpiresAt.After(time.Now().UTC())
}

// validateTrustTarget normalizes and checks a (user, ip) pair.
func validateTrustTarget(userID int64, ip string) (string, error) {
	if userID <= 0 {
		return "", fmt.Errorf("%w: user_id is required", ErrInvalidInput)
	}
	trimmed := strings.TrimSpace(ip)
	if trimmed == "" {
		return "", fmt.Errorf("%w: ip is required", ErrInvalidInput)
	}
	return trimmed, nil
}

// AddTrustedIP trusts ip for userID for the next TrustedIPTTL (30 days).
// Re-trusting an already trusted IP resets the window rather than failing.
func (s *Store) AddTrustedIP(userID int64, ip string) error {
	return s.addTrustedIPAt(userID, ip, time.Now().UTC())
}

// addTrustedIPAt is AddTrustedIP with an injectable clock; tests use it to
// seed aged rows without sleeping.
func (s *Store) addTrustedIPAt(userID int64, ip string, now time.Time) error {
	addr, err := validateTrustTarget(userID, ip)
	if err != nil {
		return err
	}
	base := now.UTC()
	_, err = s.db.Exec(`
		INSERT INTO user_trusted_ips (user_id, ip, expires_at, last_used_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(user_id, ip) DO UPDATE SET
			expires_at = excluded.expires_at,
			last_used_at = excluded.last_used_at`,
		userID, addr,
		base.Add(TrustedIPTTL).Format(sqliteTimeLayout),
		base.Format(sqliteTimeLayout),
	)
	if err != nil {
		return fmt.Errorf("add trusted ip: %w", err)
	}
	return nil
}

// IsTrustedIP reports whether ip currently carries an unexpired trust grant
// for userID. Expiry is evaluated in SQL against UTC now.
func (s *Store) IsTrustedIP(userID int64, ip string) (bool, error) {
	addr, err := validateTrustTarget(userID, ip)
	if err != nil {
		return false, err
	}
	var one int
	err = s.db.QueryRow(`
		SELECT 1 FROM user_trusted_ips
		WHERE user_id = ? AND ip = ? AND datetime(expires_at) > datetime(?)`,
		userID, addr, time.Now().UTC().Format(sqliteTimeLayout),
	).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check trusted ip: %w", err)
	}
	return true, nil
}

// ListTrustedIPs returns every trust grant for userID, most recently used
// first. Expired rows are included so the management UI can show and clean
// them up; callers gate access on TrustedIP.Expired or IsTrustedIP.
func (s *Store) ListTrustedIPs(userID int64) ([]*TrustedIP, error) {
	if userID <= 0 {
		return nil, fmt.Errorf("%w: user_id is required", ErrInvalidInput)
	}
	rows, err := s.db.Query(`
		SELECT user_id, ip, expires_at, last_used_at
		FROM user_trusted_ips WHERE user_id = ?
		ORDER BY datetime(last_used_at) DESC, ip`, userID)
	if err != nil {
		return nil, fmt.Errorf("list trusted ips: %w", err)
	}
	defer rows.Close()

	out := []*TrustedIP{}
	for rows.Next() {
		var t TrustedIP
		var expiresAt, lastUsedAt string
		if err := rows.Scan(&t.UserID, &t.IP, &expiresAt, &lastUsedAt); err != nil {
			return nil, fmt.Errorf("scan trusted ip: %w", err)
		}
		t.ExpiresAt = parseSQLiteTime(expiresAt)
		t.LastUsedAt = parseSQLiteTime(lastUsedAt)
		out = append(out, &t)
	}
	return out, rows.Err()
}

// RevokeTrustedIP drops a single trust grant. Absent rows are not an error:
// the caller's intent ("this IP must not be trusted") already holds.
func (s *Store) RevokeTrustedIP(userID int64, ip string) error {
	addr, err := validateTrustTarget(userID, ip)
	if err != nil {
		return err
	}
	if _, err := s.db.Exec(
		`DELETE FROM user_trusted_ips WHERE user_id = ? AND ip = ?`, userID, addr,
	); err != nil {
		return fmt.Errorf("revoke trusted ip: %w", err)
	}
	return nil
}

// RevokeAllTrustedIPs clears every trust grant for userID and reports how many
// rows were removed. Used by the super admin "clear trusted IPs" action.
func (s *Store) RevokeAllTrustedIPs(userID int64) (int, error) {
	if userID <= 0 {
		return 0, fmt.Errorf("%w: user_id is required", ErrInvalidInput)
	}
	res, err := s.db.Exec(`DELETE FROM user_trusted_ips WHERE user_id = ?`, userID)
	if err != nil {
		return 0, fmt.Errorf("revoke all trusted ips: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("rows affected: %w", err)
	}
	return int(n), nil
}

// TouchTrustedIP renews an active grant after a trusted-IP login, sliding the
// 30 day window forward. To keep the write rate low it only touches the row
// when last_used_at is older than TrustedIPRenewInterval (24h); within that
// interval it is a pure read and reports false.
//
// Expired and missing grants are left alone: a touch must never resurrect
// trust that has already lapsed.
func (s *Store) TouchTrustedIP(userID int64, ip string) (bool, error) {
	addr, err := validateTrustTarget(userID, ip)
	if err != nil {
		return false, err
	}
	now := time.Now().UTC()
	nowStr := now.Format(sqliteTimeLayout)
	res, err := s.db.Exec(`
		UPDATE user_trusted_ips
		SET last_used_at = ?, expires_at = ?
		WHERE user_id = ? AND ip = ?
		  AND datetime(expires_at) > datetime(?)
		  AND datetime(last_used_at) <= datetime(?)`,
		nowStr, now.Add(TrustedIPTTL).Format(sqliteTimeLayout),
		userID, addr, nowStr,
		now.Add(-TrustedIPRenewInterval).Format(sqliteTimeLayout),
	)
	if err != nil {
		return false, fmt.Errorf("touch trusted ip: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("rows affected: %w", err)
	}
	return n > 0, nil
}

// GetTrustRecommendationCount counts how many times username logged in
// successfully from ip with a real MFA challenge inside the last 30 days.
// Only login_success rows whose detail carries an "mfa=" marker count:
// trusted-IP logins record "mfa_skipped=trusted_ip" and must not feed the
// recommendation back into itself.
//
// Backed by idx_audit_logs_user_ip(username, ip, created_at).
func (s *Store) GetTrustRecommendationCount(username, ip string) (int, error) {
	name := strings.TrimSpace(username)
	if name == "" {
		return 0, fmt.Errorf("%w: username is required", ErrInvalidInput)
	}
	addr := strings.TrimSpace(ip)
	if addr == "" {
		return 0, fmt.Errorf("%w: ip is required", ErrInvalidInput)
	}
	since := time.Now().UTC().Add(-TrustRecommendationWindow).Format(sqliteTimeLayout)

	var count int
	err := s.db.QueryRow(`
		SELECT COUNT(*) FROM audit_logs
		WHERE username = ? AND ip = ?
		  AND event_type = 'login_success'
		  AND detail LIKE '%mfa=%'
		  AND datetime(created_at) >= datetime(?)`,
		name, addr, since,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count trust recommendation: %w", err)
	}
	return count, nil
}
