package detection

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/taliove/proxyhub/internal/subscription"
)

// TestBatchExamKind_Name verifies the kind name matches registration.
func TestBatchExamKind_Name(t *testing.T) {
	k := &batchExamKind{}
	if got := k.Name(); got != "batch_exam" {
		t.Errorf("Name() = %q, want %q", got, "batch_exam")
	}
}

// TestBatchExamKind_Resumable verifies batch exam is resumable.
func TestBatchExamKind_Resumable(t *testing.T) {
	k := &batchExamKind{}
	if !k.Resumable() {
		t.Error("Resumable() = false, want true")
	}
}

// TestBatchExamKind_Run_EmptyNodeList runs batch exam with empty node list.
func TestBatchExamKind_Run_EmptyNodeList(t *testing.T) {
	var emitted []json.RawMessage
	k := &batchExamKind{
		runSimplified: func(ctx context.Context, node *subscription.Node, emit func(ExamEvent)) ExamReport {
			t.Fatal("runSimplified should not be called with empty node list")
			return ExamReport{}
		},
		onComplete: func(userID int64, nodeKey string, report ExamReport) {
			t.Fatal("onComplete should not be called with empty node list")
		},
	}

	params, _ := json.Marshal(batchExamParams{NodeKeys: []string{}})
	err := k.Run(context.Background(), params, "", func(data json.RawMessage) {
		emitted = append(emitted, data)
	}, func(cursor string) {})

	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	// Should emit at least a completion event
	if len(emitted) == 0 {
		t.Error("expected at least one event, got none")
	}
}

// TestBatchExamKind_Run_SingleNode runs batch exam on a single node.
func TestBatchExamKind_Run_SingleNode(t *testing.T) {
	node := &subscription.Node{
		Name:   "test-node",
		Type:   "ss",
		Server: "example.com",
		Port:   443,
	}

	var runCalled bool
	var completeCalled bool
	var emittedEvents []string

	k := &batchExamKind{
		runSimplified: func(ctx context.Context, n *subscription.Node, emit func(ExamEvent)) ExamReport {
			runCalled = true
			if n.Name != node.Name {
				t.Errorf("runSimplified node.Name = %q, want %q", n.Name, node.Name)
			}
			emit(ExamEvent{Phase: "sample", Section: "stability"})
			return ExamReport{Stability: &StabilityMetrics{Total: 10, Succeeded: 10, Score: 100}}
		},
		onComplete: func(userID int64, nodeKey string, report ExamReport) {
			completeCalled = true
			if report.Stability == nil {
				t.Error("onComplete report.Stability = nil, want non-nil")
			}
		},
	}
	k.nodes.Store(examNodeRef{userID: 0, nodeKey: node.NodeKey()}, node)

	params, _ := json.Marshal(batchExamParams{NodeKeys: []string{node.NodeKey()}})
	err := k.Run(context.Background(), params, "", func(data json.RawMessage) {
		var ev map[string]any
		json.Unmarshal(data, &ev)
		if phase, ok := ev["phase"].(string); ok {
			emittedEvents = append(emittedEvents, phase)
		}
	}, func(cursor string) {})

	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	if !runCalled {
		t.Error("runSimplified was not called")
	}

	if !completeCalled {
		t.Error("onComplete was not called")
	}

	// Should emit at least sample and completion events
	if len(emittedEvents) < 2 {
		t.Errorf("emitted %d events, want at least 2", len(emittedEvents))
	}
}

