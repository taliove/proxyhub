package store

import (
	"errors"
	"testing"
)

func TestUpdateUser_MFAFieldsRoundTrip(t *testing.T) {
	s := newTestStore(t)
	u := mustUser(t, s, "alice")

	secret := "JBSWY3DPEHPK3PXP"
	codes := []string{"aaaa", "bbbb", "cccc"}
	if err := s.UpdateUser(u.ID, UserUpdate{
		TOTPSecret:        &secret,
		RecoveryCodesHash: &codes,
	}); err != nil {
		t.Fatalf("UpdateUser (secret + codes): %v", err)
	}

	cfg, err := s.GetUserMFAConfig(u.ID)
	if err != nil {
		t.Fatalf("GetUserMFAConfig: %v", err)
	}
	if cfg.UserID != u.ID {
		t.Errorf("UserID = %d, want %d", cfg.UserID, u.ID)
	}
	if cfg.TOTPSecret != secret {
		t.Errorf("TOTPSecret = %q, want %q", cfg.TOTPSecret, secret)
	}
	if cfg.Enabled {
		t.Error("Enabled = true; writing a secret alone must not enable MFA (two-step enroll)")
	}
	if len(cfg.RecoveryCodesHash) != 3 || cfg.RecoveryCodesHash[0] != "aaaa" {
		t.Errorf("RecoveryCodesHash = %v, want %v", cfg.RecoveryCodesHash, codes)
	}

	enabled := true
	if err := s.UpdateUser(u.ID, UserUpdate{TOTPEnabled: &enabled}); err != nil {
		t.Fatalf("UpdateUser (enable): %v", err)
	}
	cfg, err = s.GetUserMFAConfig(u.ID)
	if err != nil {
		t.Fatalf("GetUserMFAConfig after enable: %v", err)
	}
	if !cfg.Enabled {
		t.Error("Enabled = false after enabling")
	}
	if cfg.TOTPSecret != secret {
		t.Errorf("TOTPSecret = %q after enable, want unchanged", cfg.TOTPSecret)
	}
}

func TestUpdateUser_MFAFieldsAreOptional(t *testing.T) {
	s := newTestStore(t)
	u := mustUser(t, s, "alice")

	secret := "JBSWY3DPEHPK3PXP"
	enabled := true
	codes := []string{"aaaa"}
	if err := s.UpdateUser(u.ID, UserUpdate{
		TOTPSecret:        &secret,
		TOTPEnabled:       &enabled,
		RecoveryCodesHash: &codes,
	}); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}

	// Updating an unrelated field must leave MFA state untouched.
	mustChange := true
	if err := s.UpdateUser(u.ID, UserUpdate{MustChangePassword: &mustChange}); err != nil {
		t.Fatalf("UpdateUser (must_change_password): %v", err)
	}
	cfg, err := s.GetUserMFAConfig(u.ID)
	if err != nil {
		t.Fatalf("GetUserMFAConfig: %v", err)
	}
	if !cfg.Enabled || cfg.TOTPSecret != secret || len(cfg.RecoveryCodesHash) != 1 {
		t.Errorf("MFA state changed by unrelated update: %+v", cfg)
	}
}

func TestUpdateUser_EmptyRecoveryCodesInvalidatesAll(t *testing.T) {
	s := newTestStore(t)
	u := mustUser(t, s, "alice")

	codes := []string{"aaaa", "bbbb"}
	if err := s.UpdateUser(u.ID, UserUpdate{RecoveryCodesHash: &codes}); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	empty := []string{}
	if err := s.UpdateUser(u.ID, UserUpdate{RecoveryCodesHash: &empty}); err != nil {
		t.Fatalf("UpdateUser (empty codes): %v", err)
	}

	cfg, err := s.GetUserMFAConfig(u.ID)
	if err != nil {
		t.Fatalf("GetUserMFAConfig: %v", err)
	}
	if len(cfg.RecoveryCodesHash) != 0 {
		t.Errorf("RecoveryCodesHash = %v, want empty after writing an empty set", cfg.RecoveryCodesHash)
	}
}

func TestGetUserMFAConfig_NotFound(t *testing.T) {
	s := newTestStore(t)

	if _, err := s.GetUserMFAConfig(4242); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetUserMFAConfig(unknown) error = %v, want ErrNotFound", err)
	}
}

func TestResetUserMFA_BackToNeverEnrolled(t *testing.T) {
	s := newTestStore(t)
	u := mustUser(t, s, "alice")

	secret := "JBSWY3DPEHPK3PXP"
	enabled := true
	codes := []string{"aaaa", "bbbb"}
	if err := s.UpdateUser(u.ID, UserUpdate{
		TOTPSecret:        &secret,
		TOTPEnabled:       &enabled,
		RecoveryCodesHash: &codes,
	}); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}

	if err := s.ResetUserMFA(u.ID); err != nil {
		t.Fatalf("ResetUserMFA: %v", err)
	}

	cfg, err := s.GetUserMFAConfig(u.ID)
	if err != nil {
		t.Fatalf("GetUserMFAConfig: %v", err)
	}
	fresh := mustUser(t, s, "bob")
	freshCfg, err := s.GetUserMFAConfig(fresh.ID)
	if err != nil {
		t.Fatalf("GetUserMFAConfig(fresh): %v", err)
	}
	if cfg.Enabled != freshCfg.Enabled || cfg.TOTPSecret != freshCfg.TOTPSecret ||
		len(cfg.RecoveryCodesHash) != len(freshCfg.RecoveryCodesHash) {
		t.Errorf("after reset = %+v, want equivalent to never-enrolled %+v", cfg, freshCfg)
	}
	// The columns themselves must be NULL, not empty strings: "never bound"
	// is represented by NULL throughout the schema.
	var secretNull, codesNull bool
	if err := s.db.QueryRow(
		`SELECT totp_secret IS NULL, recovery_codes_hash IS NULL FROM users WHERE id = ?`, u.ID,
	).Scan(&secretNull, &codesNull); err != nil {
		t.Fatalf("inspect columns: %v", err)
	}
	if !secretNull || !codesNull {
		t.Errorf("after reset totp_secret/recovery_codes_hash must be NULL (got %v/%v)", secretNull, codesNull)
	}

	// Reset is idempotent on an already-reset user.
	if err := s.ResetUserMFA(u.ID); err != nil {
		t.Errorf("second ResetUserMFA: %v, want nil", err)
	}
}

func TestResetUserMFA_NotFound(t *testing.T) {
	s := newTestStore(t)

	if err := s.ResetUserMFA(4242); !errors.Is(err, ErrNotFound) {
		t.Errorf("ResetUserMFA(unknown) error = %v, want ErrNotFound", err)
	}
}
