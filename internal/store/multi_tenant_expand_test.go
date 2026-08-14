package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

// newExpandTestStore opens a fresh store for ticket 06 (data model expand) tests.
func newExpandTestStore(t *testing.T) *Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := OpenForTesting(dbPath)
	if err != nil {
		t.Fatalf("OpenForTesting() error = %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// seedSuperAdmin inserts a super_admin user and returns its id.
// The users table is created by migration 017_users.sql (applied by store.migrate
// before migrateMultiTenant runs), so it is guaranteed to exist here.
func seedSuperAdmin(t *testing.T, s *Store) int64 {
	t.Helper()
	res, err := s.db.Exec(
		`INSERT INTO users (username, pass_hash, role) VALUES ('admin', 'x', 'super_admin')`)
	if err != nil {
		t.Fatalf("insert super admin: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}
	return id
}

// columnExists reports whether the given table has a column with the given name.
func columnExists(t *testing.T, s *Store, table, column string) bool {
	t.Helper()
	rows, err := s.db.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		t.Fatalf("pragma_table_info(%s): %v", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan column name: %v", err)
		}
		if name == column {
			return true
		}
	}
	return false
}

// indexExists reports whether the named index exists in sqlite_master.
func indexExists(t *testing.T, s *Store, indexName string) bool {
	t.Helper()
	var name string
	err := s.db.QueryRow(
		`SELECT name FROM sqlite_master WHERE type='index' AND name=?`, indexName,
	).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return false
	}
	if err != nil {
		t.Fatalf("query index %s: %v", indexName, err)
	}
	return name == indexName
}

// userIDTables lists every business table that ticket 06 must augment with user_id.
// The "template" entry refers to the dedicated per-user template table introduced
// by ticket 06 (the legacy clash_template settings key remains as global default).
var userIDTables = []string{
	"endpoints",
	"airports",
	"self_hosted_nodes",
	"nodes",
	"node_blocks",
	"node_overrides",
	"node_health",
	"node_tags",
	"refresh_runs",
	"pull_logs",
	"jobs",
	"exam_history",
	"airport_test_runs",
	"speedtest_results",
	"audit_logs",
	"template",
}

// TestExpand_UserIDColumnsPresent verifies every business table gains a user_id
// column (INTEGER NOT NULL DEFAULT 0) plus an index, as required by ticket 06.
func TestExpand_UserIDColumnsPresent(t *testing.T) {
	s := newExpandTestStore(t)

	for _, table := range userIDTables {
		if !columnExists(t, s, table, "user_id") {
			t.Errorf("table %s missing user_id column", table)
		}
		wantIdx := "idx_" + table + "_user_id"
		if !indexExists(t, s, wantIdx) {
			t.Errorf("table %s missing index %s", table, wantIdx)
		}
	}
}

// TestExpand_BackfillToSuperAdmin verifies that pre-existing rows (written with
// user_id = 0 by older code paths) are reassigned to the super_admin's id.
func TestExpand_BackfillToSuperAdmin(t *testing.T) {
	s := newExpandTestStore(t)

	// Seed a super_admin (in production ticket 01 already created it).
	superID := seedSuperAdmin(t, s)

	// Simulate pre-ticket-06 data by zeroing user_id across all business tables.
	for _, table := range userIDTables {
		if _, err := s.db.Exec(`UPDATE ` + table + ` SET user_id = 0`); err != nil {
			t.Fatalf("zero user_id on %s: %v", table, err)
		}
	}
	// Insert at least one row into a representative subset so backfill is observable.
	if _, err := s.db.Exec(`INSERT INTO airports (name, url, user_id) VALUES ('a', 'https://example.com', 0)`); err != nil {
		t.Fatalf("seed airport: %v", err)
	}
	if _, err := s.db.Exec(`INSERT INTO audit_logs (event_type, ip, username, user_id) VALUES ('login_success', '127.0.0.1', 'admin', 0)`); err != nil {
		t.Fatalf("seed audit log: %v", err)
	}

	if err := s.BackfillUserID(); err != nil {
		t.Fatalf("BackfillUserID() error = %v", err)
	}

	var airportUID int64
	if err := s.db.QueryRow(`SELECT user_id FROM airports WHERE name = 'a'`).Scan(&airportUID); err != nil {
		t.Fatalf("query airport user_id: %v", err)
	}
	if airportUID != superID {
		t.Errorf("airport user_id = %d, want super admin id %d", airportUID, superID)
	}

	var auditUID int64
	if err := s.db.QueryRow(`SELECT user_id FROM audit_logs WHERE event_type = 'login_success'`).Scan(&auditUID); err != nil {
		t.Fatalf("query audit user_id: %v", err)
	}
	if auditUID != superID {
		t.Errorf("audit user_id = %d, want super admin id %d", auditUID, superID)
	}
}

// TestExpand_SettingsSplit verifies the settings table is split into a global
// system_settings table plus a per-user user_settings table, and that both
// read/write paths work.
func TestExpand_SettingsSplit(t *testing.T) {
	s := newExpandTestStore(t)

	// Legacy settings writes should still land in system_settings (global scope).
	if err := s.SetSetting("admin_path", "abc"); err != nil {
		t.Fatalf("SetSetting() error = %v", err)
	}
	v, err := s.GetSetting("admin_path")
	if err != nil {
		t.Fatalf("GetSetting() error = %v", err)
	}
	if v != "abc" {
		t.Errorf("GetSetting = %q, want abc", v)
	}

	// system_settings must hold the value.
	var sysVal string
	if err := s.db.QueryRow(`SELECT value FROM system_settings WHERE key = 'admin_path'`).Scan(&sysVal); err != nil {
		t.Fatalf("query system_settings: %v", err)
	}
	if sysVal != "abc" {
		t.Errorf("system_settings value = %q, want abc", sysVal)
	}

	// Per-user settings are isolated by user_id.
	if err := s.SetUserSetting(1, "theme", "dark"); err != nil {
		t.Fatalf("SetUserSetting(1) error = %v", err)
	}
	if err := s.SetUserSetting(2, "theme", "light"); err != nil {
		t.Fatalf("SetUserSetting(2) error = %v", err)
	}

	v1, err := s.GetUserSetting(1, "theme")
	if err != nil {
		t.Fatalf("GetUserSetting(1) error = %v", err)
	}
	if v1 != "dark" {
		t.Errorf("GetUserSetting(1) = %q, want dark", v1)
	}
	v2, err := s.GetUserSetting(2, "theme")
	if err != nil {
		t.Fatalf("GetUserSetting(2) error = %v", err)
	}
	if v2 != "light" {
		t.Errorf("GetUserSetting(2) = %q, want light", v2)
	}

	// Missing per-user key returns ErrNotFound.
	if _, err := s.GetUserSetting(1, "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetUserSetting(1, missing) error = %v, want ErrNotFound", err)
	}
}

// TestExpand_ListAirportsByUser verifies the new per-user filter on airports.
// Legacy ListAirports() must keep returning all rows regardless of user_id.
func TestExpand_ListAirportsByUser(t *testing.T) {
	s := newExpandTestStore(t)

	// Insert airports attributed to different users directly (bypassing CreateAirport,
	// which always writes the caller's user; here we seed both rows by hand).
	if _, err := s.db.Exec(`INSERT INTO airports (name, url, user_id) VALUES ('u1-a', 'https://example.com/1', 1)`); err != nil {
		t.Fatalf("insert u1 airport: %v", err)
	}
	if _, err := s.db.Exec(`INSERT INTO airports (name, url, user_id) VALUES ('u2-a', 'https://example.com/2', 2)`); err != nil {
		t.Fatalf("insert u2 airport: %v", err)
	}

	// Legacy interface returns all rows.
	all, err := s.ListAirports()
	if err != nil {
		t.Fatalf("ListAirports() error = %v", err)
	}
	if len(all) != 2 {
		t.Errorf("ListAirports() len = %d, want 2 (legacy returns all)", len(all))
	}

	// Per-user interface returns only that user's rows.
	u1, err := s.ListAirportsByUser(1)
	if err != nil {
		t.Fatalf("ListAirportsByUser(1) error = %v", err)
	}
	if len(u1) != 1 || u1[0].Name != "u1-a" {
		t.Errorf("ListAirportsByUser(1) = %+v, want only u1-a", u1)
	}

	u2, err := s.ListAirportsByUser(2)
	if err != nil {
		t.Fatalf("ListAirportsByUser(2) error = %v", err)
	}
	if len(u2) != 1 || u2[0].Name != "u2-a" {
		t.Errorf("ListAirportsByUser(2) = %+v, want only u2-a", u2)
	}

	// Unknown user returns empty, not an error.
	u99, err := s.ListAirportsByUser(99)
	if err != nil {
		t.Fatalf("ListAirportsByUser(99) error = %v", err)
	}
	if len(u99) != 0 {
		t.Errorf("ListAirportsByUser(99) len = %d, want 0", len(u99))
	}
}

// TestExpand_NewRowsDefaultToZeroUser verifies that rows written via legacy
// interfaces default to user_id = 0 (the pre-multi-tenant "unowned" bucket),
// keeping existing single-tenant behavior intact during the expand phase.
func TestExpand_NewRowsDefaultToZeroUser(t *testing.T) {
	s := newExpandTestStore(t)

	if _, err := s.CreateAirport("legacy", "https://example.com"); err != nil {
		t.Fatalf("CreateAirport() error = %v", err)
	}
	var uid int64
	if err := s.db.QueryRow(`SELECT user_id FROM airports WHERE name = 'legacy'`).Scan(&uid); err != nil {
		t.Fatalf("query user_id: %v", err)
	}
	if uid != 0 {
		t.Errorf("legacy insert user_id = %d, want 0 (unowned)", uid)
	}
}