// TestBatchExamKind_Run_MultipleNodes runs batch exam on multiple nodes serially.
func TestBatchExamKind_Run_MultipleNodes(t *testing.T) {
	nodes := []*subscription.Node{
		{Name: "node-1", Type: "ss", Server: "example.com", Port: 443},
		{Name: "node-2", Type: "ss", Server: "example.com", Port: 8443},
		{Name: "node-3", Type: "ss", Server: "example.com", Port: 9443},
	}

	var mu sync.Mutex
	var runOrder []string
	var completeOrder []string

	k := &batchExamKind{
		runSimplified: func(ctx context.Context, n *subscription.Node, emit func(ExamEvent)) ExamReport {
			mu.Lock()
			runOrder = append(runOrder, n.Name)
			mu.Unlock()
			time.Sleep(10 * time.Millisecond) // Simulate work
			return ExamReport{Stability: &StabilityMetrics{Total: 10, Succeeded: 9, Score: 95}}
		},
		onComplete: func(userID int64, nodeKey string, report ExamReport) {
			mu.Lock()
			completeOrder = append(completeOrder, nodeKey)
			mu.Unlock()
		},
	}

	var nodeKeys []string
	for _, n := range nodes {
		k.nodes.Store(examNodeRef{userID: 0, nodeKey: n.NodeKey()}, n)
		nodeKeys = append(nodeKeys, n.NodeKey())
	}

	params, _ := json.Marshal(batchExamParams{NodeKeys: nodeKeys})
	err := k.Run(context.Background(), params, "", func(data json.RawMessage) {}, func(cursor string) {})

	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	if len(runOrder) != len(nodes) {
		t.Errorf("runSimplified called %d times, want %d", len(runOrder), len(nodes))
	}

	if len(completeOrder) != len(nodes) {
		t.Errorf("onComplete called %d times, want %d", len(completeOrder), len(nodes))
	}

	// Verify serial execution order
	for i, expected := range nodes {
		if runOrder[i] != expected.Name {
			t.Errorf("runOrder[%d] = %q, want %q", i, runOrder[i], expected.Name)
		}
	}
}

// TestBatchExamKind_Run_ResumesFromCursor tests cursor-based resumption.
func TestBatchExamKind_Run_ResumesFromCursor(t *testing.T) {
	nodes := []*subscription.Node{
		{Name: "node-1", Type: "ss", Server: "example.com", Port: 443},
		{Name: "node-2", Type: "ss", Server: "example.com", Port: 8443},
		{Name: "node-3", Type: "ss", Server: "example.com", Port: 9443},
		{Name: "node-4", Type: "ss", Server: "example.com", Port: 10443},
	}

	var runNodes []string
	k := &batchExamKind{
		runSimplified: func(ctx context.Context, n *subscription.Node, emit func(ExamEvent)) ExamReport {
			runNodes = append(runNodes, n.Name)
			return ExamReport{Stability: &StabilityMetrics{Total: 10, Succeeded: 10, Score: 100}}
		},
		onComplete: func(userID int64, nodeKey string, report ExamReport) {},
	}

	var nodeKeys []string
	for _, n := range nodes {
		k.nodes.Store(examNodeRef{userID: 0, nodeKey: n.NodeKey()}, n)
		nodeKeys = append(nodeKeys, n.NodeKey())
	}

	params, _ := json.Marshal(batchExamParams{NodeKeys: nodeKeys})

	// Resume from cursor "3" (should skip first 3 nodes, run node-4)
	cursor := "3"
	err := k.Run(context.Background(), params, cursor, func(data json.RawMessage) {}, func(cursor string) {})

	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	if len(runNodes) != 1 {
		t.Fatalf("runSimplified called %d times, want 1", len(runNodes))
	}

	if runNodes[0] != "node-4" {
		t.Errorf("resumed with node %q, want %q", runNodes[0], "node-4")
	}
}

// TestBatchExamKind_Run_ProgressCallback verifies progress is reported after each node.
func TestBatchExamKind_Run_ProgressCallback(t *testing.T) {
	nodes := []*subscription.Node{
		{Name: "node-1", Type: "ss", Server: "example.com", Port: 443},
		{Name: "node-2", Type: "ss", Server: "example.com", Port: 8443},
	}

	k := &batchExamKind{
		runSimplified: func(ctx context.Context, n *subscription.Node, emit func(ExamEvent)) ExamReport {
			return ExamReport{Stability: &StabilityMetrics{Total: 10, Succeeded: 10, Score: 100}}
		},
		onComplete: func(userID int64, nodeKey string, report ExamReport) {},
	}

	var nodeKeys []string
	for _, n := range nodes {
		k.nodes.Store(examNodeRef{userID: 0, nodeKey: n.NodeKey()}, n)
		nodeKeys = append(nodeKeys, n.NodeKey())
	}

	params, _ := json.Marshal(batchExamParams{NodeKeys: nodeKeys})

	var progressCursors []string
	err := k.Run(context.Background(), params, "", func(data json.RawMessage) {}, func(cursor string) {
		progressCursors = append(progressCursors, cursor)
	})

	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	// Should report progress after each node
	if len(progressCursors) != len(nodes) {
		t.Errorf("progress reported %d times, want %d", len(progressCursors), len(nodes))
	}

	// Verify cursors are sequential: "1", "2"
	for i, expected := range []string{"1", "2"} {
		if progressCursors[i] != expected {
			t.Errorf("progressCursors[%d] = %q, want %q", i, progressCursors[i], expected)
		}
	}
}

