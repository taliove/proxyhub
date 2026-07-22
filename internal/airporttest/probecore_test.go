package airporttest

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/taliove/proxyhub/internal/subscription"
)

// recordingPoolWriter captures the full writeback arguments (including
// failure classification) for assertions.
type recordingPoolWriter struct {
	updates []writebackCall
}

type writebackCall struct {
	nodeKey    string
	mode       string
	available  bool
	latency    int
	failReason string
	failDetail string
}

func (w *recordingPoolWriter) UpdateNodeTestResult(nodeKey, mode string, available bool, latency int, downMbps, upMbps float64, failReason, failDetail string) bool {
	w.updates = append(w.updates, writebackCall{
		nodeKey:    nodeKey,
		mode:       mode,
		available:  available,
		latency:    latency,
		failReason: failReason,
		failDetail: failDetail,
	})
	return true
}

// makeNodes builds n nodes in the given region with distinct ports.
func makeNodes(region string, n int) []*subscription.Node {
	nodes := make([]*subscription.Node, n)
	for i := range nodes {
		nodes[i] = &subscription.Node{
			Name:   fmt.Sprintf("%s-%d", region, i),
			Server: "1.1.1.1",
			Port:   10000 + i,
			Region: region,
		}
	}
	return nodes
}

func TestProbe_SamplingQuotas(t *testing.T) {
	tests := []struct {
		name        string
		nodes       []*subscription.Node
		full        bool
		wantSampled int
	}{
		{"priority region capped at quota", makeNodes("HK", 8), false, 5},
		{"default region capped at quota", makeNodes("JP", 6), false, 2},
		{"unknown region uses default quota", makeNodes("", 5), false, 2},
		{"under quota keeps all", makeNodes("SG", 3), false, 3},
		{"full mode checks everything", makeNodes("HK", 8), true, 8},
		{"empty input", []*subscription.Node{}, false, 0},
		{"mixed regions sum of layer quotas", append(makeNodes("HK", 6), makeNodes("JP", 4)...), false, 7},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checker := &FakeHealthChecker{}
			writer := &recordingPoolWriter{}
			core := NewProbeCore(checker, writer)

			var sampledHook = -1
			outcomes, err := core.Probe(context.Background(), tt.nodes, tt.full, ProbeHooks{
				OnSampled: func(sampled int) error {
					sampledHook = sampled
					return nil
				},
			})
			if err != nil {
				t.Fatalf("Probe failed: %v", err)
			}

			if sampledHook != tt.wantSampled {
				t.Errorf("OnSampled got %d, want %d", sampledHook, tt.wantSampled)
			}
			if len(outcomes) != tt.wantSampled {
				t.Errorf("outcomes got %d, want %d", len(outcomes), tt.wantSampled)
			}
			if len(writer.updates) != tt.wantSampled {
				t.Errorf("writebacks got %d, want %d", len(writer.updates), tt.wantSampled)
			}
			for _, o := range outcomes {
				if !o.Checked {
					t.Error("expected outcome to be marked checked")
				}
			}
		})
	}
}

func TestProbe_ProgressHookSequence(t *testing.T) {
	nodes := makeNodes("HK", 3)
	core := NewProbeCore(&FakeHealthChecker{}, &recordingPoolWriter{})

	var progress []int
	_, err := core.Probe(context.Background(), nodes, false, ProbeHooks{
		OnProgress: func(checked int) {
			progress = append(progress, checked)
		},
	})
	if err != nil {
		t.Fatalf("Probe failed: %v", err)
	}

	want := []int{1, 2, 3}
	if len(progress) != len(want) {
		t.Fatalf("progress calls got %d, want %d", len(progress), len(want))
	}
	for i := range want {
		if progress[i] != want[i] {
			t.Errorf("progress[%d] got %d, want %d", i, progress[i], want[i])
		}
	}
}

func TestProbe_MutatesCheckedNodes(t *testing.T) {
	nodes := makeNodes("HK", 2)
	core := NewProbeCore(&FakeHealthChecker{}, &recordingPoolWriter{})

	outcomes, err := core.Probe(context.Background(), nodes, false, ProbeHooks{})
	if err != nil {
		t.Fatalf("Probe failed: %v", err)
	}

	// FakeHealthChecker default: available, latency 100ms.
	for _, n := range nodes {
		if !n.Available || n.Latency != 100 {
			t.Errorf("node not updated in place: available=%v latency=%d", n.Available, n.Latency)
		}
	}
	for _, o := range outcomes {
		if !o.Available || o.Latency != 100 {
			t.Errorf("outcome mismatch: available=%v latency=%d", o.Available, o.Latency)
		}
	}
}

