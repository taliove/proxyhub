package store

import (
	"database/sql"
	"errors"
	"fmt"
)

// This file implements the settings split required by ticket 06
// (data model multi-tenancy, expand phase):
//
//   - system_settings: global key/value pairs (the legacy settings table
//     content is migrated here verbatim; GetSetting/SetSetting are
//     redirected to this table so all existing callers keep working).
//   - user_settings: per-user key/value pairs keyed by (user_id, key).
//
// The legacy settings table is left in place during the expand phase so a
// rollback only needs to point reads back at it; it is dropped in the
// contract phase. Both new tables are created by migrateMultiTenant (see
// multi_tenant.go), which also migrates legacy rows into system_settings.

// GetUserSetting returns the per-user value for key. Returns ErrNotFound
// when the (userID, key) pair has never been written; callers that want a
// fallback to the global default compose GetUserSetting + GetSetting
// themselves (the store does not silently fall back — explicit is better
// than implicit when introducing a second settings scope).
func (s *Store) GetUserSetting(userID int64, key string) (string, error) {
	var value string
	err := s.db.QueryRow(
		`SELECT value FROM user_settings WHERE user_id = ? AND key = ?`,
		userID, key,
	).Scan(&value)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("get user setting: %w", err)
	}
	return value, nil
}

// SetUserSetting upserts the per-user value for key.
func (s *Store) SetUserSetting(userID int64, key, value string) error {
	if key == "" {
		return errors.New("key is required")
	}
	_, err := s.db.Exec(`
		INSERT INTO user_settings (user_id, key, value) VALUES (?, ?, ?)
		ON CONFLICT(user_id, key) DO UPDATE SET value = excluded.value`,
		userID, key, value)
	if err != nil {
		return fmt.Errorf("set user setting: %w", err)
	}
	return nil
}

// DeleteUserSetting removes a per-user override. Missing rows are a no-op
// (idempotent delete, same contract as UnblockNode).
func (s *Store) DeleteUserSetting(userID int64, key string) error {
	_, err := s.db.Exec(
		`DELETE FROM user_settings WHERE user_id = ? AND key = ?`, userID, key)
	if err != nil {
		return fmt.Errorf("delete user setting: %w", err)
	}
	return nil
}

// ListUserSettings returns all key/value pairs owned by userID.
// An unknown user simply yields an empty map (no error), matching the
// "absent = empty" convention used by ListNodeTags and friends.
func (s *Store) ListUserSettings(userID int64) (map[string]string, error) {
	rows, err := s.db.Query(
		`SELECT key, value FROM user_settings WHERE user_id = ? ORDER BY key`, userID)
	if err != nil {
		return nil, fmt.Errorf("list user settings: %w", err)
	}
	defer rows.Close()

	result := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, fmt.Errorf("scan user setting: %w", err)
		}
		result[k] = v
	}
	return result, rows.Err()
}
