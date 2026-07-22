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

// batchStabilityFixture 构造节点 fixture(example.com,无真实凭证)。
func batchStabilityNode(name, server string, port int) *subscription.Node {
	return &subscription.Node{Name: name, Type: "ss", Server: server, Port: port}
}

// stabilityCheckReport 构造带来源标记的出网+稳定性报告(模拟 runner 产出)。
func stabilityCheckReport(score int) ExamReport {
	return ExamReport{
		Source:    ExamSourceStabilityCheck,
		Stability: &StabilityMetrics{Total: 10, Succeeded: 10, Score: score},
		Egress:    &EgressMetrics{IPv4: &EgressIPv4{IP: "203.0.113.7", CountryCode: "US"}},
	}
}

func TestBatchStabilityKind_Name(t *testing.T) {
	k := &batchStabilityKind{}
	if got := k.Name(); got != "batch_stability" {
		t.Errorf("Name() = %q, want %q", got, "batch_stability")
	}
}

func TestBatchStabilityKind_Resumable(t *testing.T) {
	k := &batchStabilityKind{}
	if !k.Resumable() {
		t.Error("Resumable() = false, want true")
	}
}

func TestBatchStabilityKind_Run_EmptyNodeList(t *testing.T) {
	var emitted []json.RawMessage
	k := &batchStabilityKind{
		runCheck: func(ctx context.Context, node *subscription.Node, emit func(ExamEvent)) ExamReport {
			t.Fatal("runCheck should not be called with empty node list")
			return ExamReport{}
		},
		onComplete: func(nodeKey string, report ExamReport) {
			t.Fatal("onComplete should not be called with empty node list")
		},
	}

	params, _ := json.Marshal(batchStabilityParams{NodeKeys: []string{}})
	err := k.Run(context.Background(), params, "", func(data json.RawMessage) {
		emitted = append(emitted, data)
	}, func(cursor string) {})

	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if len(emitted) == 0 {
		t.Error("expected at least one event (done), got none")
	}
}

func TestBatchStabilityKind_Run_SingleNode(t *testing.T) {
	node := batchStabilityNode("test-node", "example.com", 443)

	var runCalled, completeCalled bool
	var emittedEvents []string

	k := &batchStabilityKind{
		runCheck: func(ctx context.Context, n *subscription.Node, emit func(ExamEvent)) ExamReport {
			runCalled = true
			if n.Name != node.Name {
				t.Errorf("runCheck node.Name = %q, want %q", n.Name, node.Name)
			}
			return stabilityCheckReport(96)
		},
		onComplete: func(nodeKey string, report ExamReport) {
			completeCalled = true
			if report.Stability == nil {
				t.Error("onComplete report.Stability = nil, want non-nil")
			}
			// 来源标记必须随报告传到收口回调(落库可区分完整体检)。
			if report.Source != ExamSourceStabilityCheck {
				t.Errorf("onComplete report.Source = %q, want %q", report.Source, ExamSourceStabilityCheck)
			}
		},
	}
	k.nodes.Store(node.NodeKey(), node)

	params, _ := json.Marshal(batchStabilityParams{NodeKeys: []string{node.NodeKey()}})
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
		t.Error("runCheck was not called")
	}
	if !completeCalled {
		t.Error("onComplete was not called")
	}
	// node_start + node_done + done
	if len(emittedEvents) < 3 {
		t.Errorf("emitted %d events, want at least 3 (node_start/node_done/done)", len(emittedEvents))
	}
}

