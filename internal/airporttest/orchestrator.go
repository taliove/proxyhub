package airporttest

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/taliove/proxyhub/internal/subscription"
)

const subscriptionUserAgent = "v2rayN/6.23"

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

	// Fetch raw subscription
	req, err := http.NewRequest(http.MethodGet, airportURL, nil)
	if err != nil {
		return o.persistFailedRun(ctx, run, start, fmt.Errorf("build request: %w", err))
	}
	req.Header.Set("User-Agent", subscriptionUserAgent)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return o.persistFailedRun(ctx, run, start, fmt.Errorf("fetch failed: %w", err))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return o.persistFailedRun(ctx, run, start, fmt.Errorf("HTTP %d", resp.StatusCode))
	}

	body, err := io.ReadAll(resp.Body)
	elapsed := time.Since(start)
	if err != nil {
		return o.persistFailedRun(ctx, run, start, fmt.Errorf("read body: %w", err))
	}

	// Decode base64 if needed
	decoded, err := base64.RawStdEncoding.DecodeString(string(body))
	if err != nil {
		decoded, err = base64.StdEncoding.DecodeString(string(body))
		if err != nil {
			decoded = body
		}
	}

	// Parse with stats
	parseResult := subscription.ParseWithStats(string(decoded), airportName)

	if len(parseResult.Nodes) == 0 && parseResult.ParseFailures == parseResult.TotalLines {
		return o.persistFailedRun(ctx, run, start, fmt.Errorf("no valid nodes found"))
	}

	// Count protocols
	protocolCounts := make(map[string]int)
	for _, node := range parseResult.Nodes {
		protocolCounts[node.Type]++
	}

	diag := DiagnosticResult{
		HTTPStatus:     resp.StatusCode,
		DurationMs:     elapsed.Milliseconds(),
		NodeCount:      len(parseResult.Nodes),
		ProtocolCounts: protocolCounts,
		ParseFailures:  parseResult.ParseFailures,
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

func (o *Orchestrator) persistFailedRun(ctx context.Context, run *TestRun, start time.Time, err error) (*TestRun, error) {
	run.Status = StatusFailed
	run.ErrorMessage = err.Error()
	dimJSON, _ := json.Marshal(DiagnosticResult{
		HTTPStatus: 0,
		DurationMs: time.Since(start).Milliseconds(),
	})
	run.DimensionsJSON = string(dimJSON)
	id, dbErr := o.store.CreateTestRun(ctx, run)
	if dbErr != nil {
		return nil, fmt.Errorf("persist failed run: %w", dbErr)
	}
	run.ID = id
	return run, nil
}
