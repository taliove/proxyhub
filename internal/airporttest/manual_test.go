package airporttest

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

// countingFetch 记录调用次数的拉取缝:手动机场必须一次都不调。
type countingFetch struct {
	calls int
	diag  *DiagnosticResult
	nodes []*subscription.Node
}

func (c *countingFetch) fetch(context.Context, string, string) (*DiagnosticResult, []*subscription.Node) {
	c.calls++
	return c.diag, c.nodes
}

// TestJobKind_ManualAirportSkipsFetch 手动机场:诊断段跳过 URL 拉取并显式标 N/A
// (ManualSource);池有节点照常测试,评分走权重重归一(httpStatus=0 现成语义)。
func TestJobKind_ManualAirportSkipsFetch(t *testing.T) {
	st := NewFakeStore(t)
	st.AirportSourceType = store.AirportSourceManual

	poolNodes := testNodes(3)
	poolOps := &MockPoolOperations{ExistingPool: poolNodes}
	orch := NewOrchestratorWithPoolOps(st, &FakeHealthChecker{}, &FakePoolWriter{}, poolOps)
	fetcher := &countingFetch{diag: &DiagnosticResult{HTTPStatus: 200}, nodes: poolNodes}
	kind := NewJobKind(orch, st, fetcher.fetch, func(string) int64 { return 0 })

	err := kind.Run(context.Background(),
		jobParams(t, JobParams{AirportID: 9, AirportName: "TestAirport"}),
		"", nil, nil)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if fetcher.calls != 0 {
		t.Errorf("fetch called %d times, want 0 (手动机场不拉 URL)", fetcher.calls)
	}

	run := st.FirstRun()
	if run == nil || run.Status != StatusCompleted {
		t.Fatalf("run Status = %v, want completed", run)
	}
	// 评分走 URL 不可达重归一:诊断 HTTPStatus=0 时拉取健康 N/A,有分即证明重归一生效
	if run.OverallScore == nil {
		t.Error("OverallScore nil, want scored under renormalized weights")
	}
}

// TestJobKind_ManualAirportEmptyPoolFails 手动机场 + 池空:run failed,
// 文案引导粘贴导入(无"订阅 URL 不可达"这种对手动机场无意义的措辞)。
func TestJobKind_ManualAirportEmptyPoolFails(t *testing.T) {
	st := NewFakeStore(t)
	st.AirportSourceType = store.AirportSourceManual

	orch := NewOrchestratorWithPoolOps(st, &FakeHealthChecker{}, &FakePoolWriter{}, &MockPoolOperations{})
	fetcher := &countingFetch{}
	kind := NewJobKind(orch, st, fetcher.fetch, func(string) int64 { return 0 })

	err := kind.Run(context.Background(),
		jobParams(t, JobParams{AirportID: 9, AirportName: "TestAirport"}),
		"", nil, nil)
	if err == nil {
		t.Fatal("Run() = nil, want error for failed run")
	}
	if fetcher.calls != 0 {
		t.Errorf("fetch called %d times, want 0", fetcher.calls)
	}

	run := st.FirstRun()
	if run == nil || run.Status != StatusFailed {
		t.Fatalf("run Status = %v, want failed", run)
	}
	if !strings.Contains(run.ErrorMessage, "manual airport") {
		t.Errorf("ErrorMessage = %q, want manual-airport wording", run.ErrorMessage)
	}
}

// TestManualDiagnosticJSONRoundTrip 诊断段 N/A 标记随 dimensions_json 序列化往返
// (前端据此渲染 N/A 而非"拉取失败"):手动机场建行时 diag 只带 ManualSource。
func TestManualDiagnosticJSONRoundTrip(t *testing.T) {
	diag := &DiagnosticResult{ManualSource: true, ProtocolCounts: map[string]int{}}
	data, err := json.Marshal(diag)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back DiagnosticResult
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !back.ManualSource {
		t.Error("ManualSource lost in JSON round trip")
	}
	if back.HTTPStatus != 0 {
		t.Errorf("HTTPStatus = %d, want 0 (N/A 而非具体状态码)", back.HTTPStatus)
	}
}
