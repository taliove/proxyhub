package store

import (
	"fmt"
	"strings"
)

// userDataTables enumerates every business table that holds per-user rows
// and must be purged when a user is physically deleted. The audit_logs
// table is deliberately NOT in this list: audit history is an append-only
// security trail and survives user deletion so post-mortem investigations
// can still attribute past events to the deleted account.
//
// Keep in sync with multiTenantTables in multi_tenant.go (minus audit_logs)
// plus user_quotas and user_xray_instances, which are per-user by primary
// key rather than by a user_id column.
var userDataTables = []string{
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
	"template",
	"user_settings",
}

// DeleteUserCascade removes a user row together with every per-user
// resource row owned by that user, in one transaction. Tables that carry
// a user_id column are cleaned by `DELETE ... WHERE user_id = ?`; the
// quota row and the Xray instance row are keyed by user_id directly.
//
// audit_logs are intentionally preserved (see userDataTables comment).
//
// Note: SQLite foreign keys are not enforced in this codebase (no PRAGMA
// foreign_keys=ON), so the user_quotas ON DELETE CASCADE clause does not
// fire automatically; the explicit deletes below are the real mechanism.
func (s *Store) DeleteUserCascade(userID int64) error {
	if userID <= 0 {
		return fmt.Errorf("%w: user_id must be positive", ErrInvalidInput)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin delete-user tx: %w", err)
	}
	defer tx.Rollback()

	for _, table := range userDataTables {
		stmt := fmt.Sprintf(`DELETE FROM %s WHERE user_id = ?`, table)
		if _, err := tx.Exec(stmt, userID); err != nil {
			// Table may not exist yet on very old databases; ignore that
			// specific failure so the cascade stays forward-compatible.
			if strings.Contains(err.Error(), "no such table") {
				continue
			}
			return fmt.Errorf("delete from %s: %w", table, err)
		}
	}

	for _, table := range []string{"user_quotas", "user_xray_instances"} {
		stmt := fmt.Sprintf(`DELETE FROM %s WHERE user_id = ?`, table)
		if _, err := tx.Exec(stmt, userID); err != nil {
			if strings.Contains(err.Error(), "no such table") {
				continue
			}
			return fmt.Errorf("delete from %s: %w", table, err)
		}
	}

	if _, err := tx.Exec(`DELETE FROM users WHERE id = ?`, userID); err != nil {
		return fmt.Errorf("delete user row: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit delete-user tx: %w", err)
	}
	return nil
}