// TestBatchExamKind_Run_Cancellation tests context cancellation stops batch exam.
func TestBatchExamKind_Run_Cancellation(t *testing.T) {
	nodes := []*subscription.Node{
		{Name: "node-1", Type: "ss", Server: "example.com", Port: 443},
		{Name: "node-2", Type: "ss", Server: "example.com", Port: 8443},
		{Name: "node-3", Type: "ss", Server: "example.com", Port: 9443},
	}

	var runCount int
	k := &batchExamKind{
		runSimplified: func(ctx context.Context, n *subscription.Node, emit func(ExamEvent)) ExamReport {
			runCount++
			time.Sleep(50 * time.Millisecond)
			return ExamReport{Stability: &StabilityMetrics{Total: 10, Succeeded: 10, Score: 100}}
		},
		onComplete: func(userID int64, nodeKey string, report ExamReport) {},
	}

	var nodeKeys []string
	for _, n := range nodes {
		k.nodes.Store(examNodeRef{userID: 0, nodeKey: n.NodeKey()}, n)
		nodeKeys = append(nodeKeys, n.NodeKey())
	}

	params, _ := json.Marshal(batchExamParams{NodeKeys: nodeKeys})

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after first node completes
	go func() {
		time.Sleep(80 * time.Millisecond)
		cancel()
	}()

	err := k.Run(ctx, params, "", func(data json.RawMessage) {}, func(cursor string) {})

	if err != context.Canceled {
		t.Errorf("Run() error = %v, want context.Canceled", err)
	}

	// Should have processed only 1 or 2 nodes before cancellation
	if runCount >= len(nodes) {
		t.Errorf("runCount = %d, want < %d (cancellation should stop early)", runCount, len(nodes))
	}
}

// TestBatchExamKind_Run_MissingNode tests handling of missing node in memory pool.
func TestBatchExamKind_Run_MissingNode(t *testing.T) {
	k := &batchExamKind{
		runSimplified: func(ctx context.Context, n *subscription.Node, emit func(ExamEvent)) ExamReport {
			t.Fatal("runSimplified should not be called for missing node")
			return ExamReport{}
		},
		onComplete: func(userID int64, nodeKey string, report ExamReport) {
			t.Fatal("onComplete should not be called for missing node")
		},
	}

	params, _ := json.Marshal(batchExamParams{NodeKeys: []string{"missing-node-key"}})

	var emittedEvents []map[string]any
	err := k.Run(context.Background(), params, "", func(data json.RawMessage) {
		var ev map[string]any
		json.Unmarshal(data, &ev)
		emittedEvents = append(emittedEvents, ev)
	}, func(cursor string) {})

	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	// Should emit error event for missing node
	found := false
	for _, ev := range emittedEvents {
		if phase, ok := ev["phase"].(string); ok && phase == "node_error" {
			if errMsg, ok := ev["error"].(string); ok && strings.Contains(errMsg, "not found") {
				found = true
				break
			}
		}
	}

	if !found {
		t.Error("expected node_error event for missing node, got none")
	}
}

// TestBatchExamKind_Run_InvalidParams tests handling of invalid JSON parameters.
func TestBatchExamKind_Run_InvalidParams(t *testing.T) {
	k := &batchExamKind{}

	err := k.Run(context.Background(), json.RawMessage(`{invalid json`), "", func(data json.RawMessage) {}, func(cursor string) {})

	if err == nil {
		t.Fatal("Run() error = nil, want error for invalid params")
	}

	if !strings.Contains(err.Error(), "unmarshal") && !strings.Contains(err.Error(), "params") {
		t.Errorf("Run() error = %v, want unmarshal/params error", err)
	}
}

