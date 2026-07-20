package detection

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/taliove/proxyhub/internal/subscription"
)

// TestBatchDetectionKind_Name verifies kind name is stable.
func TestBatchDetectionKind_Name(t *testing.T) {
	k := &batchDetectionKind{}
	if got := k.Name(); got != "batch_detection" {
		t.Errorf("Name() = %q, want %q", got, "batch_detection")
	}
}

// TestBatchDetectionKind_Resumable verifies batch detection is resumable.
func TestBatchDetectionKind_Resumable(t *testing.T) {
	k := &batchDetectionKind{}
	if !k.Resumable() {
		t.Error("Resumable() = false, want true")
	}
}

// TestBatchDetectionKind_Run_EmptyNodes verifies graceful handling of empty node list.
func TestBatchDetectionKind_Run_EmptyNodes(t *testing.T) {
	k := &batchDetectionKind{
		getNodes:   func() []*subscription.Node { return nil },
		getTargets: func() ([]Target, error) { return []Target{{Name: "test"}}, nil },
		detectNode: func(context.Context, *subscription.Node, []Target) []Result { return nil },
		saveRetag:  func(*subscription.Node, []Result) {},
	}

	params := batchDetectionParams{}
	paramsJSON, _ := json.Marshal(params)

	var events []json.RawMessage
	emit := func(data json.RawMessage) { events = append(events, data) }
	progress := func(string) {}

	err := k.Run(context.Background(), paramsJSON, "", emit, progress)
	if err != nil {
		t.Errorf("Run() with empty nodes = %v, want nil", err)
	}

	// Should emit start and done events
	if len(events) < 2 {
		t.Fatalf("emitted %d events, want at least 2", len(events))
	}
}

// TestBatchDetectionKind_Run_ProgressTracking verifies cursor progression.
func TestBatchDetectionKind_Run_ProgressTracking(t *testing.T) {
	nodes := []*subscription.Node{
		{Name: "node1", Type: "vmess", Server: "example.com", Port: 443, UUID: "00000000-0000-0000-0000-000000000000"},
		{Name: "node2", Type: "vmess", Server: "example.com", Port: 443, UUID: "00000000-0000-0000-0000-000000000001"},
	}

	targets := []Target{{Name: "connectivity", URL: "http://example.com"}}

	var detectedNodes []*subscription.Node
	detectNode := func(ctx context.Context, node *subscription.Node, tgts []Target) []Result {
		detectedNodes = append(detectedNodes, node)
		return []Result{{NodeKey: node.NodeKey(), TargetName: "connectivity", Available: true}}
	}

	var savedNodes []*subscription.Node
	saveRetag := func(node *subscription.Node, results []Result) {
		savedNodes = append(savedNodes, node)
	}

	var cursors []string
	progress := func(cursor string) { cursors = append(cursors, cursor) }

	k := &batchDetectionKind{
		getNodes:   func() []*subscription.Node { return nodes },
		getTargets: func() ([]Target, error) { return targets, nil },
		detectNode: detectNode,
		saveRetag:  saveRetag,
	}

	params := batchDetectionParams{}
	paramsJSON, _ := json.Marshal(params)

	err := k.Run(context.Background(), paramsJSON, "", nil, progress)
	if err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}

	if len(detectedNodes) != 2 {
		t.Errorf("detected %d nodes, want 2", len(detectedNodes))
	}

	if len(savedNodes) != 2 {
		t.Errorf("saved %d nodes, want 2", len(savedNodes))
	}

	if len(cursors) != 2 {
		t.Errorf("progress called %d times, want 2", len(cursors))
	}

	// Verify cursors are sequential
	if len(cursors) >= 2 {
		if cursors[0] != "1" || cursors[1] != "2" {
			t.Errorf("cursors = %v, want [1 2]", cursors)
		}
	}
}

// TestBatchDetectionKind_Run_Resume verifies resuming from cursor.
func TestBatchDetectionKind_Run_Resume(t *testing.T) {
	nodes := []*subscription.Node{
		{Name: "node1", Type: "vmess", Server: "example.com", Port: 443, UUID: "00000000-0000-0000-0000-000000000000"},
		{Name: "node2", Type: "vmess", Server: "example.com", Port: 443, UUID: "00000000-0000-0000-0000-000000000001"},
		{Name: "node3", Type: "vmess", Server: "example.com", Port: 443, UUID: "00000000-0000-0000-0000-000000000002"},
	}

	var detectedNodes []*subscription.Node
	detectNode := func(ctx context.Context, node *subscription.Node, tgts []Target) []Result {
		detectedNodes = append(detectedNodes, node)
		return []Result{{NodeKey: node.NodeKey(), Available: true}}
	}

	k := &batchDetectionKind{
		getNodes:   func() []*subscription.Node { return nodes },
		getTargets: func() ([]Target, error) { return []Target{{Name: "test"}}, nil },
		detectNode: detectNode,
		saveRetag:  func(*subscription.Node, []Result) {},
	}

	params := batchDetectionParams{}
	paramsJSON, _ := json.Marshal(params)

	// Resume from cursor "1" (already processed 1 node)
	err := k.Run(context.Background(), paramsJSON, "1", nil, func(string) {})
	if err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}

	// Should only detect nodes 2 and 3
	if len(detectedNodes) != 2 {
		t.Errorf("detected %d nodes after resume, want 2", len(detectedNodes))
	}

	if len(detectedNodes) > 0 && detectedNodes[0].Name != "node2" {
		t.Errorf("first detected node = %q, want %q", detectedNodes[0].Name, "node2")
	}
}