func TestBatchStabilityKind_Run_MultipleNodes(t *testing.T) {
	nodes := []*subscription.Node{
		batchStabilityNode("node-1", "a.example.com", 443),
		batchStabilityNode("node-2", "b.example.com", 8443),
		batchStabilityNode("node-3", "c.example.com", 1080),
	}

	var mu sync.Mutex
	var runOrder []string

	k := &batchStabilityKind{
		runCheck: func(ctx context.Context, n *subscription.Node, emit func(ExamEvent)) ExamReport {
			mu.Lock()
			runOrder = append(runOrder, n.Name)
			mu.Unlock()
			return stabilityCheckReport(80)
		},
		onComplete: func(nodeKey string, report ExamReport) {},
	}
	for _, n := range nodes {
		k.nodes.Store(n.NodeKey(), n)
	}

	keys := make([]string, len(nodes))
	for i, n := range nodes {
		keys[i] = n.NodeKey()
	}
	params, _ := json.Marshal(batchStabilityParams{NodeKeys: keys})
	err := k.Run(context.Background(), params, "", func(json.RawMessage) {}, func(string) {})

	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if len(runOrder) != len(nodes) {
		t.Fatalf("runCheck called %d times, want %d", len(runOrder), len(nodes))
	}
	for i, name := range runOrder {
		if name != nodes[i].Name {
			t.Errorf("runOrder[%d] = %q, want %q (serial order)", i, name, nodes[i].Name)
		}
	}
}

func TestBatchStabilityKind_Run_ResumesFromCursor(t *testing.T) {
	nodes := []*subscription.Node{
		batchStabilityNode("node-1", "a.example.com", 443),
		batchStabilityNode("node-2", "b.example.com", 8443),
	}

	var runNodes []string
	k := &batchStabilityKind{
		runCheck: func(ctx context.Context, n *subscription.Node, emit func(ExamEvent)) ExamReport {
			runNodes = append(runNodes, n.Name)
			return stabilityCheckReport(80)
		},
		onComplete: func(nodeKey string, report ExamReport) {},
	}
	for _, n := range nodes {
		k.nodes.Store(n.NodeKey(), n)
	}

	keys := []string{nodes[0].NodeKey(), nodes[1].NodeKey()}
	params, _ := json.Marshal(batchStabilityParams{NodeKeys: keys})
	// 游标 "1" = 已完成 1 个节点,从第二个续跑。
	err := k.Run(context.Background(), params, "1", func(json.RawMessage) {}, func(string) {})

	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if len(runNodes) != 1 {
		t.Fatalf("runCheck called %d times, want 1 (resume skips completed)", len(runNodes))
	}
	if runNodes[0] != "node-2" {
		t.Errorf("resumed node = %q, want node-2", runNodes[0])
	}
}

func TestBatchStabilityKind_Run_ProgressCallback(t *testing.T) {
	nodes := []*subscription.Node{
		batchStabilityNode("node-1", "a.example.com", 443),
		batchStabilityNode("node-2", "b.example.com", 8443),
	}

	var progressUpdates []string
	k := &batchStabilityKind{
		runCheck: func(ctx context.Context, n *subscription.Node, emit func(ExamEvent)) ExamReport {
			return stabilityCheckReport(80)
		},
		onComplete: func(nodeKey string, report ExamReport) {},
	}
	for _, n := range nodes {
		k.nodes.Store(n.NodeKey(), n)
	}

	keys := []string{nodes[0].NodeKey(), nodes[1].NodeKey()}
	params, _ := json.Marshal(batchStabilityParams{NodeKeys: keys})
	err := k.Run(context.Background(), params, "", func(json.RawMessage) {}, func(cursor string) {
		progressUpdates = append(progressUpdates, cursor)
	})

	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if len(progressUpdates) != 2 {
		t.Fatalf("progress called %d times, want 2", len(progressUpdates))
	}
	if progressUpdates[0] != "1" || progressUpdates[1] != "2" {
		t.Errorf("progress updates = %v, want [1 2] (cursor = completed count)", progressUpdates)
	}
}

