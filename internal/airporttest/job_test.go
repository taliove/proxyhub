package airporttest

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/taliove/proxyhub/internal/jobs"
	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

// fakeFetch 返回固定诊断与节点的拉取缝。
func fakeFetch(diag *DiagnosticResult, nodes []*subscription.Node) FetchFunc {
	return func(context.Context, string, string) (*DiagnosticResult, []*subscription.Node) {
		return diag, nodes
	}
}

func testNodes(n int) []*subscription.Node {
	nodes := make([]*subscription.Node, n)
	for i := range nodes {
		nodes[i] = &subscription.Node{
			Name:   "HK-Node",
			Server: "example.com",
			Port:   10000 + i,
			Region: "HK",
			Source: "TestAirport",
		}
	}
	return nodes
}

func jobParams(t *testing.T, p JobParams) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	return data
}

// 自然完成:run 建行带 job_id 与诊断数据,终态 completed,Run 返回 nil(jobs 行 done)。
func TestJobKind_RunCompletes(t *testing.T) {
	st := NewFakeStore(t)
	orch := NewOrchestrator(st, &FakeHealthChecker{}, &FakePoolWriter{})
	kind := NewJobKind(orch, st,
		fakeFetch(&DiagnosticResult{HTTPStatus: 200, NodeCount: 3}, testNodes(3)),
		func(key string) int64 {
			if key != "airport-7" {
				t.Errorf("jobIDOf key = %q, want airport-7", key)
			}
			return 42
		})

	var cursors []string
	err := kind.Run(context.Background(),
		jobParams(t, JobParams{AirportID: 7, AirportName: "TestAirport", Full: true}),
		"", nil, func(c string) { cursors = append(cursors, c) })
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(st.Runs) != 1 {
		t.Fatalf("runs created = %d, want 1", len(st.Runs))
	}
	run := st.FirstRun()
	if run.JobID != 42 {
		t.Errorf("run JobID = %d, want 42", run.JobID)
	}
	if run.Status != StatusCompleted {
		t.Errorf("run Status = %s, want completed", run.Status)
	}
	if run.OverallScore == nil {
		t.Error("run OverallScore nil, want scored")
	}
	if len(cursors) == 0 {
		t.Error("no progress cursors reported")
	}
	// 首帧游标:诊断阶段
	var first jobCursor
	if err := json.Unmarshal([]byte(cursors[0]), &first); err != nil {
		t.Fatalf("parse cursor: %v", err)
	}
	if first.Phase != string(StatusDiagnosing) {
		t.Errorf("first cursor phase = %q, want diagnosing", first.Phase)
	}
}

// 业务失败(池空 + URL 不通):run 标 failed,Run 返回非取消错误(jobs 行 failed)。
func TestJobKind_RunFailedMapsToError(t *testing.T) {
	st := NewFakeStore(t)
	orch := NewOrchestratorWithPoolOps(st, &FakeHealthChecker{}, &FakePoolWriter{}, &MockPoolOperations{})
	kind := NewJobKind(orch, st,
		fakeFetch(&DiagnosticResult{HTTPStatus: 0}, nil), // URL 不可达
		func(string) int64 { return 0 })

	err := kind.Run(context.Background(),
		jobParams(t, JobParams{AirportID: 1, AirportName: "TestAirport"}),
		"", nil, nil)
	if err == nil {
		t.Fatal("Run() = nil, want error for failed run")
	}
	if errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want non-cancel error", err)
	}

	run := st.FirstRun()
	if run == nil || run.Status != StatusFailed {
		t.Fatalf("run Status = %v, want failed", run)
	}
}

// blockingChecker 检活到 ctx 取消:首节点给真实结果,其余返回 ctx.Canceled 错误。
type blockingChecker struct {
	release chan struct{}
	once    sync.Once
}

func (b *blockingChecker) CheckAll(ctx context.Context, nodes []*subscription.Node) []*HealthCheckResult {
	<-ctx.Done()
	b.once.Do(func() { close(b.release) })
	results := make([]*HealthCheckResult, len(nodes))
	for i, n := range nodes {
		if i == 0 {
			results[i] = &HealthCheckResult{Node: n, Available: true, Latency: 100}
		} else {
			results[i] = &HealthCheckResult{Node: n, Available: false, Error: ctx.Err()}
		}
	}
	return results
}

// 取消:编排停止,run 标 cancelled 保留诊断数据;真实完成的首节点写回不回滚,
// 取消诱导的失败结果不回写池。
func TestJobKind_RunCancelled(t *testing.T) {
	st := NewFakeStore(t)
	checker := &blockingChecker{release: make(chan struct{})}
	writer := &FakePoolWriter{}
	orch := NewOrchestrator(st, checker, writer)
	kind := NewJobKind(orch, st,
		fakeFetch(&DiagnosticResult{HTTPStatus: 200, NodeCount: 3}, testNodes(3)),
		func(string) int64 { return 9 })

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- kind.Run(ctx,
			jobParams(t, JobParams{AirportID: 1, AirportName: "TestAirport", Full: true}),
			"", nil, nil)
	}()

	// 等建行进入检活阶段后取消
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if st.RunCount() > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if st.RunCount() == 0 {
		t.Fatal("run row not created before cancel")
	}
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not return after cancel")
	}

	run := st.FirstRun()
	if run.Status != StatusCancelled {
		t.Errorf("run Status = %s, want cancelled", run.Status)
	}
	if run.DimensionsJSON == "" || run.DimensionsJSON == "{}" {
		t.Error("cancelled run lost diagnostic data")
	}
	// 真实完成的首节点写回保留(不回滚);取消诱导的 2 个失败结果不回写
	if len(writer.Updates) != 1 {
		t.Fatalf("writeback count = %d, want 1 (only the genuinely completed node)", len(writer.Updates))
	}
	if !writer.Updates[0].Available {
		t.Error("written-back node should be available (real measurement)")
	}
}

