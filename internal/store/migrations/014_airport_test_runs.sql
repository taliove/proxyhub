-- Airport test runs: persist diagnostics and exam results per airport.
-- Retention: 90 days (aligned with audit/pull logs).
CREATE TABLE IF NOT EXISTS airport_test_runs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    airport_id INTEGER NOT NULL,
    created_at TIMESTAMP NOT NULL,
    sample_params TEXT NOT NULL DEFAULT '{}',
    is_full INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL,
    overall_score REAL,
    dimensions_json TEXT NOT NULL DEFAULT '{}',
    error_message TEXT,
    FOREIGN KEY(airport_id) REFERENCES airports(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_airport_test_runs_airport ON airport_test_runs(airport_id, id DESC);
CREATE INDEX IF NOT EXISTS idx_airport_test_runs_created ON airport_test_runs(created_at);