func TestProbe_FailureClassificationWrittenBack(t *testing.T) {
	nodes := makeNodes("HK", 1)
	checkErr := errors.New("connection refused")
	checker := &FakeHealthChecker{
		Results: []*HealthCheckResult{
			{Node: nodes[0], Available: false, Latency: 0, Error: checkErr},
		},
	}
	writer := &recordingPoolWriter{}
	core := NewProbeCore(checker, writer)

	outcomes, err := core.Probe(context.Background(), nodes, false, ProbeHooks{})
	if err != nil {
		t.Fatalf("Probe failed: %v", err)
	}

	if len(writer.updates) != 1 {
		t.Fatalf("writebacks got %d, want 1", len(writer.updates))
	}
	u := writer.updates[0]
	if u.failReason != "refused" {
		t.Errorf("failReason got %q, want %q", u.failReason, "refused")
	}
	if u.failDetail != checkErr.Error() {
		t.Errorf("failDetail got %q, want %q", u.failDetail, checkErr.Error())
	}
	if u.mode != "quick" {
		t.Errorf("mode got %q, want %q", u.mode, "quick")
	}
	if outcomes[0].Error != checkErr {
		t.Errorf("outcome error got %v, want %v", outcomes[0].Error, checkErr)
	}
}

func TestProbe_NilCheckerOrWriter(t *testing.T) {
	tests := []struct {
		name    string
		checker HealthChecker
		writer  PoolWriter
	}{
		{"nil checker", nil, &recordingPoolWriter{}},
		{"nil writer", &FakeHealthChecker{}, nil},
		{"both nil", nil, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nodes := makeNodes("HK", 8)
			core := NewProbeCore(tt.checker, tt.writer)

			var sampledHook = -1
			progressCalls := 0
			outcomes, err := core.Probe(context.Background(), nodes, false, ProbeHooks{
				OnSampled: func(sampled int) error {
					sampledHook = sampled
					return nil
				},
				OnProgress: func(checked int) {
					progressCalls++
				},
			})
			if err != nil {
				t.Fatalf("Probe failed: %v", err)
			}

			if sampledHook != 5 {
				t.Errorf("OnSampled got %d, want 5", sampledHook)
			}
			if len(outcomes) != 5 {
				t.Errorf("outcomes got %d, want 5", len(outcomes))
			}
			for _, o := range outcomes {
				if o.Checked {
					t.Error("outcome should not be marked checked")
				}
			}
			if progressCalls != 0 {
				t.Errorf("progress calls got %d, want 0", progressCalls)
			}
			// Nodes must remain untouched without a checker.
			for _, n := range nodes {
				if n.Available || n.Latency != 0 {
					t.Error("node mutated despite nil checker/writer")
				}
			}
		})
	}
}

func TestProbe_OnSampledErrorAborts(t *testing.T) {
	nodes := makeNodes("HK", 3)
	writer := &recordingPoolWriter{}
	core := NewProbeCore(&FakeHealthChecker{}, writer)

	hookErr := errors.New("persist failed")
	_, err := core.Probe(context.Background(), nodes, false, ProbeHooks{
		OnSampled: func(sampled int) error {
			return hookErr
		},
	})
	if !errors.Is(err, hookErr) {
		t.Fatalf("error got %v, want %v", err, hookErr)
	}
	if len(writer.updates) != 0 {
		t.Errorf("writebacks got %d, want 0 after abort", len(writer.updates))
	}
}

// 取消语义(issue 0025):ctx 已取消时,取消诱导的失败(ctx.Canceled)不是真实测量,
// 不回写池、不计进度,outcome 标 Checked=false;真实完成的结果照常写回。
func TestProbe_CancelSkipsCancelInducedFailures(t *testing.T) {
	nodes := makeNodes("HK", 3)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	checker := &FakeHealthChecker{
		Results: []*HealthCheckResult{
			{Node: nodes[0], Available: true, Latency: 100},      // 真实测量,仍写回
			{Node: nodes[1], Available: false, Error: ctx.Err()}, // 取消诱导,跳过
			{Node: nodes[2], Available: false, Error: ctx.Err()}, // 取消诱导,跳过
		},
	}
	writer := &recordingPoolWriter{}
	core := NewProbeCore(checker, writer)

	var progress []int
	outcomes, err := core.Probe(ctx, nodes, false, ProbeHooks{
		OnProgress: func(checked int) {
			progress = append(progress, checked)
		},
	})
	if err != nil {
		t.Fatalf("Probe failed: %v", err)
	}

	if len(writer.updates) != 1 {
		t.Errorf("writebacks got %d, want 1 (only the real measurement)", len(writer.updates))
	}
	if len(progress) != 1 || progress[0] != 1 {
		t.Errorf("progress got %v, want [1] (cancel-induced failures not counted)", progress)
	}
	if len(outcomes) != 3 {
		t.Fatalf("outcomes got %d, want 3", len(outcomes))
	}
	if !outcomes[0].Checked {
		t.Error("real measurement outcome should be Checked")
	}
	for _, o := range outcomes[1:] {
		if o.Checked {
			t.Error("cancel-induced outcome should not be Checked")
		}
		if !errors.Is(o.Error, context.Canceled) {
			t.Errorf("cancel-induced outcome error got %v, want context.Canceled", o.Error)
		}
	}
}
