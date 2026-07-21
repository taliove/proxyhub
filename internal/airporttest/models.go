package airporttest

import (
	"context"
	"time"

	"github.com/taliove/proxyhub/internal/subscription"
)

// RunStatus represents the status of a test run.
type RunStatus string

const (
	StatusDiagnosing RunStatus = "diagnosing"
	StatusChecking   RunStatus = "checking"
	StatusScoring    RunStatus = "scoring"
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
	store         Store
	healthChecker HealthChecker
	poolWriter    PoolWriter
	poolOps       PoolOperations // for pool-aware logic
}

// HealthChecker abstracts health check operations (for testing).
type HealthChecker interface {
	CheckAll(ctx context.Context, nodes []*subscription.Node) []*HealthCheckResult
}

// HealthCheckResult represents single node health check outcome.
type HealthCheckResult struct {
	Node      *subscription.Node
	Available bool
	Latency   int
	Error     error
}

// PoolWriter abstracts node pool write operations (复用全局健康检查写回路径).
type PoolWriter interface {
	UpdateNodeTestResult(nodeKey, mode string, available bool, latency int, downMbps, upMbps float64) bool
}

// PoolOperations abstracts pool loading and single-airport upsert for pool-aware testing.
type PoolOperations interface {
	// LoadPoolBySource returns nodes in the pool matching the given source (airport name).
	LoadPoolBySource(source string) ([]*subscription.Node, error)
	// UpsertAirportNodes merges fetched nodes into pool (single-airport scope).
	UpsertAirportNodes(airportName string, fetchedNodes []*subscription.Node) error
}

// Store abstracts database operations for airport testing.
type Store interface {
	CreateTestRun(ctx context.Context, run *TestRun) (int64, error)
	GetTestRun(ctx context.Context, airportID, runID int64) (*TestRun, error)
	UpdateTestRun(ctx context.Context, run *TestRun) error
}

// NewOrchestrator creates a new test orchestrator.
func NewOrchestrator(store Store, healthChecker HealthChecker, poolWriter PoolWriter) *Orchestrator {
	return &Orchestrator{
		store:         store,
		healthChecker: healthChecker,
		poolWriter:    poolWriter,
		poolOps:       nil, // will be set by handler wiring
	}
}

// NewOrchestratorWithPoolOps creates orchestrator with pool operations (for testing and pool-aware mode).
func NewOrchestratorWithPoolOps(store Store, healthChecker HealthChecker, poolWriter PoolWriter, poolOps PoolOperations) *Orchestrator {
	return &Orchestrator{
		store:         store,
		healthChecker: healthChecker,
		poolWriter:    poolWriter,
		poolOps:       poolOps,
	}
}
