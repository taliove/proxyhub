package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// User role constants. Role transitions beyond this set require a schema or
// code change; do not introduce ad-hoc role strings.
const (
	RoleSuperAdmin = "super_admin"
	RoleUser       = "user"
)

// Honeypot usernames must stay in sync with internal/server/security.go's
// honeypotUsernames. New user accounts are forbidden from taking these names
// so any login attempt against them unambiguously indicates an attack.
var reservedUsernames = map[string]bool{
	"admin":         true,
	"administrator": true,
	"root":          true,
	"superuser":     true,
	"sysadmin":      true,
	"system":        true,
	"manager":       true,
	"test":          true,
	"guest":         true,
	"operator":      true,
	"webmaster":     true,
}

// Sentinel errors surfaced by the users CRUD surface.
var (
	ErrUsernameTaken    = errors.New("username already taken")
	ErrUsernameReserved = errors.New("username is reserved")
	ErrInvalidRole      = errors.New("invalid role")
	ErrInvalidInput     = errors.New("invalid input")
)

// User is the multi-tenant account record persisted in the users table.
type User struct {
	ID                 int64      `json:"id"`
	Username           string     `json:"username"`
	PassHash           string     `json:"-"` // never serialized
	Role               string     `json:"role"`
	MustChangePassword bool       `json:"must_change_password"`
	DisabledAt         *time.Time `json:"disabled_at,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	LastLoginAt        *time.Time `json:"last_login_at,omitempty"`
}

// Disabled reports whether the account has been disabled (DisabledAt set).
func (u *User) Disabled() bool {
	return u.DisabledAt != nil
}

// IsReservedUsername reports whether name is a honeypot/reserved username.
// Comparison is case-insensitive and trims surrounding whitespace.
func IsReservedUsername(name string) bool {
	return reservedUsernames[strings.ToLower(strings.TrimSpace(name))]
}

// validateUsername trims and checks the candidate username. Returns the
// normalized (trimmed) form on success.
func validateUsername(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", fmt.Errorf("%w: username is required", ErrInvalidInput)
	}
	if len(trimmed) > 64 {
		return "", fmt.Errorf("%w: username too long", ErrInvalidInput)
	}
	if IsReservedUsername(trimmed) {
		return "", ErrUsernameReserved
	}
	return trimmed, nil
}

// validateRole ensures role is one of the supported constants.
func validateRole(role string) error {
	if role != RoleSuperAdmin && role != RoleUser {
		return fmt.Errorf("%w: %q", ErrInvalidRole, role)
	}
	return nil
}

// CreateUser inserts a new user. passHash must already be a bcrypt hash;
// this layer never sees plaintext passwords. Returns the created row.
func (s *Store) CreateUser(username, passHash, role string, mustChangePassword bool) (*User, error) {
	name, err := validateUsername(username)
	if err != nil {
		return nil, err
	}
	if err := validateRole(role); err != nil {
		return nil, err
	}
	if passHash == "" {
		return nil, fmt.Errorf("%w: pass_hash is required", ErrInvalidInput)
	}

	res, err := s.db.Exec(
		`INSERT INTO users (username, pass_hash, role, must_change_password)
		 VALUES (?, ?, ?, ?)`,
		name, passHash, role, boolToInt(mustChangePassword),
	)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrUsernameTaken
		}
		return nil, fmt.Errorf("insert user: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("user last insert id: %w", err)
	}
	return s.GetUserByID(id)
}

// GetUserByUsername fetches a user by exact (case-sensitive) username.
// Returns ErrNotFound when missing.
func (s *Store) GetUserByUsername(username string) (*User, error) {
	return s.scanUser(s.db.QueryRow(
		`SELECT id, username, pass_hash, role, must_change_password,
		        disabled_at, created_at, last_login_at
		 FROM users WHERE username = ?`, username,
	))
}

// GetUserByID fetches a user by primary key. Returns ErrNotFound when missing.
func (s *Store) GetUserByID(id int64) (*User, error) {
	return s.scanUser(s.db.QueryRow(
		`SELECT id, username, pass_hash, role, must_change_password,
		        disabled_at, created_at, last_login_at
		 FROM users WHERE id = ?`, id,
	))
}

// UserUpdate holds the mutable fields of a User. Nil pointers leave the
// column untouched; PassHash empty string leaves the hash untouched.
type UserUpdate struct {
	PassHash           *string
	Role               *string
	MustChangePassword *bool
	LastLoginAt        *time.Time
}

// UpdateUser applies the non-nil fields in u to the user with the given id.
// Returns ErrNotFound when the id does not exist.
func (s *Store) UpdateUser(id int64, u UserUpdate) error {
	sets := []string{}
	args := []any{}

	if u.PassHash != nil {
		if *u.PassHash == "" {
			return fmt.Errorf("%w: pass_hash cannot be empty", ErrInvalidInput)
		}
		sets = append(sets, "pass_hash = ?")
		args = append(args, *u.PassHash)
	}
	if u.Role != nil {
		if err := validateRole(*u.Role); err != nil {
			return err
		}
		sets = append(sets, "role = ?")
		args = append(args, *u.Role)
	}
	if u.MustChangePassword != nil {
		sets = append(sets, "must_change_password = ?")
		args = append(args, boolToInt(*u.MustChangePassword))
	}
	if u.LastLoginAt != nil {
		sets = append(sets, "last_login_at = ?")
		args = append(args, *u.LastLoginAt)
	}
	if len(sets) == 0 {
		return fmt.Errorf("%w: no fields to update", ErrInvalidInput)
	}

	args = append(args, id)
	res, err := s.db.Exec(
		fmt.Sprintf(`UPDATE users SET %s WHERE id = ?`, strings.Join(sets, ", ")),
		args...,
	)
	if err != nil {
		return fmt.Errorf("update user: %w", err)
	}
	return checkAffected(res)
}

// DisableUser marks the account disabled at the given timestamp. Idempotent
// in effect but returns ErrNotFound for unknown ids.
func (s *Store) DisableUser(id int64, at time.Time) error {
	res, err := s.db.Exec(
		`UPDATE users SET disabled_at = ? WHERE id = ?`, at, id,
	)
	if err != nil {
		return fmt.Errorf("disable user: %w", err)
	}
	return checkAffected(res)
}

// EnableUser clears the disabled_at marker.
func (s *Store) EnableUser(id int64) error {
	res, err := s.db.Exec(
		`UPDATE users SET disabled_at = NULL WHERE id = ?`, id,
	)
	if err != nil {
		return fmt.Errorf("enable user: %w", err)
	}
	return checkAffected(res)
}

// DeleteUser removes ONLY the user row. This database does not enable
// PRAGMA foreign_keys (see users_cascade.go), so the ON DELETE CASCADE on
// user_quotas.user_id never fires: dependent rows survive as orphans.
// Production deletion must go through DeleteUserCascade; DeleteUser exists
// for tests that only need the row gone.
func (s *Store) DeleteUser(id int64) error {
	res, err := s.db.Exec(`DELETE FROM users WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	return checkAffected(res)
}

