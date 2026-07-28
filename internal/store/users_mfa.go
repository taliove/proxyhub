package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// UserMFAConfig is the multi-factor state of one account.
//
// "Never enrolled" is TOTPSecret == "", Enabled == false and no recovery
// codes; that is what a fresh account and a reset account both look like.
type UserMFAConfig struct {
	UserID int64 `json:"user_id"`
	// TOTPSecret is the base32 shared secret, empty when unbound. Stored in
	// plaintext by design (same trust boundary as the password hashes in this
	// database, see docs/SECURITY.md).
	TOTPSecret string `json:"-"`
	// Enabled reports whether TOTP verification is active for this account.
	Enabled bool `json:"enabled"`
	// RecoveryCodesHash holds the SHA-256 hashes of the unused recovery codes.
	RecoveryCodesHash []string `json:"-"`
}

// encodeRecoveryCodes serializes recovery code hashes for storage. An empty
// set is stored as an empty JSON array (an explicit "all codes invalidated"),
// distinct from the NULL written by ResetUserMFA ("never enrolled").
func encodeRecoveryCodes(codes []string) (string, error) {
	if len(codes) == 0 {
		return "[]", nil
	}
	buf, err := json.Marshal(codes)
	if err != nil {
		return "", fmt.Errorf("encode recovery codes: %w", err)
	}
	return string(buf), nil
}

// decodeRecoveryCodes parses the stored recovery code hashes. NULL and the
// empty string both decode to no codes.
func decodeRecoveryCodes(raw sql.NullString) ([]string, error) {
	if !raw.Valid || raw.String == "" {
		return nil, nil
	}
	var codes []string
	if err := json.Unmarshal([]byte(raw.String), &codes); err != nil {
		return nil, fmt.Errorf("decode recovery codes: %w", err)
	}
	return codes, nil
}

// GetUserMFAConfig reads the MFA state of a user.
// Returns ErrNotFound when the id does not exist.
func (s *Store) GetUserMFAConfig(id int64) (*UserMFAConfig, error) {
	var cfg UserMFAConfig
	var secret, codes sql.NullString
	var enabled int
	err := s.db.QueryRow(
		`SELECT id, totp_secret, totp_enabled, recovery_codes_hash
		 FROM users WHERE id = ?`, id,
	).Scan(&cfg.UserID, &secret, &enabled, &codes)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get user mfa config: %w", err)
	}

	cfg.TOTPSecret = secret.String
	cfg.Enabled = enabled != 0
	decoded, err := decodeRecoveryCodes(codes)
	if err != nil {
		return nil, err
	}
	cfg.RecoveryCodesHash = decoded
	return &cfg, nil
}

// ResetUserMFA returns the account to the never-enrolled state: the secret is
// dropped, TOTP is disabled and every recovery code is invalidated. The
// columns are set back to NULL rather than empty strings so a reset account is
// indistinguishable from one that never bound an authenticator.
//
// Trusted IPs are deliberately left alone: revoking them is a separate,
// independently auditable action (RevokeAllTrustedIPs).
//
// Returns ErrNotFound when the id does not exist. Idempotent otherwise.
func (s *Store) ResetUserMFA(id int64) error {
	res, err := s.db.Exec(
		`UPDATE users
		 SET totp_secret = NULL, totp_enabled = 0, recovery_codes_hash = NULL
		 WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("reset user mfa: %w", err)
	}
	return checkAffected(res)
}
