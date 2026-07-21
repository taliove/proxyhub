package store

import (
	"context"
	"database/sql"
	"fmt"
)

// AirportWithTestRun extends Airport with latest test run info for list view.
type AirportWithTestRun struct {
	Airport
	LastTestScore  *float64 `json:"last_test_score,omitempty"`
	LastTestAt     *string  `json:"last_test_at,omitempty"`
	LastTestStatus *string  `json:"last_test_status,omitempty"`
}

// ListAirportsWithTestRuns lists all airports with their latest test run info.
// Uses a LEFT JOIN with subquery to fetch the most recent test run per airport,
// avoiding N+1 queries.
func (s *Store) ListAirportsWithTestRuns(ctx context.Context) ([]*AirportWithTestRun, error) {
	query := `
		SELECT
			a.id, a.name, a.url, a.abbr, a.enabled, a.created_at,
			atr.overall_score, atr.created_at, atr.status
		FROM airports a
		LEFT JOIN (
			SELECT airport_id, overall_score, created_at, status
			FROM airport_test_runs atr1
			WHERE id = (
				SELECT id FROM airport_test_runs atr2
				WHERE atr2.airport_id = atr1.airport_id
				ORDER BY created_at DESC, id DESC
				LIMIT 1
			)
		) atr ON a.id = atr.airport_id
		ORDER BY a.id DESC
	`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query airports with test runs: %w", err)
	}
	defer rows.Close()

	var airports []*AirportWithTestRun
	for rows.Next() {
		var awt AirportWithTestRun
		var enabled int
		var score sql.NullFloat64
		var testAt sql.NullString
		var status sql.NullString

		err := rows.Scan(
			&awt.ID, &awt.Name, &awt.URL, &awt.Abbr, &enabled, &awt.CreatedAt,
			&score, &testAt, &status,
		)
		if err != nil {
			return nil, fmt.Errorf("scan airport row: %w", err)
		}

		awt.Enabled = enabled == 1
		if score.Valid {
			awt.LastTestScore = &score.Float64
		}
		if testAt.Valid {
			awt.LastTestAt = &testAt.String
		}
		if status.Valid {
			awt.LastTestStatus = &status.String
		}

		airports = append(airports, &awt)
	}

	return airports, rows.Err()
}
