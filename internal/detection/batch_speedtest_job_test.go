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

// speedtestNode fixture:example.com + 全零 UUID,绝不含真实凭证。
func speedtestNode(name string, port int) *subscription.Node {
	return &subscription.Node{
		Name:   name,
		Type:   "vmess",
		Server: "example.com",
		Port:   port,
		UUID:   "00000000-0000-0000-0000-000000000000",
		Source: "airport",
	}
}

// TestBatchSpeedtestKind_Name verifies the kind name matches registration.
func TestBatchSpeedtestKind_Name(t *testing.T) {
	k := &batchSpeedtestKind{}
	if got := k.Name(); got != "batch_speedtest" {
		t.Errorf("Name() = %q, want %q", got, "batch_speedtest")
	}
}

// TestBatchSpeedtestKind_Resumable verifies batch speedtest is resumable.
func TestBatchSpeedtestKind_Resumable(t *testing.T) {
	k := &batchSpeedtestKind{}
	if !k.Resumable() {
		t.Error("Resumable() = false, want true")
	}
}

// TestBatchSpeedtestKind_Run_EmptyNodeList runs batch speedtest with empty node list.
func TestBatchSpeedtestKind_Run_EmptyNodeList(t *testing.T) {
	var emitted []json.RawMessage
	k := &batchSpeedtestKind{
		run: func(ctx context.Context, node *subscription.Node) TestResult {
			t.Fatal("run should not be called with empty node list")
			return TestResult{}
		},
		onComplete: func(userID int64, node *subscription.Node, result TestResult) {
			t.Fatal("onComplete should not be called with empty node list")
		},
	}

	params, _ := json.Marshal(batchSpeedtestParams{NodeKeys: []string{}})
	err := k.Run(context.Background(), params, "", func(data json.RawMessage) {
		emitted = append(emitted, data)
	}, func(cursor string) {})

	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if len(emitted) == 0 {
		t.Error("expected at least one event, got none")
	}
}

// TestBatchSpeedtestKind_Run_SingleNode runs batch speedtest on a single node:
// runner called with the live node, onComplete receives node + result,
// node_done event carries the TestResult.
func TestBatchSpeedtestKind_Run_SingleNode(t *testing.T) {
	node := speedtestNode("test-node", 443)

	var runCalled, completeCalled bool
	var doneResult *TestResult

	k := &batchSpeedtestKind{
		run: func(ctx context.Context, n *subscription.Node) TestResult {
			runCalled = true
			if n.Name != node.Name {
				t.Errorf("run node.Name = %q, want %q", n.Name, node.Name)
			}
			return TestResult{Available: true, Mode: "bandwidth", DownMbps: 55.5}
		},
		onComplete: func(userID int64, n *subscription.Node, result TestResult) {
			completeCalled = true
			if n == nil || n.NodeKey() != node.NodeKey() {
				t.Errorf("onComplete node = %v, want %q", n, node.NodeKey())
			}
			if result.DownMbps != 55.5 {
				t.Errorf("onComplete result.DownMbps = %v, want 55.5", result.DownMbps)
			}
		},
	}
	k.nodes.Store(examNodeRef{userID: 0, nodeKey: node.NodeKey()}, node)

	params, _ := json.Marshal(batchSpeedtestParams{NodeKeys: []string{node.NodeKey()}})
	var phases []string
	err := k.Run(context.Background(), params, "", func(data json.RawMessage) {
		var ev BatchSpeedtestEvent
		if json.Unmarshal(data, &ev) == nil {
			phases = append(phases, ev.Phase)
			if ev.Phase == "node_done" {
				doneResult = ev.Result
			}
		}
	}, func(cursor string) {})

	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if !runCalled {
		t.Error("run was not called")
	}
	if !completeCalled {
		t.Error("onComplete was not called")
	}
	if doneResult == nil || doneResult.DownMbps != 55.5 {
		t.Errorf("node_done result = %+v, want DownMbps 55.5", doneResult)
	}
	// expect node_start, node_done, done
	if len(phases) < 3 {
		t.Errorf("phases = %v, want at least node_start/node_done/done", phases)
	}
}

