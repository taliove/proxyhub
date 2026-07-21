package airporttest

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// RunDiagnostic executes diagnostic phase for an airport.
// Returns the created run with diagnostic results. Does not modify node pool.
func (o *Orchestrator) RunDiagnostic(ctx context.Context, airportID int64, airportName, airportURL string, isFull bool) (*TestRun, error) {
	run := &TestRun{
		AirportID:    airportID,
		CreatedAt:    time.Now().UTC(),
		SampleParams: "{}",
		IsFull:       isFull,
		Status:       StatusDiagnosing,
	}

	start := time.Now()
	sub, err := o.fetcher.Fetch(airportName, airportURL)
	elapsed := time.Since(start)

	if err != nil {
		run.Status = StatusFailed
		run.ErrorMessage = fmt.Sprintf("fetch failed: %v", err)
		dimJSON, _ := json.Marshal(DiagnosticResult{
			HTTPStatus: 0,
			DurationMs: elapsed.Milliseconds(),
		})
		run.DimensionsJSON = string(dimJSON)
		id, dbErr := o.store.CreateTestRun(ctx, run)
		if dbErr != nil {
			return nil, fmt.Errorf("persist failed run: %w", dbErr)
		}
		run.ID = id
		return run, nil
	}

	// Count protocols
	protocolCounts := make(map[string]int)
	for _, node := range sub.Nodes {
		protocolCounts[node.Type]++
	}

	diag := DiagnosticResult{
		HTTPStatus:     200,
		DurationMs:     elapsed.Milliseconds(),
		NodeCount:      len(sub.Nodes),
		ProtocolCounts: protocolCounts,
		ParseFailures:  0,
	}

	dimJSON, _ := json.Marshal(diag)
	run.Status = StatusCompleted
	run.DimensionsJSON = string(dimJSON)

	id, err := o.store.CreateTestRun(ctx, run)
	if err != nil {
		return nil, fmt.Errorf("persist run: %w", err)
	}
	run.ID = id
	return run, nil
}