func TestBatchStabilityKind_Run_Cancellation(t *testing.T) {
	node := batchStabilityNode("node-1", "example.com", 443)

	ctx, cancel := context.WithCancel(context.Background())
	k := &batchStabilityKind{
		runCheck: func(ctx context.Context, n *subscription.Node, emit func(ExamEvent)) ExamReport {
			cancel() // 第一个节点跑完后取消
			return stabilityCheckReport(80)
		},
		onComplete: func(nodeKey string, report ExamReport) {},
	}
	k.nodes.Store(node.NodeKey(), node)

	params, _ := json.Marshal(batchStabilityParams{NodeKeys: []string{node.NodeKey(), "other-key"}})
	err := k.Run(ctx, params, "", func(json.RawMessage) {}, func(string) {})

	if err == nil {
		t.Error("Run() error = nil, want context.Canceled")
	}
}

func TestBatchStabilityKind_Run_MissingNode(t *testing.T) {
	var errorEvents []string
	k := &batchStabilityKind{
		runCheck: func(ctx context.Context, n *subscription.Node, emit func(ExamEvent)) ExamReport {
			t.Fatal("runCheck should not be called for missing node")
			return ExamReport{}
		},
		onComplete: func(nodeKey string, report ExamReport) {
			t.Fatal("onComplete should not be called for missing node")
		},
	}

	params, _ := json.Marshal(batchStabilityParams{NodeKeys: []string{"missing-key"}})
	err := k.Run(context.Background(), params, "", func(data json.RawMessage) {
		var ev map[string]any
		json.Unmarshal(data, &ev)
		if phase, _ := ev["phase"].(string); phase == "node_error" {
			errorEvents = append(errorEvents, ev["error"].(string))
		}
	}, func(string) {})

	if err != nil {
		t.Fatalf("Run() error = %v, want nil (missing node is per-node error, not job error)", err)
	}
	if len(errorEvents) != 1 {
		t.Fatalf("node_error events = %d, want 1", len(errorEvents))
	}
	if !strings.Contains(errorEvents[0], "not found") {
		t.Errorf("node_error = %q, want mention of node not found", errorEvents[0])
	}
}

func TestBatchStabilityKind_Run_InvalidParams(t *testing.T) {
	k := &batchStabilityKind{}
	err := k.Run(context.Background(), json.RawMessage(`{invalid`), "", func(json.RawMessage) {}, func(string) {})
	if err == nil {
		t.Error("Run() error = nil, want unmarshal error")
	}
}

func TestBatchStabilityKind_CancelEvent(t *testing.T) {
	k := &batchStabilityKind{}
	data, ok := k.CancelEvent()
	if !ok {
		t.Fatal("CancelEvent() ok = false, want true")
	}
	var ev map[string]any
	if err := json.Unmarshal(data, &ev); err != nil {
		t.Fatalf("CancelEvent() data unmarshal: %v", err)
	}
	if ev["phase"] != "cancelled" {
		t.Errorf("CancelEvent() phase = %v, want cancelled", ev["phase"])
	}
}

// 管理器层:启动固定 key(全局单例)、参数落 jobs 表、取消语义。
func TestBatchStabilityJobManager_StartCancel(t *testing.T) {
	node := batchStabilityNode("test-node", "example.com", 443)

	mgr := NewBatchStabilityJobManager(
		func(ctx context.Context, n *subscription.Node, emit func(ExamEvent)) ExamReport {
			// 阻塞直到任务 ctx 取消,模拟长任务。
			<-ctx.Done()
			return ExamReport{}
		},
		func(nodeKey string, report ExamReport) {},
	)

	key, err := mgr.Start([]string{node.NodeKey()}, []*subscription.Node{node}, "selected")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if key != "batch_stability" {
		t.Errorf("Start() key = %q, want batch_stability (global singleton)", key)
	}

	// 任务运行中:订阅应成功。
	sub, err := mgr.Subscribe(key)
	if err != nil {
		t.Fatalf("Subscribe() error = %v, want nil for running job", err)
	}
	sub.Close()

	if !mgr.Cancel(key) {
		t.Error("Cancel() = false, want true for running job")
	}

	// 等取消生效后再次取消应失败(任务已收口)。
	deadline := time.Now().Add(2 * time.Second)
	for mgr.Cancel(key) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
}
