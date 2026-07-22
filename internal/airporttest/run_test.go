package airporttest

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/taliove/proxyhub/internal/subscription"
)

// FakeHealthChecker 测试用假健康检查器
type FakeHealthChecker struct {
	Results []*HealthCheckResult
}

func (f *FakeHealthChecker) CheckAll(ctx context.Context, nodes []*subscription.Node) []*HealthCheckResult {
	if f.Results != nil {
		return f.Results
	}
	// 默认:全部可用,延迟100ms
	results := make([]*HealthCheckResult, len(nodes))
	for i, n := range nodes {
		results[i] = &HealthCheckResult{
			Node:      n,
			Available: true,
			Latency:   100,
		}
	}
	return results
}

// FakePoolWriter 测试用假池写入器
type FakePoolWriter struct {
	Updates []PoolUpdate
}

type PoolUpdate struct {
	NodeKey   string
	Available bool
	Latency   int
}

func (f *FakePoolWriter) UpdateNodeTestResult(nodeKey, mode string, available bool, latency int, downMbps, upMbps float64, failReason, failDetail string) bool {
	f.Updates = append(f.Updates, PoolUpdate{
		NodeKey:   nodeKey,
		Available: available,
		Latency:   latency,
	})
	return true
}

// FakeStore 测试用假存储(任务化后 kind 在 goroutine 里跑,读写须并发安全)
type FakeStore struct {
	mu        sync.Mutex
	Runs      map[int64]*TestRun
	NextRunID int64
	t         *testing.T
}

func NewFakeStore(t *testing.T) *FakeStore {
	return &FakeStore{
		Runs:      make(map[int64]*TestRun),
		NextRunID: 1,
		t:         t,
	}
}

func (s *FakeStore) CreateTestRun(ctx context.Context, run *TestRun) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.NextRunID
	s.NextRunID++
	run.ID = id
	s.Runs[id] = run
	return id, nil
}

func (s *FakeStore) GetTestRun(ctx context.Context, airportID, runID int64) (*TestRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	run, ok := s.Runs[runID]
	if !ok {
		return nil, nil
	}
	return run, nil
}

func (s *FakeStore) UpdateTestRun(ctx context.Context, run *TestRun) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Runs[run.ID] = run
	return nil
}

// RunCount 并发安全地读已建行数(任务化取消测试轮询用)。
func (s *FakeStore) RunCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.Runs)
}

// FirstRun 并发安全地取首条 run(单 run 场景断言用)。
func (s *FakeStore) FirstRun() *TestRun {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.Runs {
		return r
	}
	return nil
}

func TestRunTest_EmptyPool(t *testing.T) {
	store := NewFakeStore(t)
	checker := &FakeHealthChecker{}
	writer := &FakePoolWriter{}
	orch := NewOrchestrator(store, checker, writer)

	run := &TestRun{
		ID:        1,
		AirportID: 1,
		CreatedAt: time.Now(),
		Status:    StatusDiagnosing,
	}
	store.Runs[1] = run

	diagResult := &DiagnosticResult{
		HTTPStatus:     200,
		ParseFailures:  0,
		NodeCount:      0,
	}

	updatedRun, err := orch.RunTest(context.Background(), run, "TestAirport", []*subscription.Node{}, diagResult, nil)
	if err != nil {
		t.Fatalf("RunTest failed: %v", err)
	}
	if updatedRun.Status != StatusCompleted {
		t.Errorf("status got %s, want %s", updatedRun.Status, StatusCompleted)
	}
	if updatedRun.OverallScore == nil || *updatedRun.OverallScore != 0 {
		t.Errorf("empty pool should score 0")
	}
}

func TestRunTest_SamplingAndWriteback(t *testing.T) {
	store := NewFakeStore(t)
	checker := &FakeHealthChecker{}
	writer := &FakePoolWriter{}
	orch := NewOrchestrator(store, checker, writer)

	// 8个HK节点,应抽5个
	nodes := make([]*subscription.Node, 8)
	for i := range nodes {
		nodes[i] = &subscription.Node{
			Name:   "HK",
			Server: "1.1.1.1",
			Port:   10000 + i,
			Region: "HK",
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

	diagResult := &DiagnosticResult{
		HTTPStatus:    200,
		ParseFailures: 0,
		NodeCount:     8,
	}

	updatedRun, err := orch.RunTest(context.Background(), run, "TestAirport", nodes, diagResult, nil)
	if err != nil {
		t.Fatalf("RunTest failed: %v", err)
	}
	if updatedRun.Status != StatusCompleted {
		t.Errorf("status got %s, want %s", updatedRun.Status, StatusCompleted)
	}
	// 写回应该是5次(抽样配额)
	if len(writer.Updates) != 5 {
		t.Errorf("writeback count got %d, want 5", len(writer.Updates))
	}
}

func TestRunTest_FullMode(t *testing.T) {
	store := NewFakeStore(t)
	checker := &FakeHealthChecker{}
	writer := &FakePoolWriter{}
	orch := NewOrchestrator(store, checker, writer)

	nodes := make([]*subscription.Node, 8)
	for i := range nodes {
		nodes[i] = &subscription.Node{
			Name:   "HK",
			Server: "1.1.1.1",
			Port:   10000 + i,
			Region: "HK",
		}
	}

	run := &TestRun{
		ID:        1,
		AirportID: 1,
		CreatedAt: time.Now(),
		Status:    StatusDiagnosing,
		IsFull:    true, // 全量模式
	}
	store.Runs[1] = run

	diagResult := &DiagnosticResult{
		HTTPStatus:    200,
		ParseFailures: 0,
		NodeCount:     8,
	}

	updatedRun, err := orch.RunTest(context.Background(), run, "TestAirport", nodes, diagResult, nil)
	if err != nil {
		t.Fatalf("RunTest failed: %v", err)
	}
	// 全量模式应该检测全部8个
	if len(writer.Updates) != 8 {
		t.Errorf("full mode writeback count got %d, want 8", len(writer.Updates))
	}
	if updatedRun.Status != StatusCompleted {
		t.Errorf("status got %s, want completed", updatedRun.Status)
	}
}

func TestRunTest_StatusFlow(t *testing.T) {
	store := NewFakeStore(t)
	checker := &FakeHealthChecker{}
	writer := &FakePoolWriter{}
	orch := NewOrchestrator(store, checker, writer)

	nodes := []*subscription.Node{
		{Name: "HK", Server: "1.1.1.1", Port: 443, Region: "HK"},
	}

	run := &TestRun{
		ID:        1,
		AirportID: 1,
		CreatedAt: time.Now(),
		Status:    StatusDiagnosing,
	}
	store.Runs[1] = run

	diagResult := &DiagnosticResult{
		HTTPStatus:    200,
		ParseFailures: 0,
		NodeCount:     1,
	}

	_, err := orch.RunTest(context.Background(), run, "TestAirport", nodes, diagResult, nil)
	if err != nil {
		t.Fatalf("RunTest failed: %v", err)
	}

	// 验证状态流转:checking → scoring → completed
	finalRun := store.Runs[1]
	if finalRun.Status != StatusCompleted {
		t.Errorf("final status got %s, want %s", finalRun.Status, StatusCompleted)
	}
}