// TestNormalizeBatchExamMode covers default and validation of the batch exam mode.
func TestNormalizeBatchExamMode(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"empty defaults to simplified", "", BatchExamModeSimplified, false},
		{"explicit simplified", "simplified", BatchExamModeSimplified, false},
		{"full", "full", BatchExamModeFull, false},
		{"uppercase rejected", "FULL", "", true},
		{"unknown rejected", "deep", "", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeBatchExamMode(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("normalizeBatchExamMode(%q) error = nil, want error", tc.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeBatchExamMode(%q) error = %v, want nil", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("normalizeBatchExamMode(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestNewBatchExamParams_NormalizesMode verifies params always persist an explicit mode.
func TestNewBatchExamParams_NormalizesMode(t *testing.T) {
	p, err := newBatchExamParams([]string{"k1"}, "selected", "")
	if err != nil {
		t.Fatalf("newBatchExamParams empty mode error = %v, want nil", err)
	}
	if p.Mode != BatchExamModeSimplified {
		t.Errorf("newBatchExamParams empty mode Mode = %q, want %q", p.Mode, BatchExamModeSimplified)
	}

	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	if !strings.Contains(string(raw), `"mode":"simplified"`) {
		t.Errorf("marshaled params = %s, want explicit mode field", raw)
	}

	if _, err := newBatchExamParams([]string{"k1"}, "selected", "bogus"); err == nil {
		t.Error("newBatchExamParams bogus mode error = nil, want error")
	}
}

// TestBatchExamKind_Run_ModeFull_UsesFullRunner verifies mode=full selects the full runner.
func TestBatchExamKind_Run_ModeFull_UsesFullRunner(t *testing.T) {
	node := &subscription.Node{Name: "full-node", Type: "ss", Server: "example.com", Port: 443}

	var simplifiedCalled, fullCalled bool
	var completedReport ExamReport

	k := &batchExamKind{
		runSimplified: func(ctx context.Context, n *subscription.Node, emit func(ExamEvent)) ExamReport {
			simplifiedCalled = true
			return ExamReport{Stability: &StabilityMetrics{Total: 10, Succeeded: 10, Score: 100}}
		},
		runFull: func(ctx context.Context, n *subscription.Node, emit func(ExamEvent)) ExamReport {
			fullCalled = true
			return ExamReport{
				Stability: &StabilityMetrics{Total: 10, Succeeded: 10, Score: 100},
				Unlock:    &UnlockMetrics{},
			}
		},
		onComplete: func(userID int64, nodeKey string, report ExamReport) {
			completedReport = report
		},
	}
	k.nodes.Store(examNodeRef{userID: 0, nodeKey: node.NodeKey()}, node)

	params, _ := json.Marshal(batchExamParams{NodeKeys: []string{node.NodeKey()}, Mode: BatchExamModeFull})
	err := k.Run(context.Background(), params, "", func(data json.RawMessage) {}, func(cursor string) {})

	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if simplifiedCalled {
		t.Error("runSimplified was called in full mode, want not called")
	}
	if !fullCalled {
		t.Error("runFull was not called in full mode")
	}
	if completedReport.Unlock == nil {
		t.Errorf("onComplete report.Unlock = nil, want full report with unlock section")
	}
}

// TestBatchExamKind_Run_LegacyParamsResume_KeepsSimplified verifies upgrade compatibility:
// params persisted before the mode field existed must resume with the simplified runner.
func TestBatchExamKind_Run_LegacyParamsResume_KeepsSimplified(t *testing.T) {
	nodes := []*subscription.Node{
		{Name: "node-1", Type: "ss", Server: "example.com", Port: 443},
		{Name: "node-2", Type: "ss", Server: "example.com", Port: 8443},
	}

	var simplifiedRuns, fullRuns []string
	k := &batchExamKind{
		runSimplified: func(ctx context.Context, n *subscription.Node, emit func(ExamEvent)) ExamReport {
			simplifiedRuns = append(simplifiedRuns, n.Name)
			return ExamReport{Stability: &StabilityMetrics{Total: 10, Succeeded: 10, Score: 100}}
		},
		runFull: func(ctx context.Context, n *subscription.Node, emit func(ExamEvent)) ExamReport {
			fullRuns = append(fullRuns, n.Name)
			return ExamReport{Stability: &StabilityMetrics{Total: 10, Succeeded: 10, Score: 100}}
		},
		onComplete: func(userID int64, nodeKey string, report ExamReport) {},
	}

	var nodeKeys []string
	for _, n := range nodes {
		k.nodes.Store(examNodeRef{userID: 0, nodeKey: n.NodeKey()}, n)
		nodeKeys = append(nodeKeys, n.NodeKey())
	}

	// Legacy params: marshaled before the mode field existed (no "mode" key at all).
	legacyParams, _ := json.Marshal(map[string]any{"node_keys": nodeKeys})

	// Resume from cursor "1": only node-2 remains.
	err := k.Run(context.Background(), legacyParams, "1", func(data json.RawMessage) {}, func(cursor string) {})

	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if len(fullRuns) != 0 {
		t.Errorf("runFull called for legacy params (%v), want simplified only", fullRuns)
	}
	if len(simplifiedRuns) != 1 || simplifiedRuns[0] != "node-2" {
		t.Errorf("runSimplified runs = %v, want [node-2]", simplifiedRuns)
	}
}

// TestBatchExamKind_Run_ModeUnknown_ReturnsError verifies unknown persisted mode fails fast.
func TestBatchExamKind_Run_ModeUnknown_ReturnsError(t *testing.T) {
	k := &batchExamKind{
		runSimplified: func(ctx context.Context, n *subscription.Node, emit func(ExamEvent)) ExamReport {
			t.Fatal("runSimplified should not be called for unknown mode")
			return ExamReport{}
		},
	}

	params, _ := json.Marshal(batchExamParams{NodeKeys: []string{"k"}, Mode: "bogus"})
	err := k.Run(context.Background(), params, "", func(data json.RawMessage) {}, func(cursor string) {})

	if err == nil {
		t.Fatal("Run() error = nil, want error for unknown mode")
	}
	if !strings.Contains(err.Error(), "mode") {
		t.Errorf("Run() error = %v, want mode error", err)
	}
}

// TestBatchExamKind_Run_ModeFull_MissingFullRunner_ReturnsError verifies full mode without
// a configured full runner fails fast instead of silently degrading.
func TestBatchExamKind_Run_ModeFull_MissingFullRunner_ReturnsError(t *testing.T) {
	k := &batchExamKind{
		runSimplified: func(ctx context.Context, n *subscription.Node, emit func(ExamEvent)) ExamReport {
			t.Fatal("runSimplified should not be called when full mode requested")
			return ExamReport{}
		},
	}

	params, _ := json.Marshal(batchExamParams{NodeKeys: []string{"k"}, Mode: BatchExamModeFull})
	err := k.Run(context.Background(), params, "", func(data json.RawMessage) {}, func(cursor string) {})

	if err == nil {
		t.Fatal("Run() error = nil, want error for missing full runner")
	}
}

// TestBatchExamJobManager_Start_InvalidMode verifies Start rejects unknown modes.
func TestBatchExamJobManager_Start_InvalidMode(t *testing.T) {
	mgr := NewBatchExamJobManager(
		func(ctx context.Context, n *subscription.Node, emit func(ExamEvent)) ExamReport {
			return ExamReport{}
		},
		func(ctx context.Context, n *subscription.Node, emit func(ExamEvent)) ExamReport {
			return ExamReport{}
		},
		nil,
	)

	if _, err := mgr.Start([]string{"k"}, nil, "selected", "bogus"); err == nil {
		t.Error("Start() error = nil, want error for invalid mode")
	}
}

// TestBatchExamKind_CancelEvent verifies cancel event generation.
func TestBatchExamKind_CancelEvent(t *testing.T) {
	k := &batchExamKind{}

	data, ok := k.CancelEvent()
	if !ok {
		t.Fatal("CancelEvent() ok = false, want true")
	}

	var ev map[string]any
	if err := json.Unmarshal(data, &ev); err != nil {
		t.Fatalf("CancelEvent() returned invalid JSON: %v", err)
	}

	if phase, ok := ev["phase"].(string); !ok || phase != "cancelled" {
		t.Errorf("CancelEvent() phase = %q, want %q", phase, "cancelled")
	}
}
