package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// AirportTestRun represents a test run record.
type AirportTestRun struct {
	ID             int64     `json:"id"`
	AirportID      int64     `json:"airport_id"`
	CreatedAt      time.Time `json:"created_at"`
	SampleParams   string    `json:"sample_params"`
	IsFull         bool      `json:"is_full"`
	Status         string    `json:"status"`
	OverallScore   *float64  `json:"overall_score,omitempty"`
	DimensionsJSON string    `json:"dimensions_json"`
	ErrorMessage   string    `json:"error_message,omitempty"`
}

// CreateAirportTestRun inserts a new test run.
func (s *Store) CreateAirportTestRun(ctx context.Context, run *AirportTestRun) (int64, error) {
	// Use explicit UTC format for created_at (see ADR 0010 re: modernc.org/sqlite datetime compatibility)
	createdStr := run.CreatedAt.UTC().Format("2006-01-02 15:04:05")

	result, err := s.db.ExecContext(ctx,
		`INSERT INTO airport_test_runs
		(airport_id, created_at, sample_params, is_full, status, overall_score, dimensions_json, error_message)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		run.AirportID, createdStr, run.SampleParams, boolToInt(run.IsFull),
		run.Status, run.OverallScore, run.DimensionsJSON, run.ErrorMessage)
	if err != nil {
		return 0, fmt.Errorf("insert test run: %w", err)
	}

	id, _ := result.LastInsertId()
	return id, nil
}

// GetAirportTestRun retrieves a test run by airport ID and run ID.
func (s *Store) GetAirportTestRun(ctx context.Context, airportID, runID int64) (*AirportTestRun, error) {
	var run AirportTestRun
	var isFull int
	err := s.db.QueryRowContext(ctx,
		`SELECT id, airport_id, created_at, sample_params, is_full, status, overall_score, dimensions_json, error_message
		FROM airport_test_runs
		WHERE airport_id = ? AND id = ?`,
		airportID, runID).Scan(
		&run.ID, &run.AirportID, &run.CreatedAt, &run.SampleParams, &isFull,
		&run.Status, &run.OverallScore, &run.DimensionsJSON, &run.ErrorMessage)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query test run: %w", err)
	}
	run.IsFull = isFull == 1
	return &run, nil
}

// UpdateAirportTestRun updates an existing test run.
func (s *Store) UpdateAirportTestRun(ctx context.Context, run *AirportTestRun) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE airport_test_runs
		SET status = ?, overall_score = ?, dimensions_json = ?, error_message = ?
		WHERE id = ?`,
		run.Status, run.OverallScore, run.DimensionsJSON, run.ErrorMessage, run.ID)
	if err != nil {
		return fmt.Errorf("update test run: %w", err)
	}
	return nil
}

// PruneAirportTestRuns deletes test runs older than specified time (90-day retention).
func (s *Store) PruneAirportTestRuns(olderThan time.Time) error {
	cutoff := olderThan.UTC().Format("2006-01-02 15:04:05")
	_, err := s.db.ExecContext(context.Background(),
		`DELETE FROM airport_test_runs WHERE datetime(created_at) < datetime(?)`, cutoff)
	if err != nil {
		return fmt.Errorf("prune airport test runs: %w", err)
	}
	return nil
}
