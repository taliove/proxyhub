package store

import (
	"strings"
	"testing"
	"time"
)

// Auto escalation counting (pull-guard ticket 05).

// seedPullLog inserts one pull_logs row with an explicit timestamp so window
// boundaries can be tested without sleeping.
func seedPullLog(t *testing.T, s *Store, ip, status string, at time.Time) {
	t.Helper()
	if _, err := s.db.Exec(
		`INSERT INTO pull_logs (endpoint_id, ip, user_agent, pulled_at, status)
		 VALUES (?, ?, '', ?, ?)`,
		1, ip, at.UTC().Format(sqliteTimeLayout), status,
	); err != nil {
		t.Fatalf("seed pull log: %v", err)
	}
}

// TestCountRecentPullStatus_FiltersIPStatusAndWindow the count that drives the
// escalation chain must be scoped on all three axes: one IP, one status, one
// time window. Anything wider would ban an innocent address.
func TestCountRecentPullStatus_FiltersIPStatusAndWindow(t *testing.T) {
	s := newTestStore(t)
	now := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)

	// In scope: 3 rate_limited rows for the target inside the window.
	for i := 1; i <= 3; i++ {
		seedPullLog(t, s, "203.0.113.9", PullStatusRateLimited, now.Add(-time.Duration(i)*time.Minute))
	}
	// Out of scope: right status and IP but outside the window.
	seedPullLog(t, s, "203.0.113.9", PullStatusRateLimited, now.Add(-2*time.Hour))
	// Out of scope: right IP and window, different status.
	seedPullLog(t, s, "203.0.113.9", PullStatusOK, now.Add(-time.Minute))
	// Out of scope: right status and window, different IP.
	seedPullLog(t, s, "203.0.113.10", PullStatusRateLimited, now.Add(-time.Minute))

	got, err := s.countRecentPullStatusAt("203.0.113.9", PullStatusRateLimited, time.Hour, now)
	if err != nil {
		t.Fatalf("countRecentPullStatusAt: %v", err)
	}
	if got != 3 {
		t.Errorf("count = %d, want 3 (IP + status + window scoped)", got)
	}
}

// TestCountRecentPullStatus_WindowBoundary a row exactly one window old has
// left the window, matching the rate limiter's exclusive boundary.
func TestCountRecentPullStatus_WindowBoundary(t *testing.T) {
	s := newTestStore(t)
	now := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)

	seedPullLog(t, s, "203.0.113.11", PullStatusRateLimited, now.Add(-time.Hour))
	seedPullLog(t, s, "203.0.113.11", PullStatusRateLimited, now.Add(-time.Hour+time.Second))

	got, err := s.countRecentPullStatusAt("203.0.113.11", PullStatusRateLimited, time.Hour, now)
	if err != nil {
		t.Fatalf("countRecentPullStatusAt: %v", err)
	}
	if got != 1 {
		t.Errorf("count = %d, want 1 (the row exactly one window old has left it)", got)
	}
}

// TestCountRecentPullStatus_RejectsBadInput an empty IP, unknown status or
// non-positive window is a programming error, not a zero count: returning 0
// would silently disable the escalation chain.
func TestCountRecentPullStatus_RejectsBadInput(t *testing.T) {
	s := newTestStore(t)
	cases := []struct {
		name   string
		ip     string
		status string
		window time.Duration
	}{
		{"empty ip", "", PullStatusRateLimited, time.Hour},
		{"unknown status", "203.0.113.12", "nonsense", time.Hour},
		{"zero window", "203.0.113.12", PullStatusRateLimited, 0},
	}
	for _, tc := range cases {
		if _, err := s.CountRecentPullStatus(tc.ip, tc.status, tc.window); err == nil {
			t.Errorf("%s: error = nil, want an error", tc.name)
		}
	}
}

// TestCountRecentPullStatus_UsesIndex the escalation chain runs on the /sub
// hot path, on the request an abuser controls. The query must be index-served,
// never a full scan of a table that grows with every pull.
func TestCountRecentPullStatus_UsesIndex(t *testing.T) {
	s := newTestStore(t)
	if !indexExists(t, s, "idx_pull_logs_ip_status_time") {
		t.Fatal("idx_pull_logs_ip_status_time missing after migrate()")
	}

	rows, err := s.db.Query(`
		EXPLAIN QUERY PLAN
		SELECT COUNT(*) FROM pull_logs
		WHERE ip = ? AND status = ? AND datetime(pulled_at) > datetime(?)`,
		"203.0.113.13", PullStatusRateLimited, "2026-07-27 11:00:00")
	if err != nil {
		t.Fatalf("explain query plan: %v", err)
	}
	defer rows.Close()

	plan := ""
	for rows.Next() {
		var id, parent, notUsed int
		var detail string
		if err := rows.Scan(&id, &parent, &notUsed, &detail); err != nil {
			t.Fatalf("scan plan: %v", err)
		}
		plan += detail + "\n"
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("plan rows: %v", err)
	}
	if !containsIndexSearch(plan, "idx_pull_logs_ip_status_time") {
		t.Errorf("query plan does not use idx_pull_logs_ip_status_time:\n%s", plan)
	}
}

// containsIndexSearch reports whether an EXPLAIN QUERY PLAN output shows a
// search served by the named index (as opposed to "SCAN pull_logs").
func containsIndexSearch(plan, index string) bool {
	return strings.Contains(plan, "USING INDEX "+index) ||
		strings.Contains(plan, "USING COVERING INDEX "+index)
}
