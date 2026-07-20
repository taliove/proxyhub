package server

import (
	"context"
	"testing"
	"time"

	"github.com/taliove/proxyhub/internal/detection"
	"github.com/taliove/proxyhub/internal/subscription"
)

// TestDetectionService_TriggerDetection_JobsBased verifies trigger starts a job.
func TestDetectionService_TriggerDetection_JobsBased(t *testing.T) {
	nodes := []*subscription.Node{
		{Name: "test", Type: "vmess", Server: "example.com", Port: 443, UUID: "00000000-0000-0000-0000-000000000000"},
	}
	targets := []detection.Target{{Name: "connectivity", URL: "http://example.com"}}

	batchMgr := detection.NewBatchDetectionManager(
		nil, // No store for in-memory only test
		func() []*subscription.Node { return nodes },
		func() ([]detection.Target, error) { return targets, nil },
		func(context.Context, *subscription.Node, []detection.Target) []detection.Result {
			return []detection.Result{{Available: true}}
		},
		func(*subscription.Node, []detection.Result) {},
	)

	svc := NewDetectionServiceJobs(batchMgr, nil, nil, nil,
		func() []*subscription.Node { return nodes },
		func() ([]detection.Target, error) { return targets, nil },
	)

	// First trigger should succeed
	err := svc.TriggerDetection(context.Background(), DetectionScope{Type: "all"})
	if err != nil {
		t.Fatalf("TriggerDetection() = %v, want nil", err)
	}

	// Second trigger should fail (already running)
	err = svc.TriggerDetection(context.Background(), DetectionScope{Type: "all"})
	if err == nil {
		t.Error("TriggerDetection() while running = nil, want error")
	}
}

// TestDetectionService_CancelDetection_JobsBased verifies cancel stops the job.
func TestDetectionService_CancelDetection_JobsBased(t *testing.T) {
	nodes := []*subscription.Node{
		{Name: "test", Type: "vmess", Server: "example.com", Port: 443, UUID: "00000000-0000-0000-0000-000000000000"},
	}

	blockCh := make(chan struct{})
	detectNode := func(ctx context.Context, node *subscription.Node, targets []detection.Target) []detection.Result {
		<-blockCh // Block until test releases
		return []detection.Result{{Available: true}}
	}

	batchMgr := detection.NewBatchDetectionManager(
		nil, // No store
		func() []*subscription.Node { return nodes },
		func() ([]detection.Target, error) { return []detection.Target{{Name: "test"}}, nil },
		detectNode,
		func(*subscription.Node, []detection.Result) {},
	)

	svc := NewDetectionServiceJobs(batchMgr, nil, nil, nil,
		func() []*subscription.Node { return nodes },
		func() ([]detection.Target, error) { return []detection.Target{{Name: "test"}}, nil },
	)

	// Start detection
	if err := svc.TriggerDetection(context.Background(), DetectionScope{Type: "all"}); err != nil {
		t.Fatalf("TriggerDetection() = %v, want nil", err)
	}

	// Give it time to start
	time.Sleep(50 * time.Millisecond)

	// Cancel should succeed
	err := svc.CancelDetection()
	if err != nil {
		t.Errorf("CancelDetection() = %v, want nil", err)
	}

	// Cancel again should fail (no running job)
	err = svc.CancelDetection()
	if err == nil {
		t.Error("CancelDetection() when not running = nil, want error")
	}

	close(blockCh) // Release the blocked detection
}

// TestDetectionService_GetStatus_JobsBased verifies status reporting.
func TestDetectionService_GetStatus_JobsBased(t *testing.T) {
	nodes := []*subscription.Node{
		{Name: "node1", Type: "vmess", Server: "example.com", Port: 443, UUID: "00000000-0000-0000-0000-000000000000"},
		{Name: "node2", Type: "vmess", Server: "example.com", Port: 443, UUID: "00000000-0000-0000-0000-000000000001"},
	}

	var processedCount int
	doneCh := make(chan struct{})
	detectNode := func(ctx context.Context, node *subscription.Node, targets []detection.Target) []detection.Result {
		processedCount++
		if processedCount == 2 {
			close(doneCh)
		}
		return []detection.Result{{Available: true}}
	}

	batchMgr := detection.NewBatchDetectionManager(
		nil, // No store
		func() []*subscription.Node { return nodes },
		func() ([]detection.Target, error) { return []detection.Target{{Name: "test"}}, nil },
		detectNode,
		func(*subscription.Node, []detection.Result) {},
	)

	svc := NewDetectionServiceJobs(batchMgr, nil, nil, nil,
		func() []*subscription.Node { return nodes },
		func() ([]detection.Target, error) { return []detection.Target{{Name: "test"}}, nil },
	)

	// Before trigger: not running
	status := svc.GetStatus()
	if status.Running {
		t.Error("GetStatus() before trigger: Running = true, want false")
	}

	// Start detection
	if err := svc.TriggerDetection(context.Background(), DetectionScope{Type: "all"}); err != nil {
		t.Fatalf("TriggerDetection() = %v, want nil", err)
	}

	// During detection: running
	time.Sleep(50 * time.Millisecond)
	status = svc.GetStatus()
	if !status.Running {
		t.Error("GetStatus() during detection: Running = false, want true")
	}
	if status.TotalNodes != 2 {
		t.Errorf("GetStatus() TotalNodes = %d, want 2", status.TotalNodes)
	}

	// Wait for completion
	select {
	case <-doneCh:
	case <-time.After(2 * time.Second):
		t.Fatal("detection did not complete in time")
	}

	// After completion: not running
	time.Sleep(100 * time.Millisecond)
	status = svc.GetStatus()
	if status.Running {
		t.Error("GetStatus() after completion: Running = true, want false")
	}
}

// TestDetectionService_ScopeSelection verifies node scope filtering.
func TestDetectionService_ScopeSelection(t *testing.T) {
	nodes := []*subscription.Node{
		{Name: "node1", Type: "vmess", Server: "example.com", Port: 443, UUID: "00000000-0000-0000-0000-000000000000"},
		{Name: "node2", Type: "vmess", Server: "example.com", Port: 443, UUID: "00000000-0000-0000-0000-000000000001"},
		{Name: "node3", Type: "vmess", Server: "example.com", Port: 443, UUID: "00000000-0000-0000-0000-000000000002"},
	}

	var detectedNodes []*subscription.Node
	detectNode := func(ctx context.Context, node *subscription.Node, targets []detection.Target) []detection.Result {
		detectedNodes = append(detectedNodes, node)
		return []detection.Result{{Available: true}}
	}

	batchMgr := detection.NewBatchDetectionManager(
		nil, // No store
		func() []*subscription.Node { return nodes },
		func() ([]detection.Target, error) { return []detection.Target{{Name: "test"}}, nil },
		detectNode,
		func(*subscription.Node, []detection.Result) {},
	)

	svc := NewDetectionServiceJobs(batchMgr, nil, nil, nil,
		func() []*subscription.Node { return nodes },
		func() ([]detection.Target, error) { return []detection.Target{{Name: "test"}}, nil },
	)

	// Test selected scope
	detectedNodes = nil
	scope := DetectionScope{
		Type:     "selected",
		NodeKeys: []string{nodes[0].NodeKey(), nodes[2].NodeKey()},
	}

	if err := svc.TriggerDetection(context.Background(), scope); err != nil {
		t.Fatalf("TriggerDetection(selected) = %v, want nil", err)
	}

	time.Sleep(200 * time.Millisecond)

	if len(detectedNodes) != 2 {
		t.Errorf("detected %d nodes with selected scope, want 2", len(detectedNodes))
	}
}