// TestBatchSpeedtestKind_Run_FailureStillCompletes verifies a failed measurement
// (Available=false + Error) still flows to onComplete and node_done:失败也写回,
// 让节点视图能看到"测过但失败"而非停留旧值。
func TestBatchSpeedtestKind_Run_FailureStillCompletes(t *testing.T) {
	node := speedtestNode("dead-node", 443)

	var completeResult TestResult
	k := &batchSpeedtestKind{
		run: func(ctx context.Context, n *subscription.Node) TestResult {
			return TestResult{Available: false, Mode: "bandwidth", Error: "timeout: 连接超时"}
		},
		onComplete: func(userID int64, n *subscription.Node, result TestResult) {
			completeResult = result
		},
	}
	k.nodes.Store(examNodeRef{userID: 0, nodeKey: node.NodeKey()}, node)

	params, _ := json.Marshal(batchSpeedtestParams{NodeKeys: []string{node.NodeKey()}})
	err := k.Run(context.Background(), params, "", func(data json.RawMessage) {}, func(cursor string) {})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if completeResult.Available {
		t.Error("onComplete result.Available = true, want false")
	}
	if completeResult.Error == "" {
		t.Error("onComplete result.Error empty, want failure detail")
	}
}

// TestBatchSpeedtestKind_Run_MultipleNodes runs batch speedtest serially over nodes.
func TestBatchSpeedtestKind_Run_MultipleNodes(t *testing.T) {
	nodes := []*subscription.Node{
		speedtestNode("node-1", 443),
		speedtestNode("node-2", 8443),
		speedtestNode("node-3", 9443),
	}

	var mu sync.Mutex
	var runOrder []string
	var completeOrder []string

	k := &batchSpeedtestKind{
		run: func(ctx context.Context, n *subscription.Node) TestResult {
			mu.Lock()
			runOrder = append(runOrder, n.Name)
			mu.Unlock()
			time.Sleep(10 * time.Millisecond)
			return TestResult{Available: true, Mode: "bandwidth", DownMbps: 10}
		},
		onComplete: func(userID int64, n *subscription.Node, result TestResult) {
			mu.Lock()
			completeOrder = append(completeOrder, n.NodeKey())
			mu.Unlock()
		},
	}

	var nodeKeys []string
	for _, n := range nodes {
		k.nodes.Store(examNodeRef{userID: 0, nodeKey: n.NodeKey()}, n)
		nodeKeys = append(nodeKeys, n.NodeKey())
	}

	params, _ := json.Marshal(batchSpeedtestParams{NodeKeys: nodeKeys})
	err := k.Run(context.Background(), params, "", func(data json.RawMessage) {}, func(cursor string) {})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	if len(runOrder) != len(nodes) {
		t.Errorf("run called %d times, want %d", len(runOrder), len(nodes))
	}
	if len(completeOrder) != len(nodes) {
		t.Errorf("onComplete called %d times, want %d", len(completeOrder), len(nodes))
	}
	for i, expected := range nodes {
		if runOrder[i] != expected.Name {
			t.Errorf("runOrder[%d] = %q, want %q (serial order)", i, runOrder[i], expected.Name)
		}
	}
}

// TestBatchSpeedtestKind_Run_ResumesFromCursor tests cursor-based resumption.
func TestBatchSpeedtestKind_Run_ResumesFromCursor(t *testing.T) {
	nodes := []*subscription.Node{
		speedtestNode("node-1", 443),
		speedtestNode("node-2", 8443),
		speedtestNode("node-3", 9443),
		speedtestNode("node-4", 10443),
	}

	var runNodes []string
	k := &batchSpeedtestKind{
		run: func(ctx context.Context, n *subscription.Node) TestResult {
			runNodes = append(runNodes, n.Name)
			return TestResult{Available: true, Mode: "bandwidth", DownMbps: 10}
		},
		onComplete: func(userID int64, n *subscription.Node, result TestResult) {},
	}

	var nodeKeys []string
	for _, n := range nodes {
		k.nodes.Store(examNodeRef{userID: 0, nodeKey: n.NodeKey()}, n)
		nodeKeys = append(nodeKeys, n.NodeKey())
	}

	params, _ := json.Marshal(batchSpeedtestParams{NodeKeys: nodeKeys})
	err := k.Run(context.Background(), params, "3", func(data json.RawMessage) {}, func(cursor string) {})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if len(runNodes) != 1 {
		t.Fatalf("run called %d times, want 1", len(runNodes))
	}
	if runNodes[0] != "node-4" {
		t.Errorf("resumed with node %q, want %q", runNodes[0], "node-4")
	}
}

