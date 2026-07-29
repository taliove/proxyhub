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
	return s.listAirportsWithTestRuns(ctx, 0)
}

// ListAirportsWithTestRunsByUser lists the given user's airports with latest
// test run info (ticket 07). Test run rows are joined per airport, so the
// per-user filter on airports already scopes the joined runs.
func (s *Store) ListAirportsWithTestRunsByUser(ctx context.Context, userID int64) ([]*AirportWithTestRun, error) {
	return s.listAirportsWithTestRuns(ctx, userID)
}

func (s *Store) listAirportsWithTestRuns(ctx context.Context, userID int64) ([]*AirportWithTestRun, error) {
	query := `
		SELECT
			a.id, a.name, a.url, a.abbr, a.enabled, a.created_at, a.user_id,
			a.source_type, a.usage_upload, a.usage_download, a.usage_total, a.usage_expire, a.web_page_url,
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
	`
	args := []any{}
	if userID > 0 {
		query += ` WHERE a.user_id = ?`
		args = append(args, userID)
	}
	query += ` ORDER BY a.id DESC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query airports with test runs: %w", err)
	}
	defer rows.Close()

	var airports []*AirportWithTestRun
	for rows.Next() {
		var awt AirportWithTestRun
		var score sql.NullFloat64
		var testAt sql.NullString
		var status sql.NullString

		err := scanJoinedAirportRow(rows, &awt, &score, &testAt, &status)
		if err != nil {
			return nil, fmt.Errorf("scan airport row: %w", err)
		}
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

// scanJoinedAirportRow 扫描 listAirportsWithTestRuns 的联表行:
// 前 13 列与 airportColumns 同序,后 3 列为最近测试 run 的 LEFT JOIN 字段(可空)。
func scanJoinedAirportRow(rows *sql.Rows, awt *AirportWithTestRun, score *sql.NullFloat64, testAt, status *sql.NullString) error {
	var enabled int
	err := rows.Scan(
		&awt.ID, &awt.Name, &awt.URL, &awt.Abbr, &enabled, &awt.CreatedAt, &awt.UserID,
		&awt.SourceType, &awt.UsageUpload, &awt.UsageDownload, &awt.UsageTotal, &awt.UsageExpire, &awt.WebPageURL,
		score, testAt, status,
	)
	if err != nil {
		return err
	}
	awt.Enabled = enabled == 1
	return nil
}
