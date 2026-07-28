package store

import "testing"

// TestAuditLogs_UserAgent tests that user_agent column is stored and retrieved correctly
func TestAuditLogs_UserAgent(t *testing.T) {
	st := newTestStore(t)

	// Record events with different user agents
	if err := st.RecordAuditEvent("login_success", "1.1.1.1", "admin", "test detail", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/120.0"); err != nil {
		t.Fatalf("RecordAuditEvent() with UA error = %v", err)
	}
	if err := st.RecordAuditEvent("login_failure", "2.2.2.2", "attacker", "", "curl/7.68.0"); err != nil {
		t.Fatalf("RecordAuditEvent() with UA error = %v", err)
	}
	// Empty UA should also work
	if err := st.RecordAuditEvent("honeypot_ban", "3.3.3.3", "root", "banned", ""); err != nil {
		t.Fatalf("RecordAuditEvent() with empty UA error = %v", err)
	}

	events, total, err := st.ListAuditEvents(AuditFilter{}, 50, 0)
	if err != nil {
		t.Fatalf("ListAuditEvents() error = %v", err)
	}
	if total != 3 {
		t.Fatalf("total = %d, want 3", total)
	}

	// Verify UAs are stored correctly (reverse order: newest first)
	if events[0].UserAgent != "" {
		t.Errorf("event[0] UA = %q, want empty", events[0].UserAgent)
	}
	if events[1].UserAgent != "curl/7.68.0" {
		t.Errorf("event[1] UA = %q, want curl/7.68.0", events[1].UserAgent)
	}
	if events[2].UserAgent != "Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/120.0" {
		t.Errorf("event[2] UA = %q, want Chrome UA", events[2].UserAgent)
	}
}

// TestAuditLogs_UserAgent_ExistingRecords tests backward compatibility with existing records
func TestAuditLogs_UserAgent_ExistingRecords(t *testing.T) {
	st := newTestStore(t)

	// Simulate old records without user_agent (manual insert)
	st.db.Exec(`INSERT INTO audit_logs (event_type, ip, username, detail, created_at) VALUES (?, ?, ?, ?, datetime('now'))`,
		"old_event", "1.1.1.1", "user", "old detail")

	// New record with UA
	if err := st.RecordAuditEvent("new_event", "2.2.2.2", "user", "new detail", "Mozilla/5.0"); err != nil {
		t.Fatalf("RecordAuditEvent() error = %v", err)
	}

	events, total, err := st.ListAuditEvents(AuditFilter{}, 50, 0)
	if err != nil {
		t.Fatalf("ListAuditEvents() error = %v", err)
	}
	if total != 2 {
		t.Fatalf("total = %d, want 2", total)
	}

	// Old record should have empty UA (default value)
	if events[1].UserAgent != "" {
		t.Errorf("old record UA = %q, want empty", events[1].UserAgent)
	}
	// New record should have the UA
	if events[0].UserAgent != "Mozilla/5.0" {
		t.Errorf("new record UA = %q, want Mozilla/5.0", events[0].UserAgent)
	}
}
