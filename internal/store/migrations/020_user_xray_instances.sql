-- 020: Per-user Xray instance management (ticket 08)
-- Each user owns an independent Xray process with its own generated config and
-- dedicated inbound port. Status/PID are persisted so restarts can detect stale
-- state and reconcile (kill orphan processes, mark interrupted instances).
--
-- user_id is UNIQUE: exactly one Xray instance per user.
-- port is the allocated loopback SOCKS/HTTP listen port (unique per live instance).
-- config_path points at var/xray/<userID>/xray_config.json (managed by xraymgr).
-- status lifecycle: stopped -> starting -> running -> stopped | failed.

CREATE TABLE IF NOT EXISTS user_xray_instances (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id         INTEGER NOT NULL UNIQUE,
    port            INTEGER NOT NULL,
    config_path     TEXT NOT NULL DEFAULT '',
    pid             INTEGER NOT NULL DEFAULT 0,
    status          TEXT NOT NULL DEFAULT 'stopped',
    last_started_at TIMESTAMP,
    created_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_user_xray_instances_status ON user_xray_instances(status);
CREATE INDEX IF NOT EXISTS idx_user_xray_instances_port ON user_xray_instances(port);
