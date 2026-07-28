package store

import (
	"fmt"
	"strings"
	"time"
)

// Auto escalation support (pull-guard ticket 05).
//
// The blacklist guard escalates a repeatedly rate-limited source into a
// scope=sub rule. Deciding that needs one number: how many rows of a given
// status one IP produced inside a recent window. That count is read here.

// migratePullLogEscalationIndex adds the index the escalation count needs.
// Idempotent: safe on every startup.
//
// The count runs on the /sub path, on exactly the requests an abusive client
// controls, so it must never degrade into a scan of a table that grows with
// every pull. (ip, status, pulled_at) is the full predicate of the query in
// leading-column order, which makes it an index search with the time range
// resolved inside the index. idx_pull_logs_ip alone would still visit every row
// that IP ever produced.
//
// Must run after migratePullLogStatus has created the status column.
func (s *Store) migratePullLogEscalationIndex() error {
	if _, err := s.db.Exec(
		`CREATE INDEX IF NOT EXISTS idx_pull_logs_ip_status_time
		 ON pull_logs(ip, status, pulled_at)`,
	); err != nil {
		return fmt.Errorf("create idx_pull_logs_ip_status_time: %w", err)
	}
	return nil
}

// CountRecentPullStatus counts pull_logs rows for ip with the given status
// inside the last window. It is scoped across every subscription address on
// purpose: the escalation chain judges a source, not a single address, so a
// client spreading its hammering over several addresses must not dodge it.
//
// Bad input is an error rather than a zero count: a caller passing an unknown
// status or a zero window has a bug, and answering 0 would silently disable the
// escalation chain instead of surfacing it.
func (s *Store) CountRecentPullStatus(ip, status string, window time.Duration) (int, error) {
	return s.countRecentPullStatusAt(ip, status, window, time.Now().UTC())
}

// countRecentPullStatusAt is CountRecentPullStatus with an injectable clock so
// tests can drive window boundaries without sleeping.
func (s *Store) countRecentPullStatusAt(ip, status string, window time.Duration, now time.Time) (int, error) {
	addr := strings.TrimSpace(ip)
	if addr == "" {
		return 0, fmt.Errorf("%w: ip is required", ErrInvalidInput)
	}
	if !IsValidPullStatus(status) {
		return 0, fmt.Errorf("%w: unknown pull status %q", ErrInvalidInput, status)
	}
	if window <= 0 {
		return 0, fmt.Errorf("%w: window must be positive", ErrInvalidInput)
	}

	// Exclusive boundary, matching the rate limiter's window: a row exactly one
	// window old has left it.
	since := now.UTC().Add(-window).Format(sqliteTimeLayout)
	var count int
	if err := s.db.QueryRow(`
		SELECT COUNT(*) FROM pull_logs
		WHERE ip = ? AND status = ? AND datetime(pulled_at) > datetime(?)`,
		addr, status, since,
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("count recent pull status: %w", err)
	}
	return count, nil
}
