package store

import (
	"database/sql"
	"errors"
	"fmt"
)

// This file drives the ticket 06 migration (data model multi-tenancy,
// expand phase). It is intentionally self-contained: the expand phase must
// not change any existing behavior, so all additions live here and in
// settings_split.go, and existing query paths keep hitting the same columns
// they always did.
//
// What migrateMultiTenant does, in order:
//  1. Create the dedicated per-user template table (the "template" entry in
//     the ticket's table list — the legacy clash_template settings key stays
//     as the global default; per-user template overrides land here).
//  2. Add user_id (INTEGER NOT NULL DEFAULT 0) to every business table via
//     addColumnIfMissing (idempotent; SQLite has no IF NOT EXISTS for
//     ADD COLUMN). Default 0 is the "unowned" bucket — legacy rows stay
//     visible to legacy queries, which do not filter by user_id.
//  3. Create one index per table on user_id.
//  4. Create system_settings / user_settings and copy legacy settings rows
//     into system_settings (the legacy table itself is kept for rollback).
//  5. Backfill: reassign every row still at user_id = 0 to the first
//     super_admin, if one exists. On a fresh install before ticket 01 seeds
//     the admin this is a no-op; BackfillUserID is exported so the ticket 01
//     seeding path can re-run it once the super admin exists.
//
// Reference SQL for the whole migration lives in
// migrations/019_user_id_backfill.sql; the Go side applies the same schema
// incrementally so existing databases converge without a rebuild.

// multiTenantTables lists every business table that gains a user_id column
// in the expand phase. Keep in sync with migrations/019_user_id_backfill.sql
// and with userIDTables in multi_tenant_expand_test.go.
var multiTenantTables = []string{
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

// migrateMultiTenant applies the ticket 06 schema changes idempotently.
// Called from migrate() BEFORE MigrateAdminToSuperUser so the settings split
// (system_settings / user_settings) and the users table both exist before any
// code reads settings; MigrateAdminToSuperUser then runs on top and seeds the
// first super_admin, after which BackfillUserID assigns legacy rows to it.
func (s *Store) migrateMultiTenant() error {
	// 1. Dedicated per-user template table. Created first so the table loop
	//    below can add its user_id column through the same code path as the
	//    other tables (it is created with user_id already present, making
	//    the per-table ALTER a no-op for it).
	if _, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS template (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id    INTEGER NOT NULL DEFAULT 0,
			name       TEXT NOT NULL DEFAULT '',
			content    TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`); err != nil {
		return fmt.Errorf("create template table: %w", err)
	}

	// 2+3. user_id column + index on every business table.
	for _, table := range multiTenantTables {
		if err := s.addColumnIfMissing(table, "user_id", "INTEGER NOT NULL DEFAULT 0"); err != nil {
			return err
		}
		idx := fmt.Sprintf(
			`CREATE INDEX IF NOT EXISTS idx_%s_user_id ON %s(user_id)`, table, table)
		if _, err := s.db.Exec(idx); err != nil {
			return fmt.Errorf("create index idx_%s_user_id: %w", table, err)
		}
	}

	// 4. Settings split. system_settings takes over the global scope;
	//    user_settings holds per-user overrides. Legacy rows are copied
	//    (INSERT OR IGNORE keeps it idempotent); the legacy settings table
	//    is intentionally NOT dropped during the expand phase.
	if _, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS system_settings (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`); err != nil {
		return fmt.Errorf("create system_settings: %w", err)
	}
	if _, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS user_settings (
			user_id INTEGER NOT NULL,
			key     TEXT NOT NULL,
			value   TEXT NOT NULL,
			PRIMARY KEY (user_id, key)
		)`); err != nil {
		return fmt.Errorf("create user_settings: %w", err)
	}
	if _, err := s.db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_user_settings_user_id ON user_settings(user_id)`); err != nil {
		return fmt.Errorf("create user_settings index: %w", err)
	}
	if _, err := s.db.Exec(`
		INSERT OR IGNORE INTO system_settings (key, value) SELECT key, value FROM settings`); err != nil {
		return fmt.Errorf("migrate legacy settings: %w", err)
	}

	// 5. Backfill pre-existing rows to the super admin (no-op when no super
	//    admin exists yet — see BackfillUserID).
	return s.BackfillUserID()
}

// BackfillUserID reassigns every business row still marked with the
// "unowned" user_id = 0 to the first super_admin. It is idempotent and
// safe to call repeatedly: once rows carry a real owner they are never
// touched again. When no super_admin exists (fresh install before the
// ticket 01 seeding runs) the call is a no-op and returns nil.
func (s *Store) BackfillUserID() error {
	superID, err := s.firstSuperAdminID()
	if err != nil {
		return err
	}
	if superID == 0 {
		// No super admin yet — nothing to backfill. Ticket 01's seeding
		// path calls BackfillUserID again once the admin exists.
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin backfill tx: %w", err)
	}
	defer tx.Rollback()

	for _, table := range multiTenantTables {
		stmt := fmt.Sprintf(`UPDATE %s SET user_id = ? WHERE user_id = 0`, table)
		if _, err := tx.Exec(stmt, superID); err != nil {
			return fmt.Errorf("backfill %s: %w", table, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit backfill: %w", err)
	}
	return nil
}

// firstSuperAdminID returns the id of the first super_admin user, or 0 when
// the users table does not exist yet (ticket 01 not applied) or contains no
// super_admin. Both absences are normal during the expand phase and are not
// errors.
func (s *Store) firstSuperAdminID() (int64, error) {
	// The users table is owned by ticket 01; during the expand phase it may
	// legitimately not exist yet. Treat that as "no super admin".
	var tbl string
	err := s.db.QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name='users'`).Scan(&tbl)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("check users table: %w", err)
	}

	var id int64
	err = s.db.QueryRow(
		`SELECT id FROM users WHERE role = 'super_admin' ORDER BY id ASC LIMIT 1`).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("query super admin: %w", err)
	}
	return id, nil
}
