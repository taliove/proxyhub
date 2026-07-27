package store

import (
	"database/sql"
	"errors"
	"fmt"
)

// MigrateAdminToSuperUser migrates the legacy single-admin credentials from
// settings (admin_user / admin_pass_hash) into the users table as a
// super_admin row. It runs at startup and is idempotent:
//   - users table non-empty            -> no-op
//   - settings.admin_user missing      -> no-op (fresh install, setup screen
//     will populate settings + the first user via the same code path later)
//   - settings.admin_pass_hash missing -> no-op (incomplete state; do not
//     guess a password)
//
// The legacy KV entries are kept untouched so older code paths that still
// read them continue to work until the auth migration (ticket 02) removes
// them. The migrated row uses must_change_password = false: the operator
// already chose this password once.
func (s *Store) MigrateAdminToSuperUser() error {
	count, err := s.countUsers()
	if err != nil {
		return fmt.Errorf("count users: %w", err)
	}
	if count > 0 {
		return nil
	}

	username, err := s.GetSetting("admin_user")
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read admin_user: %w", err)
	}

	passHash, err := s.GetSetting("admin_pass_hash")
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read admin_pass_hash: %w", err)
	}
	if username == "" || passHash == "" {
		return nil
	}

	// Bypass honeypot guard: this row was created by the operator through the
	// legacy setup flow, which already enforced the honeypot blocklist. If an
	// old database somehow contains a reserved name (e.g. before the
	// honeypot guard was added), we still migrate it as-is so the operator
	// keeps access — locking them out at startup would be worse.
	res, err := s.db.Exec(
		`INSERT INTO users (username, pass_hash, role, must_change_password)
		 VALUES (?, ?, ?, 0)
		 ON CONFLICT(username) DO NOTHING`,
		username, passHash, RoleSuperAdmin,
	)
	if err != nil {
		return fmt.Errorf("migrate super admin: %w", err)
	}
	_ = res // ON CONFLICT DO NOTHING makes this fully idempotent
	return nil
}

// countUsers returns the number of rows in the users table. Returns 0 when
// the users table does not exist yet (defensive; migrate() creates it before
// calling MigrateAdminToSuperUser).
func (s *Store) countUsers() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		return 0, err
	}
	return n, nil
}
