package airporttest

import (
	"context"
	"testing"
	"time"

	"github.com/taliove/proxyhub/internal/subscription"
)

// TestRunTestWithPool_PoolHasNodes_URLReachable tests Branch A: pool has nodes, URL reachable.
// Expected: use diagnostic result, test pool nodes, fetch health 10% included.
func TestRunTestWithPool_PoolHasNodes_URLReachable(t *testing.T) {
	store := NewFakeStore(t)

	// Health checker marks all as available
	checker := &FakeHealthChecker{
		Results: nil, // Will use default: all available, latency 100ms
	}
	writer := &FakePoolWriter{}
	orch := NewOrchestrator(store, checker, writer)

	// Pool has 5 HK nodes
	poolNodes := make([]*subscription.Node, 5)
	for i := range poolNodes {
		poolNodes[i] = &subscription.Node{
			Name:   "HK-Node",
			Server: "1.1.1.1",
			Port:   10000 + i,
			Region: "HK",
			Source: "TestAirport",
		}
	}

	run := &TestRun{
		ID:        1,
		AirportID: 1,
		CreatedAt: time.Now(),
		Status:    StatusDiagnosing,
		IsFull:    false,
	}
	store.Runs[1] = run

	// Diagnostic succeeded (URL reachable)
	diagResult := &DiagnosticResult{
		HTTPStatus:    200,
		ParseFailures: 0,
		NodeCount:     5,
	}

	updatedRun, err := orch.RunTest(context.Background(), run, "TestAirport", poolNodes, diagResult, nil)
	if err != nil {
		t.Fatalf("RunTest failed: %v", err)
	}

	if updatedRun.Status != StatusCompleted {
		t.Errorf("status got %s, want %s", updatedRun.Status, StatusCompleted)
	}

	// Should have sampled and written back (5 nodes, all sampled for HK priority region)
	if len(writer.Updates) != 5 {
		t.Errorf("writeback count got %d, want 5", len(writer.Updates))
	}

	// Overall score should include fetch health (10%)
	if updatedRun.OverallScore == nil {
		t.Fatal("OverallScore is nil")
	}
	// With all available (from health checker), good latency (100ms), should be high
	// Availability 50 + Latency ~30 + FetchHealth 10 + Region 2 = ~92
	if *updatedRun.OverallScore < 80 {
		t.Errorf("overall score got %.2f, should be > 80 with all components", *updatedRun.OverallScore)
	}
}

// TestRunTestWithPool_PoolHasNodes_URLUnreachable tests Branch A: pool has nodes, URL unreachable.
// Expected: diagnostic fails but run continues, test pool nodes, fetch health N/A with renormalized weights.
func TestRunTestWithPool_PoolHasNodes_URLUnreachable(t *testing.T) {
	store := NewFakeStore(t)
	checker := &FakeHealthChecker{}
	writer := &FakePoolWriter{}
	orch := NewOrchestrator(store, checker, writer)

	// Pool has 5 HK nodes
	poolNodes := make([]*subscription.Node, 5)
	for i := range poolNodes {
		poolNodes[i] = &subscription.Node{
			Name:      "HK-Node",
			Server:    "1.1.1.1",
			Port:      10000 + i,
			Region:    "HK",
			Source:    "TestAirport",
			Available: true,
			Latency:   100,
		}
	}

	run := &TestRun{
		ID:        1,
		AirportID: 1,
		CreatedAt: time.Now(),
		Status:    StatusDiagnosing,
		IsFull:    false,
	}
	store.Runs[1] = run

	// Diagnostic failed (URL unreachable: HTTP 404 or fetch error)
	diagResult := &DiagnosticResult{
		HTTPStatus:    404,
		ParseFailures: 0,
		NodeCount:     0,
	}

	updatedRun, err := orch.RunTest(context.Background(), run, "TestAirport", poolNodes, diagResult, nil)
	if err != nil {
		t.Fatalf("RunTest failed: %v", err)
	}

	if updatedRun.Status != StatusCompleted {
		t.Errorf("status got %s, want %s", updatedRun.Status, StatusCompleted)
	}

	// Should still test pool nodes
	if len(writer.Updates) != 5 {
		t.Errorf("writeback count got %d, want 5", len(writer.Updates))
	}

	// Overall score should use renormalized weights (no fetch health 10%)
	// availability 5/9, latency 3/9, region 1/9
	if updatedRun.OverallScore == nil {
		t.Fatal("OverallScore is nil")
	}
	// With good availability and latency, should still score high (>70)
	if *updatedRun.OverallScore < 70 {
		t.Errorf("overall score got %.2f, should be > 70 with renormalized weights", *updatedRun.OverallScore)
	}
}

