package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// XrayInstance status values (ticket 08 per-user Xray lifecycle).
const (
	XrayStatusStopped = "stopped"
	XrayStatusRunning = "running"
	XrayStatusFailed  = "failed"
)

// XrayInstance represents one user's dedicated Xray process state.
// Exactly one row exists per user (user_id UNIQUE). Port is the loopback
// listen port allocated from the user's quota range; PID is recorded so
// restarts can detect stale/orphaned processes.
type XrayInstance struct {
	ID            int64      `json:"id"`
	UserID        int64      `json:"user_id"`
	Port          int        `json:"port"`
	ConfigPath    string     `json:"config_path"`
	PID           int        `json:"pid"`
	Status        string     `json:"status"`
	LastStartedAt *time.Time `json:"last_started_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// CreateOrUpdateXrayInstance upserts the per-user Xray instance row.
// user_id is the natural key: an existing row for the same user is updated
// (port/config_path preserved unless overwritten by non-zero values).
func (s *Store) CreateOrUpdateXrayInstance(inst *XrayInstance) error {
	if inst.UserID <= 0 {
		return errors.New("user_id is required")
	}
	if inst.Status == "" {
		inst.Status = XrayStatusStopped
	}
	_, err := s.db.Exec(`
		INSERT INTO user_xray_instances (user_id, port, config_path, pid, status, last_started_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET
			port = excluded.port,
			config_path = excluded.config_path,
			pid = excluded.pid,
			status = excluded.status,
			last_started_at = excluded.last_started_at,
			updated_at = CURRENT_TIMESTAMP
	`, inst.UserID, inst.Port, inst.ConfigPath, inst.PID, inst.Status, timeOrNil(inst.LastStartedAt))
	if err != nil {
		return fmt.Errorf("upsert user_xray_instances: %w", err)
	}
	return nil
}

// GetXrayInstanceByUserID fetches the instance row for a user.
// Returns ErrNotFound when the user has no instance yet.
func (s *Store) GetXrayInstanceByUserID(userID int64) (*XrayInstance, error) {
	row := s.db.QueryRow(`
		SELECT id, user_id, port, config_path, pid, status, last_started_at, created_at, updated_at
		FROM user_xray_instances WHERE user_id = ?
	`, userID)
	return scanXrayInstance(row)
}

// UpdateXrayInstanceStatus updates status/pid/last_started_at for a user.
// Pass startedAt=zero to leave last_started_at unchanged (e.g. stopping).
func (s *Store) UpdateXrayInstanceStatus(userID int64, status string, pid int, startedAt time.Time) error {
	var lastStarted interface{}
	if !startedAt.IsZero() {
		lastStarted = startedAt
	}
	res, err := s.db.Exec(`
		UPDATE user_xray_instances
		SET status = ?, pid = ?,
		    last_started_at = COALESCE(?, last_started_at),
		    updated_at = CURRENT_TIMESTAMP
		WHERE user_id = ?
	`, status, pid, lastStarted, userID)
	if err != nil {
		return fmt.Errorf("update user_xray_instances status: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteXrayInstance removes the user's instance row. Idempotent: missing
// rows are silently ignored (cleanup paths should not fail when state is
// already gone).
func (s *Store) DeleteXrayInstance(userID int64) error {
	_, err := s.db.Exec(`DELETE FROM user_xray_instances WHERE user_id = ?`, userID)
	if err != nil {
		return fmt.Errorf("delete user_xray_instances: %w", err)
	}
	return nil
}

// ListXrayInstances returns all rows ordered by user_id (deterministic for
// tests and for the admin overview).
func (s *Store) ListXrayInstances() ([]*XrayInstance, error) {
	rows, err := s.db.Query(`
		SELECT id, user_id, port, config_path, pid, status, last_started_at, created_at, updated_at
		FROM user_xray_instances ORDER BY user_id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list user_xray_instances: %w", err)
	}
	defer rows.Close()

	var out []*XrayInstance
	for rows.Next() {
		inst, err := scanXrayInstanceRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, inst)
	}
	return out, rows.Err()
}

// ListUsedXrayPorts returns every port currently assigned to any user
// instance. Used by the port allocator to avoid conflicts (ticket 08).
func (s *Store) ListUsedXrayPorts() ([]int, error) {
	rows, err := s.db.Query(`SELECT port FROM user_xray_instances`)
	if err != nil {
		return nil, fmt.Errorf("list used xray ports: %w", err)
	}
	defer rows.Close()

	var ports []int
	for rows.Next() {
		var p int
		if err := rows.Scan(&p); err != nil {
			return nil, fmt.Errorf("scan port: %w", err)
		}
		ports = append(ports, p)
	}
	return ports, rows.Err()
}

// scanXrayInstance reads a single QueryRow result.
func scanXrayInstance(row *sql.Row) (*XrayInstance, error) {
	var inst XrayInstance
	var lastStarted sql.NullTime
	err := row.Scan(
		&inst.ID, &inst.UserID, &inst.Port, &inst.ConfigPath,
		&inst.PID, &inst.Status, &lastStarted,
		&inst.CreatedAt, &inst.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan xray instance: %w", err)
	}
	if lastStarted.Valid {
		t := lastStarted.Time
		inst.LastStartedAt = &t
	}
	return &inst, nil
}

// scanXrayInstanceRows reads a single row from a *sql.Rows cursor.
func scanXrayInstanceRows(rows *sql.Rows) (*XrayInstance, error) {
	var inst XrayInstance
	var lastStarted sql.NullTime
	if err := rows.Scan(
		&inst.ID, &inst.UserID, &inst.Port, &inst.ConfigPath,
		&inst.PID, &inst.Status, &lastStarted,
		&inst.CreatedAt, &inst.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("scan xray instance row: %w", err)
	}
	if lastStarted.Valid {
		t := lastStarted.Time
		inst.LastStartedAt = &t
	}
	return &inst, nil
}

// timeOrNil converts an optional *time.Time to a driver value (NULL when nil).
func timeOrNil(t *time.Time) interface{} {
	if t == nil {
		return nil
	}
	return *t
}