// TestBatchDetectionKind_Run_Cancellation verifies context cancellation stops detection.
func TestBatchDetectionKind_Run_Cancellation(t *testing.T) {
	nodes := []*subscription.Node{
		{Name: "node1", Type: "vmess", Server: "example.com", Port: 443, UUID: "00000000-0000-0000-0000-000000000000"},
		{Name: "node2", Type: "vmess", Server: "example.com", Port: 443, UUID: "00000000-0000-0000-0000-000000000001"},
		{Name: "node3", Type: "vmess", Server: "example.com", Port: 443, UUID: "00000000-0000-0000-0000-000000000002"},
	}

	ctx, cancel := context.WithCancel(context.Background())

	var detectedCount int
	detectNode := func(ctx context.Context, node *subscription.Node, tgts []Target) []Result {
		detectedCount++
		if detectedCount == 2 {
			cancel() // Cancel after 2nd node
		}
		return []Result{{NodeKey: node.NodeKey(), Available: true}}
	}

	k := &batchDetectionKind{
		getNodes:   func() []*subscription.Node { return nodes },
		getTargets: func() ([]Target, error) { return []Target{{Name: "test"}}, nil },
		detectNode: detectNode,
		saveRetag:  func(*subscription.Node, []Result) {},
	}

	params := batchDetectionParams{}
	paramsJSON, _ := json.Marshal(params)

	err := k.Run(ctx, paramsJSON, "", nil, func(string) {})

	// Should return context.Canceled
	if err != context.Canceled {
		t.Errorf("Run() after cancel = %v, want %v", err, context.Canceled)
	}

	// Should have processed fewer than all nodes
	if detectedCount >= len(nodes) {
		t.Errorf("detected %d nodes after cancel, want < %d", detectedCount, len(nodes))
	}
}

// TestBatchDetectionKind_EventFormat verifies event JSON format.
func TestBatchDetectionKind_EventFormat(t *testing.T) {
	nodes := []*subscription.Node{
		{Name: "test-node", Type: "vmess", Server: "example.com", Port: 443, UUID: "00000000-0000-0000-0000-000000000000"},
	}

	k := &batchDetectionKind{
		getNodes:   func() []*subscription.Node { return nodes },
		getTargets: func() ([]Target, error) { return []Target{{Name: "connectivity"}}, nil },
		detectNode: func(ctx context.Context, node *subscription.Node, tgts []Target) []Result {
			return []Result{{NodeKey: node.NodeKey(), Available: true, Latency: 100}}
		},
		saveRetag: func(*subscription.Node, []Result) {},
	}

	params := batchDetectionParams{}
	paramsJSON, _ := json.Marshal(params)

	var events []json.RawMessage
	emit := func(data json.RawMessage) { events = append(events, data) }

	_ = k.Run(context.Background(), paramsJSON, "", emit, func(string) {})

	// Should emit: start, node_done, done
	if len(events) != 3 {
		t.Fatalf("emitted %d events, want 3", len(events))
	}

	// Verify start event
	var startEv batchDetectionEvent
	if err := json.Unmarshal(events[0], &startEv); err != nil {
		t.Fatalf("unmarshal start event: %v", err)
	}
	if startEv.Phase != "start" {
		t.Errorf("start event phase = %q, want %q", startEv.Phase, "start")
	}
	if startEv.Total != 1 {
		t.Errorf("start event total = %d, want 1", startEv.Total)
	}

	// Verify node_done event
	var nodeDoneEv batchDetectionEvent
	if err := json.Unmarshal(events[1], &nodeDoneEv); err != nil {
		t.Fatalf("unmarshal node_done event: %v", err)
	}
	if nodeDoneEv.Phase != "node_done" {
		t.Errorf("node_done event phase = %q, want %q", nodeDoneEv.Phase, "node_done")
	}
	if nodeDoneEv.Completed != 1 {
		t.Errorf("node_done event completed = %d, want 1", nodeDoneEv.Completed)
	}

	// Verify done event
	var doneEv batchDetectionEvent
	if err := json.Unmarshal(events[2], &doneEv); err != nil {
		t.Fatalf("unmarshal done event: %v", err)
	}
	if doneEv.Phase != "done" {
		t.Errorf("done event phase = %q, want %q", doneEv.Phase, "done")
	}
}

// TestBatchDetectionKind_CancelEvent verifies cancel event format.
func TestBatchDetectionKind_CancelEvent(t *testing.T) {
	k := &batchDetectionKind{}
	data, ok := k.CancelEvent()
	if !ok {
		t.Fatal("CancelEvent() ok = false, want true")
	}

	var ev batchDetectionEvent
	if err := json.Unmarshal(data, &ev); err != nil {
		t.Fatalf("unmarshal cancel event: %v", err)
	}

	if ev.Phase != "cancelled" {
		t.Errorf("cancel event phase = %q, want %q", ev.Phase, "cancelled")
	}
}
