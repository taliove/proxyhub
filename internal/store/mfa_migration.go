package store

import "fmt"

// migrateMFA applies the multi-factor authentication schema (login hardening
// ticket 02). Idempotent: safe on every startup.
//
//   - users gains totp_secret / totp_enabled / recovery_codes_hash. NULL secret
//     and NULL recovery codes mean "never enrolled"; totp_enabled defaults to 0
//     so existing accounts upgrade into the not-yet-bound state.
//   - user_trusted_ips records the per-user "trust this IP for 30 days" grants.
//     The (user_id, ip) primary key keeps trust per user: the same office IP
//     trusted by one account grants nothing to another.
//   - audit_logs gains a (username, ip, created_at) composite index so the trust
//     recommendation query (MFA successes for this user from this IP in the last
//     30 days) is an index range scan instead of a full table scan.
//
// Must run after 017_users.sql has created the users table.
func (s *Store) migrateMFA() error {
	if err := s.addColumnIfMissing("users", "totp_secret", "TEXT"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing("users", "totp_enabled", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing("users", "recovery_codes_hash", "TEXT"); err != nil {
		return err
	}

	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS user_trusted_ips (
	user_id      INTEGER NOT NULL,
	ip           TEXT NOT NULL,
	expires_at   TIMESTAMP NOT NULL,
	last_used_at TIMESTAMP NOT NULL,
	PRIMARY KEY (user_id, ip)
)`); err != nil {
		return fmt.Errorf("create user_trusted_ips: %w", err)
	}

	if _, err := s.db.Exec(
		`CREATE INDEX IF NOT EXISTS idx_audit_logs_user_ip ON audit_logs(username, ip, created_at)`,
	); err != nil {
		return fmt.Errorf("create idx_audit_logs_user_ip: %w", err)
	}

	return nil
}
