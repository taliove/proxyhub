-- Users table: multi-tenant account records.
-- role is constrained to 'super_admin' or 'user' at the Go layer (CHECK would
-- break future role extensions without a table rebuild).
-- username is UNIQUE and case-insensitive comparisons are done in Go before insert.
CREATE TABLE IF NOT EXISTS users (
    id                   INTEGER PRIMARY KEY AUTOINCREMENT,
    username             TEXT NOT NULL UNIQUE,
    pass_hash            TEXT NOT NULL,
    role                 TEXT NOT NULL,
    must_change_password INTEGER NOT NULL DEFAULT 0,
    disabled_at          TIMESTAMP,
    created_at           TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_login_at        TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_users_role ON users(role);
