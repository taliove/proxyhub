-- 019_user_id_backfill.sql — ticket 06 (data model multi-tenancy, expand phase).
--
-- Adds a user_id column (INTEGER NOT NULL DEFAULT 0) plus an index to every
-- business table that is scoped per user in the multi-tenant model. The
-- default 0 keeps pre-existing rows in the "unowned" bucket so legacy reads
-- keep returning them; the expand phase explicitly does NOT break existing
-- behavior.
--
-- REFERENCE ONLY — do NOT wire this file into applyMigrationFile's switch.
-- The live executor is migrateMultiTenant (multi_tenant.go): SQLite has no
-- "ALTER TABLE ADD COLUMN IF NOT EXISTS", so column adds are guarded by
-- addColumnIfMissing on the Go side. This file is the canonical schema
-- reference; keep it in sync with the Go side when either changes.
--
-- The "template" table referenced below is the dedicated per-user template
-- table introduced by ticket 06 (the legacy clash_template settings key
-- remains as the global default; per-user overrides land in this table).

-- Per-user template table (user-owned overrides of the global clash template).
CREATE TABLE IF NOT EXISTS template (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER NOT NULL DEFAULT 0,
    name       TEXT NOT NULL DEFAULT '',
    content    TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- user_id columns + indexes (one per business table).
-- On a fresh database the columns are added here directly; on existing
-- databases the Go-side addColumnIfMissing applies the same declarations
-- incrementally (both paths converge on the same final schema).
ALTER TABLE endpoints           ADD COLUMN user_id INTEGER NOT NULL DEFAULT 0;
ALTER TABLE airports            ADD COLUMN user_id INTEGER NOT NULL DEFAULT 0;
ALTER TABLE self_hosted_nodes   ADD COLUMN user_id INTEGER NOT NULL DEFAULT 0;
ALTER TABLE nodes               ADD COLUMN user_id INTEGER NOT NULL DEFAULT 0;
ALTER TABLE node_blocks         ADD COLUMN user_id INTEGER NOT NULL DEFAULT 0;
ALTER TABLE node_overrides      ADD COLUMN user_id INTEGER NOT NULL DEFAULT 0;
ALTER TABLE node_health         ADD COLUMN user_id INTEGER NOT NULL DEFAULT 0;
ALTER TABLE node_tags           ADD COLUMN user_id INTEGER NOT NULL DEFAULT 0;
ALTER TABLE refresh_runs        ADD COLUMN user_id INTEGER NOT NULL DEFAULT 0;
ALTER TABLE pull_logs           ADD COLUMN user_id INTEGER NOT NULL DEFAULT 0;
ALTER TABLE jobs                ADD COLUMN user_id INTEGER NOT NULL DEFAULT 0;
ALTER TABLE exam_history        ADD COLUMN user_id INTEGER NOT NULL DEFAULT 0;
ALTER TABLE airport_test_runs   ADD COLUMN user_id INTEGER NOT NULL DEFAULT 0;
ALTER TABLE speedtest_results   ADD COLUMN user_id INTEGER NOT NULL DEFAULT 0;
ALTER TABLE audit_logs          ADD COLUMN user_id INTEGER NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_endpoints_user_id           ON endpoints(user_id);
CREATE INDEX IF NOT EXISTS idx_airports_user_id            ON airports(user_id);
CREATE INDEX IF NOT EXISTS idx_self_hosted_nodes_user_id   ON self_hosted_nodes(user_id);
CREATE INDEX IF NOT EXISTS idx_nodes_user_id               ON nodes(user_id);
CREATE INDEX IF NOT EXISTS idx_node_blocks_user_id         ON node_blocks(user_id);
CREATE INDEX IF NOT EXISTS idx_node_overrides_user_id      ON node_overrides(user_id);
CREATE INDEX IF NOT EXISTS idx_node_health_user_id         ON node_health(user_id);
CREATE INDEX IF NOT EXISTS idx_node_tags_user_id           ON node_tags(user_id);
CREATE INDEX IF NOT EXISTS idx_refresh_runs_user_id        ON refresh_runs(user_id);
CREATE INDEX IF NOT EXISTS idx_pull_logs_user_id           ON pull_logs(user_id);
CREATE INDEX IF NOT EXISTS idx_jobs_user_id                ON jobs(user_id);
CREATE INDEX IF NOT EXISTS idx_exam_history_user_id        ON exam_history(user_id);
CREATE INDEX IF NOT EXISTS idx_airport_test_runs_user_id   ON airport_test_runs(user_id);
CREATE INDEX IF NOT EXISTS idx_speedtest_results_user_id   ON speedtest_results(user_id);
CREATE INDEX IF NOT EXISTS idx_audit_logs_user_id          ON audit_logs(user_id);
CREATE INDEX IF NOT EXISTS idx_template_user_id            ON template(user_id);

-- Backfill: assign every pre-existing row to the first super_admin.
-- The users table is owned by ticket 01; this UPDATE is a no-op when the
-- users table is empty (fresh install before ticket 01 seeds the admin),
-- which is correct: 0 stays as the "unowned" marker until the super admin
-- exists, and BackfillUserID on the Go side re-runs safely at startup.
UPDATE endpoints           SET user_id = (SELECT id FROM users WHERE role = 'super_admin' ORDER BY id ASC LIMIT 1) WHERE user_id = 0 AND EXISTS (SELECT 1 FROM users WHERE role = 'super_admin');
UPDATE airports            SET user_id = (SELECT id FROM users WHERE role = 'super_admin' ORDER BY id ASC LIMIT 1) WHERE user_id = 0 AND EXISTS (SELECT 1 FROM users WHERE role = 'super_admin');
UPDATE self_hosted_nodes   SET user_id = (SELECT id FROM users WHERE role = 'super_admin' ORDER BY id ASC LIMIT 1) WHERE user_id = 0 AND EXISTS (SELECT 1 FROM users WHERE role = 'super_admin');
UPDATE nodes               SET user_id = (SELECT id FROM users WHERE role = 'super_admin' ORDER BY id ASC LIMIT 1) WHERE user_id = 0 AND EXISTS (SELECT 1 FROM users WHERE role = 'super_admin');
UPDATE node_blocks         SET user_id = (SELECT id FROM users WHERE role = 'super_admin' ORDER BY id ASC LIMIT 1) WHERE user_id = 0 AND EXISTS (SELECT 1 FROM users WHERE role = 'super_admin');
UPDATE node_overrides      SET user_id = (SELECT id FROM users WHERE role = 'super_admin' ORDER BY id ASC LIMIT 1) WHERE user_id = 0 AND EXISTS (SELECT 1 FROM users WHERE role = 'super_admin');
UPDATE node_health         SET user_id = (SELECT id FROM users WHERE role = 'super_admin' ORDER BY id ASC LIMIT 1) WHERE user_id = 0 AND EXISTS (SELECT 1 FROM users WHERE role = 'super_admin');
UPDATE node_tags           SET user_id = (SELECT id FROM users WHERE role = 'super_admin' ORDER BY id ASC LIMIT 1) WHERE user_id = 0 AND EXISTS (SELECT 1 FROM users WHERE role = 'super_admin');
UPDATE refresh_runs        SET user_id = (SELECT id FROM users WHERE role = 'super_admin' ORDER BY id ASC LIMIT 1) WHERE user_id = 0 AND EXISTS (SELECT 1 FROM users WHERE role = 'super_admin');
UPDATE pull_logs           SET user_id = (SELECT id FROM users WHERE role = 'super_admin' ORDER BY id ASC LIMIT 1) WHERE user_id = 0 AND EXISTS (SELECT 1 FROM users WHERE role = 'super_admin');
UPDATE jobs                SET user_id = (SELECT id FROM users WHERE role = 'super_admin' ORDER BY id ASC LIMIT 1) WHERE user_id = 0 AND EXISTS (SELECT 1 FROM users WHERE role = 'super_admin');
UPDATE exam_history        SET user_id = (SELECT id FROM users WHERE role = 'super_admin' ORDER BY id ASC LIMIT 1) WHERE user_id = 0 AND EXISTS (SELECT 1 FROM users WHERE role = 'super_admin');
UPDATE airport_test_runs   SET user_id = (SELECT id FROM users WHERE role = 'super_admin' ORDER BY id ASC LIMIT 1) WHERE user_id = 0 AND EXISTS (SELECT 1 FROM users WHERE role = 'super_admin');
UPDATE speedtest_results   SET user_id = (SELECT id FROM users WHERE role = 'super_admin' ORDER BY id ASC LIMIT 1) WHERE user_id = 0 AND EXISTS (SELECT 1 FROM users WHERE role = 'super_admin');
UPDATE audit_logs          SET user_id = (SELECT id FROM users WHERE role = 'super_admin' ORDER BY id ASC LIMIT 1) WHERE user_id = 0 AND EXISTS (SELECT 1 FROM users WHERE role = 'super_admin');
UPDATE template            SET user_id = (SELECT id FROM users WHERE role = 'super_admin' ORDER BY id ASC LIMIT 1) WHERE user_id = 0 AND EXISTS (SELECT 1 FROM users WHERE role = 'super_admin');

-- Settings split: global settings move to system_settings; per-user
-- key/value pairs live in user_settings. The legacy settings table is kept
-- in place (reads/writes are redirected on the Go side); it is dropped in
-- the contract phase, not in expand.
CREATE TABLE IF NOT EXISTS system_settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS user_settings (
    user_id INTEGER NOT NULL,
    key     TEXT NOT NULL,
    value   TEXT NOT NULL,
    PRIMARY KEY (user_id, key)
);
CREATE INDEX IF NOT EXISTS idx_user_settings_user_id ON user_settings(user_id);

-- Migrate legacy rows: everything currently in settings is global by
-- definition (pre-multi-tenant there was exactly one admin), so it all
-- lands in system_settings. INSERT OR IGNORE keeps the operation idempotent.
INSERT OR IGNORE INTO system_settings (key, value) SELECT key, value FROM settings;
