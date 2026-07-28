package store

import "fmt"

// migratePullLogStatus adds the pull outcome column to pull_logs (pull-guard
// ticket 01). Idempotent: safe on every startup.
//
//   - status defaults to 'ok' so rows written before this migration keep their
//     original meaning: until now only successfully delivered pulls were
//     recorded, so every legacy row is an 'ok' row.
//   - the (endpoint_id, status) index serves the per-endpoint IP detail view,
//     which groups by status, and the later guard queries that count recent
//     rate_limited rows for one endpoint.
//
// Must run after the base schema has created pull_logs.
func (s *Store) migratePullLogStatus() error {
	if err := s.addColumnIfMissing("pull_logs", "status",
		fmt.Sprintf("TEXT NOT NULL DEFAULT '%s'", PullStatusOK)); err != nil {
		return err
	}

	if _, err := s.db.Exec(
		`CREATE INDEX IF NOT EXISTS idx_pull_logs_endpoint_status ON pull_logs(endpoint_id, status)`,
	); err != nil {
		return fmt.Errorf("create idx_pull_logs_endpoint_status: %w", err)
	}

	return nil
}
