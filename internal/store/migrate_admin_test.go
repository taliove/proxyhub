package store

import (
	"errors"
	"path/filepath"
	"testing"
)

// TestMigrateAdminToSuperUser_FreshInstall verifies that on a brand-new
// database with no admin credentials, MigrateAdminToSuperUser is a no-op
// (the setup screen will create the first user via a later code path).
func TestMigrateAdminToSuperUser_FreshInstall(t *testing.T) {
	s := newTestStore(t)

	users, err := s.ListUsers()
	if err != nil {
		t.Fatalf("ListUsers() error = %v", err)
	}
	if len(users) != 0 {
		t.Errorf("ListUsers() len = %d, want 0 on fresh install", len(users))
	}
}

// TestMigrateAdminToSuperUser_MigratesLegacyCredentials verifies that when
// settings contains admin_user + admin_pass_hash and the users table is
// empty, a super_admin row is created with the legacy hash preserved and
// must_change_password = false.
func TestMigrateAdminToSuperUser_MigratesLegacyCredentials(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")

	// Bootstrap a database and seed legacy settings, bypassing the Open()
	// migration path so the users table starts empty.
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	// Seed legacy credentials and clear any auto-migrated user.
	if err := s.SetSetting("admin_user", "operator1"); err != nil {
		t.Fatalf("SetSetting admin_user: %v", err)
	}
	if err := s.SetSetting("admin_pass_hash", "$2a$10$legacyhash"); err != nil {
		t.Fatalf("SetSetting admin_pass_hash: %v", err)
	}
	if _, err := s.db.Exec(`DELETE FROM users`); err != nil {
		t.Fatalf("clear users: %v", err)
	}
	s.Close()

	// Reopen: migrate() should run MigrateAdminToSuperUser.
	s2, err := Open(dbPath)
	if err != nil {
		t.Fatalf("second Open() error = %v", err)
	}
	defer s2.Close()

	u, err := s2.GetUserByUsername("operator1")
	if err != nil {
		t.Fatalf("GetUserByUsername() error = %v", err)
	}
	if u.Role != RoleSuperAdmin {
		t.Errorf("Role = %q, want %q", u.Role, RoleSuperAdmin)
	}
	if u.PassHash != "$2a$10$legacyhash" {
		t.Errorf("PassHash = %q, want legacy hash preserved", u.PassHash)
	}
	if u.MustChangePassword {
		t.Error("MustChangePassword = true, want false (migrated super admin keeps password)")
	}

	// Legacy KV must remain untouched.
	v, err := s2.GetSetting("admin_user")
	if err != nil || v != "operator1" {
		t.Errorf("admin_user KV = %q, %v; want preserved", v, err)
	}
}

// TestMigrateAdminToSuperUser_Idempotent verifies that re-running the
// migration with a populated users table does not insert duplicates.
func TestMigrateAdminToSuperUser_Idempotent(t *testing.T) {
	s := newTestStore(t)

	if err := s.SetSetting("admin_user", "legacyadmin"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	if err := s.SetSetting("admin_pass_hash", "h"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	// Pre-populate users table (simulating an earlier migration).
	if _, err := s.CreateUser("existing", "h2", RoleUser, false); err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}

	if err := s.MigrateAdminToSuperUser(); err != nil {
		t.Fatalf("MigrateAdminToSuperUser() error = %v", err)
	}

	users, err := s.ListUsers()
	if err != nil {
		t.Fatalf("ListUsers() error = %v", err)
	}
	if len(users) != 1 {
		t.Errorf("ListUsers() len = %d, want 1 (no migration when non-empty)", len(users))
	}
	if users[0].Username != "existing" {
		t.Errorf("existing user overwritten: got %q", users[0].Username)
	}
}

// TestMigrateAdminToSuperUser_MissingPassHash verifies that an incomplete
// legacy state (username without password hash) does not create a row.
func TestMigrateAdminToSuperUser_MissingPassHash(t *testing.T) {
	s := newTestStore(t)

	if err := s.SetSetting("admin_user", "lonely"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	if err := s.MigrateAdminToSuperUser(); err != nil {
		t.Fatalf("MigrateAdminToSuperUser() error = %v", err)
	}

	_, err := s.GetUserByUsername("lonely")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("GetUserByUsername() error = %v, want ErrNotFound", err)
	}
}

// TestMigrateAdminToSuperUser_ReservedNameStillMigrates verifies the
// documented bypass: the migration writes the row even when the legacy
// username happens to match the honeypot blocklist. Locking the operator
// out at startup would be worse than carrying the risky name forward.
func TestMigrateAdminToSuperUser_ReservedNameStillMigrates(t *testing.T) {
	s := newTestStore(t)

	if err := s.SetSetting("admin_user", "admin"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	if err := s.SetSetting("admin_pass_hash", "h"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	if err := s.MigrateAdminToSuperUser(); err != nil {
		t.Fatalf("MigrateAdminToSuperUser() error = %v", err)
	}

	u, err := s.GetUserByUsername("admin")
	if err != nil {
		t.Fatalf("GetUserByUsername() error = %v", err)
	}
	if u.Role != RoleSuperAdmin {
		t.Errorf("Role = %q, want %q", u.Role, RoleSuperAdmin)
	}
}

// TestMigrateAdminToSuperUser_EmptyStrings verifies empty-string KV values
// are treated as missing.
func TestMigrateAdminToSuperUser_EmptyStrings(t *testing.T) {
	s := newTestStore(t)

	if err := s.SetSetting("admin_user", ""); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	if err := s.SetSetting("admin_pass_hash", ""); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	if err := s.MigrateAdminToSuperUser(); err != nil {
		t.Fatalf("MigrateAdminToSuperUser() error = %v", err)
	}

	users, err := s.ListUsers()
	if err != nil {
		t.Fatalf("ListUsers() error = %v", err)
	}
	if len(users) != 0 {
		t.Errorf("ListUsers() len = %d, want 0 for empty legacy values", len(users))
	}
}