// TestRunTestWithPool_PoolEmpty_URLReachable tests Branch B: pool empty, URL reachable.
// Expected: upsert fetched nodes into pool, then test them, fetch health 10% included.
func TestRunTestWithPool_PoolEmpty_URLReachable(t *testing.T) {
	store := NewFakeStore(t)
	checker := &FakeHealthChecker{}
	writer := &FakePoolWriter{}

	// Mock pool loader and writer for single-airport upsert
	mockPoolOps := &MockPoolOperations{
		ExistingPool: []*subscription.Node{}, // Empty pool
	}

	orch := NewOrchestratorWithPoolOps(store, checker, writer, mockPoolOps)

	// No pool nodes initially
	run := &TestRun{
		ID:        1,
		AirportID: 1,
		CreatedAt: time.Now(),
		Status:    StatusDiagnosing,
		IsFull:    false,
	}
	store.Runs[1] = run

	// Diagnostic succeeded, fetched 3 nodes
	diagResult := &DiagnosticResult{
		HTTPStatus:    200,
		ParseFailures: 0,
		NodeCount:     3,
	}

	// Fetched nodes from diagnostic
	fetchedNodes := []*subscription.Node{
		{Name: "HK-1", Server: "1.1.1.1", Port: 443, Region: "HK", Source: "TestAirport"},
		{Name: "HK-2", Server: "1.1.1.2", Port: 443, Region: "HK", Source: "TestAirport"},
		{Name: "SG-1", Server: "2.2.2.1", Port: 443, Region: "SG", Source: "TestAirport"},
	}

	updatedRun, err := orch.RunTest(context.Background(), run, "TestAirport", fetchedNodes, diagResult, nil)
	if err != nil {
		t.Fatalf("RunTest failed: %v", err)
	}

	if updatedRun.Status != StatusCompleted {
		t.Errorf("status got %s, want %s", updatedRun.Status, StatusCompleted)
	}

	// Should have upserted nodes (check mock was called)
	if !mockPoolOps.UpsertCalled {
		t.Error("expected pool upsert to be called for empty pool + URL reachable")
	}

	// Should test all 3 nodes (no sampling needed for small pool)
	if len(writer.Updates) != 3 {
		t.Errorf("writeback count got %d, want 3", len(writer.Updates))
	}

	// Score should include fetch health
	if updatedRun.OverallScore == nil {
		t.Fatal("OverallScore is nil")
	}
	if *updatedRun.OverallScore < 80 {
		t.Errorf("overall score got %.2f, should be high with all components", *updatedRun.OverallScore)
	}
}

// TestRunTestWithPool_PoolEmpty_URLUnreachable tests Branch B: pool empty, URL unreachable.
// Expected: run fails with clear error message.
func TestRunTestWithPool_PoolEmpty_URLUnreachable(t *testing.T) {
	store := NewFakeStore(t)
	checker := &FakeHealthChecker{}
	writer := &FakePoolWriter{}

	mockPoolOps := &MockPoolOperations{
		ExistingPool: []*subscription.Node{}, // Empty pool
	}

	orch := NewOrchestratorWithPoolOps(store, checker, writer, mockPoolOps)

	run := &TestRun{
		ID:        1,
		AirportID: 1,
		CreatedAt: time.Now(),
		Status:    StatusDiagnosing,
		IsFull:    false,
	}
	store.Runs[1] = run

	// Diagnostic failed
	diagResult := &DiagnosticResult{
		HTTPStatus:    404,
		ParseFailures: 0,
		NodeCount:     0,
	}

	updatedRun, err := orch.RunTest(context.Background(), run, "TestAirport", []*subscription.Node{}, diagResult, nil)
	if err != nil {
		t.Fatalf("RunTest failed: %v", err)
	}

	// Run should fail
	if updatedRun.Status != StatusFailed {
		t.Errorf("status got %s, want %s", updatedRun.Status, StatusFailed)
	}

	// Error message should be clear
	if updatedRun.ErrorMessage == "" {
		t.Error("expected non-empty error message")
	}
	if !contains(updatedRun.ErrorMessage, "unreachable") && !contains(updatedRun.ErrorMessage, "no pool nodes") {
		t.Errorf("error message should mention URL unreachable and no pool nodes, got: %s", updatedRun.ErrorMessage)
	}

	// Should not attempt upsert or health checks
	if mockPoolOps.UpsertCalled {
		t.Error("should not call upsert when both diagnostic and pool are empty")
	}
	if len(writer.Updates) != 0 {
		t.Error("should not write back any health checks")
	}
}

// MockPoolOperations mocks pool loading and single-airport upsert
type MockPoolOperations struct {
	ExistingPool  []*subscription.Node
	UpsertCalled  bool
	UpsertedNodes []*subscription.Node
}

func (m *MockPoolOperations) LoadPoolBySource(source string) ([]*subscription.Node, error) {
	var result []*subscription.Node
	for _, n := range m.ExistingPool {
		if n.Source == source {
			result = append(result, n)
		}
	}
	return result, nil
}

func (m *MockPoolOperations) UpsertAirportNodes(_ context.Context, airportName string, nodes []*subscription.Node) error {
	m.UpsertCalled = true
	m.UpsertedNodes = nodes
	return nil
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) &&
		(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
			indexOf(s, substr) >= 0))
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