// TestBatchSpeedtestKind_Run_ProgressCallback verifies progress cursor after each node.
func TestBatchSpeedtestKind_Run_ProgressCallback(t *testing.T) {
	nodes := []*subscription.Node{
		speedtestNode("node-1", 443),
		speedtestNode("node-2", 8443),
	}

	k := &batchSpeedtestKind{
		run:        func(ctx context.Context, n *subscription.Node) TestResult { return TestResult{Mode: "bandwidth"} },
		onComplete: func(userID int64, n *subscription.Node, result TestResult) {},
	}

	var nodeKeys []string
	for _, n := range nodes {
		k.nodes.Store(examNodeRef{userID: 0, nodeKey: n.NodeKey()}, n)
		nodeKeys = append(nodeKeys, n.NodeKey())
	}

	params, _ := json.Marshal(batchSpeedtestParams{NodeKeys: nodeKeys})
	var progressCursors []string
	err := k.Run(context.Background(), params, "", func(data json.RawMessage) {}, func(cursor string) {
		progressCursors = append(progressCursors, cursor)
	})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	if len(progressCursors) != len(nodes) {
		t.Errorf("progress reported %d times, want %d", len(progressCursors), len(nodes))
	}
	for i, expected := range []string{"1", "2"} {
		if progressCursors[i] != expected {
			t.Errorf("progressCursors[%d] = %q, want %q", i, progressCursors[i], expected)
		}
	}
}

// TestBatchSpeedtestKind_Run_Cancellation tests context cancellation stops the job.
func TestBatchSpeedtestKind_Run_Cancellation(t *testing.T) {
	nodes := []*subscription.Node{
		speedtestNode("node-1", 443),
		speedtestNode("node-2", 8443),
		speedtestNode("node-3", 9443),
	}

	var runCount int
	k := &batchSpeedtestKind{
		run: func(ctx context.Context, n *subscription.Node) TestResult {
			runCount++
			time.Sleep(50 * time.Millisecond)
			return TestResult{Available: true, Mode: "bandwidth", DownMbps: 10}
		},
		onComplete: func(userID int64, n *subscription.Node, result TestResult) {},
	}

	var nodeKeys []string
	for _, n := range nodes {
		k.nodes.Store(examNodeRef{userID: 0, nodeKey: n.NodeKey()}, n)
		nodeKeys = append(nodeKeys, n.NodeKey())
	}

	params, _ := json.Marshal(batchSpeedtestParams{NodeKeys: nodeKeys})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(80 * time.Millisecond)
		cancel()
	}()

	err := k.Run(ctx, params, "", func(data json.RawMessage) {}, func(cursor string) {})
	if err != context.Canceled {
		t.Errorf("Run() error = %v, want context.Canceled", err)
	}
	if runCount >= len(nodes) {
		t.Errorf("runCount = %d, want < %d (cancellation should stop early)", runCount, len(nodes))
	}
}

// TestBatchSpeedtestKind_Run_MissingNode tests missing node emits node_error and continues.
func TestBatchSpeedtestKind_Run_MissingNode(t *testing.T) {
	k := &batchSpeedtestKind{
		run: func(ctx context.Context, n *subscription.Node) TestResult {
			t.Fatal("run should not be called for missing node")
			return TestResult{}
		},
		onComplete: func(userID int64, n *subscription.Node, result TestResult) {
			t.Fatal("onComplete should not be called for missing node")
		},
	}

	params, _ := json.Marshal(batchSpeedtestParams{NodeKeys: []string{"missing-node-key"}})
	var emittedEvents []map[string]any
	err := k.Run(context.Background(), params, "", func(data json.RawMessage) {
		var ev map[string]any
		json.Unmarshal(data, &ev)
		emittedEvents = append(emittedEvents, ev)
	}, func(cursor string) {})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

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

// TestBatchSpeedtestKind_Run_InvalidParams tests invalid JSON params return error.
func TestBatchSpeedtestKind_Run_InvalidParams(t *testing.T) {
	k := &batchSpeedtestKind{}
	err := k.Run(context.Background(), json.RawMessage(`{invalid json`), "", func(data json.RawMessage) {}, func(cursor string) {})
	if err == nil {
		t.Fatal("Run() error = nil, want error for invalid params")
	}
	if !strings.Contains(err.Error(), "unmarshal") && !strings.Contains(err.Error(), "params") {
		t.Errorf("Run() error = %v, want unmarshal/params error", err)
	}
}

// TestBatchSpeedtestKind_CancelEvent verifies cancel event generation.
func TestBatchSpeedtestKind_CancelEvent(t *testing.T) {
	k := &batchSpeedtestKind{}
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
