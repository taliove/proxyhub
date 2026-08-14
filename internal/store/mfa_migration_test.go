package store

import (
	"path/filepath"
	"testing"
)

// TestMigrateMFA_SchemaInPlace verifies that a freshly opened database carries
// the MFA columns, the trusted IP table and the audit recommendation index.
func TestMigrateMFA_SchemaInPlace(t *testing.T) {
	s := newTestStore(t)

	for _, col := range []string{"totp_secret", "totp_enabled", "recovery_codes_hash"} {
		if !s.columnExistsUnlocked("users", col) {
			t.Errorf("users.%s column missing after migrate()", col)
		}
	}

	if _, err := s.db.Exec(`SELECT user_id, ip, expires_at, last_used_at FROM user_trusted_ips LIMIT 0`); err != nil {
		t.Errorf("user_trusted_ips table missing or malformed: %v", err)
	}

	if !indexExists(t, s, "idx_audit_logs_user_ip") {
		t.Error("idx_audit_logs_user_ip index missing after migrate()")
	}
}

// TestMigrateMFA_DefaultsForExistingRows verifies old rows get totp_enabled = 0
// and NULL secret/recovery codes, i.e. "never enrolled".
func TestMigrateMFA_DefaultsForExistingRows(t *testing.T) {
	s := newTestStore(t)

	u, err := s.CreateUser("alice", "$2a$10$hash", RoleUser, false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	cfg, err := s.GetUserMFAConfig(u.ID)
	if err != nil {
		t.Fatalf("GetUserMFAConfig: %v", err)
	}
	if cfg.Enabled {
		t.Error("Enabled = true, want false for a fresh user")
	}
	if cfg.TOTPSecret != "" {
		t.Errorf("TOTPSecret = %q, want empty", cfg.TOTPSecret)
	}
	if len(cfg.RecoveryCodesHash) != 0 {
		t.Errorf("RecoveryCodesHash = %v, want empty", cfg.RecoveryCodesHash)
	}
}

// TestMigrateMFA_Idempotent verifies repeated migration runs neither fail nor
// clobber existing MFA state, both in-process and across a reopen.
func TestMigrateMFA_Idempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := OpenForTesting(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	u, err := s.CreateUser("bob", "$2a$10$hash", RoleUser, false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	secret := "JBSWY3DPEHPK3PXP"
	enabled := true
	codes := []string{"hash-a", "hash-b"}
	if err := s.UpdateUser(u.ID, UserUpdate{
		TOTPSecret:        &secret,
		TOTPEnabled:       &enabled,
		RecoveryCodesHash: &codes,
	}); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	if err := s.AddTrustedIP(u.ID, "203.0.113.7"); err != nil {
		t.Fatalf("AddTrustedIP: %v", err)
	}

	// Re-running the migration in place must be a no-op.
	if err := s.migrateMFA(); err != nil {
		t.Fatalf("second migrateMFA: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Reopening runs migrate() again over a populated database.
	s2, err := OpenForTesting(dbPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()

	cfg, err := s2.GetUserMFAConfig(u.ID)
	if err != nil {
		t.Fatalf("GetUserMFAConfig after reopen: %v", err)
	}
	if !cfg.Enabled || cfg.TOTPSecret != secret || len(cfg.RecoveryCodesHash) != 2 {
		t.Errorf("MFA config lost across migration: %+v", cfg)
	}
	trusted, err := s2.IsTrustedIP(u.ID, "203.0.113.7")
	if err != nil {
		t.Fatalf("IsTrustedIP after reopen: %v", err)
	}
	if !trusted {
		t.Error("trusted IP lost across migration re-run")
	}
}