// 拉取期间被取消:尚未建行,直接取消收口,无 run 产出。
func TestJobKind_CancelDuringFetchNoRun(t *testing.T) {
	st := NewFakeStore(t)
	orch := NewOrchestrator(st, &FakeHealthChecker{}, &FakePoolWriter{})
	kind := NewJobKind(orch, st,
		func(ctx context.Context, _, _ string) (*DiagnosticResult, []*subscription.Node) {
			<-ctx.Done()
			return &DiagnosticResult{}, nil
		},
		func(string) int64 { return 0 })

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- kind.Run(ctx,
			jobParams(t, JobParams{AirportID: 1, AirportName: "A"}),
			"", nil, nil)
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()

	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
	if st.RunCount() != 0 {
		t.Errorf("runs created = %d, want 0 (cancelled before row creation)", st.RunCount())
	}
}

// 安全回归:订阅 URL 是凭证,不落 params_json;Run 按 airport_id 经 store 解析 URL。
func TestJobKind_RunResolvesURLFromStore(t *testing.T) {
	st := NewFakeStore(t)
	st.AirportURL = "https://example.com/sub/STORE-TOKEN-9f8e7d"
	orch := NewOrchestrator(st, &FakeHealthChecker{}, &FakePoolWriter{})
	var gotURL string
	kind := NewJobKind(orch, st,
		func(_ context.Context, _, url string) (*DiagnosticResult, []*subscription.Node) {
			gotURL = url
			return &DiagnosticResult{HTTPStatus: 200, NodeCount: 2}, testNodes(2)
		},
		func(string) int64 { return 0 })

	// params 不含 airport_url;URL 必须由 Run 从 store 解析
	err := kind.Run(context.Background(),
		json.RawMessage(`{"airport_id":7,"airport_name":"TestAirport","full":true}`),
		"", nil, nil)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if gotURL != st.AirportURL {
		t.Errorf("fetch url = %q, want store-resolved %q", gotURL, st.AirportURL)
	}
}

// 入队后机场被删除:Run 以明确的非凭证错误收口(ErrAirportGone),
// 不发起拉取、不建行(jobs 行 failed)。
func TestJobKind_RunFailsWhenAirportDeleted(t *testing.T) {
	st := NewFakeStore(t)
	st.AirportURLErr = ErrAirportGone
	orch := NewOrchestrator(st, &FakeHealthChecker{}, &FakePoolWriter{})
	fetchCalled := false
	kind := NewJobKind(orch, st,
		func(context.Context, string, string) (*DiagnosticResult, []*subscription.Node) {
			fetchCalled = true
			return &DiagnosticResult{}, nil
		},
		func(string) int64 { return 0 })

	err := kind.Run(context.Background(),
		json.RawMessage(`{"airport_id":7,"airport_name":"TestAirport"}`),
		"", nil, nil)
	if err == nil {
		t.Fatal("Run() = nil, want error for deleted airport")
	}
	if !errors.Is(err, ErrAirportGone) {
		t.Errorf("Run() error = %v, want wrapping ErrAirportGone", err)
	}
	if fetchCalled {
		t.Error("fetch called, want no fetch when airport is gone")
	}
	if st.RunCount() != 0 {
		t.Errorf("runs created = %d, want 0", st.RunCount())
	}
}

// 重启残留:进行中的 airport_test 任务行由 Recover 标 interrupted,不续跑
// (Resumable=false,对齐 refresh);run 行残留由 store.FailRunningAirportTestRuns
// 收口为 failed(0024 已覆盖,此处确认 kind 侧衔接)。
func TestJobKind_RecoverMarksInterrupted(t *testing.T) {
	st, err := store.OpenForTesting(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.OpenForTesting() error = %v", err)
	}
	t.Cleanup(func() { st.Close() })

	jobStore := st.Jobs()
	rowID, err := jobStore.Insert(JobKindName, JobKey(1), json.RawMessage(`{"airport_id":1}`))
	if err != nil {
		t.Fatalf("Insert() error = %v", err)
	}

	mgr := jobs.NewManager(jobStore)
	orch := NewOrchestrator(NewFakeStore(t), &FakeHealthChecker{}, &FakePoolWriter{})
	mgr.Register(NewJobKind(orch, NewFakeStore(t), fakeFetch(&DiagnosticResult{}, nil), func(string) int64 { return 0 }))
	if err := mgr.RecoverOwn(); err != nil {
		t.Fatalf("RecoverOwn() error = %v", err)
	}

	rec, err := jobStore.Get(rowID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if rec.Status != jobs.StatusInterrupted {
		t.Errorf("job status = %s, want interrupted", rec.Status)
	}
	if keys := mgr.RunningKeys(JobKindName); len(keys) != 0 {
		t.Errorf("running keys = %v, want none (no resume)", keys)
	}
}