// ListUsers returns every user ordered by id.
func (s *Store) ListUsers() ([]*User, error) {
	rows, err := s.db.Query(
		`SELECT id, username, pass_hash, role, must_change_password,
		        disabled_at, created_at, last_login_at
		 FROM users ORDER BY id`,
	)
	if err != nil {
		return nil, fmt.Errorf("query users: %w", err)
	}
	defer rows.Close()

	var out []*User
	for rows.Next() {
		u, err := scanUserFrom(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// UserWithQuotaUsage aggregates a user row with its quota and current
// airport/endpoint usage counts. Used by the admin users list.
type UserWithQuotaUsage struct {
	User
	Quota          *UserQuota `json:"quota,omitempty"`
	AirportCount   int        `json:"airport_count"`
	EndpointCount  int        `json:"endpoint_count"`
}

// ListUsersWithQuotaUsage returns all users with their quota (if any) and
// current airport/endpoint usage. Tables that do not yet have a user_id
// column (pre ticket 06) contribute zero counts; LEFT JOINs keep the row.
func (s *Store) ListUsersWithQuotaUsage() ([]*UserWithQuotaUsage, error) {
	// The airport/endpoint subselects tolerate a missing user_id column by
	// returning zero; after ticket 06 lands they aggregate per user.
	airportHasUID := s.columnExistsUnlocked("airports", "user_id")
	endpointHasUID := s.columnExistsUnlocked("endpoints", "user_id")

	airportSub := `SELECT 0`
	if airportHasUID {
		airportSub = `(SELECT COUNT(*) FROM airports a WHERE a.user_id = u.id)`
	}
	endpointSub := `SELECT 0`
	if endpointHasUID {
		endpointSub = `(SELECT COUNT(*) FROM endpoints e WHERE e.user_id = u.id)`
	}

	q := fmt.Sprintf(
		`SELECT u.id, u.username, u.pass_hash, u.role, u.must_change_password,
		        u.disabled_at, u.created_at, u.last_login_at,
		        q.max_airports, q.max_endpoints, q.xray_port_start, q.xray_port_end,
		        %s, %s
		 FROM users u
		 LEFT JOIN user_quotas q ON q.user_id = u.id
		 ORDER BY u.id`, airportSub, endpointSub)

	rows, err := s.db.Query(q)
	if err != nil {
		return nil, fmt.Errorf("query users with quotas: %w", err)
	}
	defer rows.Close()

	var out []*UserWithQuotaUsage
	for rows.Next() {
		var u UserWithQuotaUsage
		var mustChange int
		var disabledAt, lastLoginAt sql.NullTime
		var maxAirports, maxEndpoints, portStart, portEnd sql.NullInt64

		if err := rows.Scan(
			&u.ID, &u.Username, &u.PassHash, &u.Role, &mustChange,
			&disabledAt, &u.CreatedAt, &lastLoginAt,
			&maxAirports, &maxEndpoints, &portStart, &portEnd,
			&u.AirportCount, &u.EndpointCount,
		); err != nil {
			return nil, err
		}
		u.MustChangePassword = mustChange != 0
		if disabledAt.Valid {
			t := disabledAt.Time
			u.DisabledAt = &t
		}
		if lastLoginAt.Valid {
			t := lastLoginAt.Time
			u.LastLoginAt = &t
		}
		if maxAirports.Valid {
			u.Quota = &UserQuota{
				UserID:        u.ID,
				MaxAirports:   int(maxAirports.Int64),
				MaxEndpoints:  int(maxEndpoints.Int64),
				XrayPortStart: int(portStart.Int64),
				XrayPortEnd:   int(portEnd.Int64),
			}
		}
		out = append(out, &u)
	}
	return out, rows.Err()
}

// columnExistsUnlocked reports whether the named column exists on the table.
// Internal helper; mirrors addColumnIfMissing's pragma inspection but read-only.
func (s *Store) columnExistsUnlocked(table, column string) bool {
	rows, err := s.db.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return false
		}
		if name == column {
			return true
		}
	}
	return false
}

func (s *Store) scanUser(row *sql.Row) (*User, error) {
	u, err := scanUserFrom(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return u, err
}

func scanUserFrom(r rowScanner) (*User, error) {
	var u User
	var mustChange int
	var disabledAt, lastLoginAt sql.NullTime
	if err := r.Scan(
		&u.ID, &u.Username, &u.PassHash, &u.Role, &mustChange,
		&disabledAt, &u.CreatedAt, &lastLoginAt,
	); err != nil {
		return nil, err
	}
	u.MustChangePassword = mustChange != 0
	if disabledAt.Valid {
		t := disabledAt.Time
		u.DisabledAt = &t
	}
	if lastLoginAt.Valid {
		t := lastLoginAt.Time
		u.LastLoginAt = &t
	}
	return &u, nil
}

// isUniqueViolation reports whether err is a SQLite UNIQUE constraint failure.
// modernc.org/sqlite surfaces this as an error string containing "UNIQUE".
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}
