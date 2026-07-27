-- Per-user resource quotas. user_id references users(id) with CASCADE delete so
-- quota rows never outlive their owner.
-- xray_port_start/xray_port_end define the per-user Xray listen port range;
-- zero means "no per-user xray allocated yet" (assigned by ticket 08).
CREATE TABLE IF NOT EXISTS user_quotas (
    user_id          INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    max_airports     INTEGER NOT NULL DEFAULT 0,
    max_endpoints    INTEGER NOT NULL DEFAULT 0,
    xray_port_start  INTEGER NOT NULL DEFAULT 0,
    xray_port_end    INTEGER NOT NULL DEFAULT 0
);
