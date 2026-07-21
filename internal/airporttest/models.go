package airporttest

import (
	"context"
	"time"
)

// RunStatus represents the status of a test run.
type RunStatus string

const (
	StatusDiagnosing RunStatus = "diagnosing"
	StatusCompleted  RunStatus = "completed"
	StatusFailed     RunStatus = "failed"
)

// DiagnosticResult contains the outcome of the diagnostic phase.
type DiagnosticResult struct {
	HTTPStatus     int               `json:"http_status"`
	DurationMs     int64             `json:"duration_ms"`
	NodeCount      int               `json:"node_count"`
	ProtocolCounts map[string]int    `json:"protocol_counts"`
	ParseFailures  int               `json:"parse_failures"`
}

// TestRun represents a single test execution for an airport.
type TestRun struct {
	ID             int64             `json:"id"`
	AirportID      int64             `json:"airport_id"`
	CreatedAt      time.Time         `json:"created_at"`
	SampleParams   string            `json:"sample_params"`
	IsFull         bool              `json:"is_full"`
	Status         RunStatus         `json:"status"`
	OverallScore   *float64          `json:"overall_score,omitempty"`
	DimensionsJSON string            `json:"dimensions_json"`
	ErrorMessage   string            `json:"error_message,omitempty"`
}

// Orchestrator coordinates airport test execution.
type Orchestrator struct {
	store Store
}

// Store abstracts database operations for airport testing.
type Store interface {
	CreateTestRun(ctx context.Context, run *TestRun) (int64, error)
	GetTestRun(ctx context.Context, airportID, runID int64) (*TestRun, error)
}

// NewOrchestrator creates a new test orchestrator.
func NewOrchestrator(store Store) *Orchestrator {
	return &Orchestrator{
		store: store,
	}
}
