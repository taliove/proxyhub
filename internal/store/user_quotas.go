package store

import (
	"database/sql"
	"errors"
	"fmt"
)

// UserQuota is the per-user resource quota record persisted in user_quotas.
// A zero value on either Xray port bound means "no per-user Xray range has
// been allocated yet" (ticket 08 allocates ranges lazily).
type UserQuota struct {
	UserID        int64 `json:"user_id"`
	MaxAirports   int   `json:"max_airports"`
	MaxEndpoints  int   `json:"max_endpoints"`
	MaxTemplates  int   `json:"max_templates"`
	XrayPortStart int   `json:"xray_port_start"`
	XrayPortEnd   int   `json:"xray_port_end"`
}

// GetUserQuota returns the quota row for the user, or ErrNotFound when the
// user has no quota row yet (callers decide whether to treat that as
// "unlimited" or "deny").
func (s *Store) GetUserQuota(userID int64) (*UserQuota, error) {
	q, err := scanUserQuotaFrom(s.db.QueryRow(
		`SELECT user_id, max_airports, max_endpoints, xray_port_start, xray_port_end,
		        COALESCE(max_templates, 10) as max_templates
		 FROM user_quotas WHERE user_id = ?`, userID,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return q, err
}

// UpsertUserQuota inserts or replaces the quota row for a user.
// Negative numeric fields are rejected.
func (s *Store) UpsertUserQuota(q *UserQuota) error {
	if q == nil {
		return fmt.Errorf("%w: quota is required", ErrInvalidInput)
	}
	if q.UserID <= 0 {
		return fmt.Errorf("%w: user_id must be positive", ErrInvalidInput)
	}
	if q.MaxAirports < 0 || q.MaxEndpoints < 0 || q.MaxTemplates < 0 {
		return fmt.Errorf("%w: quotas cannot be negative", ErrInvalidInput)
	}
	if q.XrayPortStart < 0 || q.XrayPortEnd < 0 {
		return fmt.Errorf("%w: port range cannot be negative", ErrInvalidInput)
	}
	if q.XrayPortStart > 0 && q.XrayPortEnd > 0 && q.XrayPortStart > q.XrayPortEnd {
		return fmt.Errorf("%w: port range start > end", ErrInvalidInput)
	}

	_, err := s.db.Exec(
		`INSERT INTO user_quotas (user_id, max_airports, max_endpoints, max_templates, xray_port_start, xray_port_end)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(user_id) DO UPDATE SET
		   max_airports = excluded.max_airports,
		   max_endpoints = excluded.max_endpoints,
		   max_templates = excluded.max_templates,
		   xray_port_start = excluded.xray_port_start,
		   xray_port_end = excluded.xray_port_end`,
		q.UserID, q.MaxAirports, q.MaxEndpoints, q.MaxTemplates, q.XrayPortStart, q.XrayPortEnd,
	)
	if err != nil {
		return fmt.Errorf("upsert user quota: %w", err)
	}
	return nil
}

// DeleteUserQuota removes the quota row for a user. Returns ErrNotFound when
// no row exists.
func (s *Store) DeleteUserQuota(userID int64) error {
	res, err := s.db.Exec(`DELETE FROM user_quotas WHERE user_id = ?`, userID)
	if err != nil {
		return fmt.Errorf("delete user quota: %w", err)
	}
	return checkAffected(res)
}

func scanUserQuotaFrom(r rowScanner) (*UserQuota, error) {
	var q UserQuota
	if err := r.Scan(
		&q.UserID, &q.MaxAirports, &q.MaxEndpoints, &q.XrayPortStart, &q.XrayPortEnd, &q.MaxTemplates,
	); err != nil {
		return nil, err
	}
	return &q, nil
}
