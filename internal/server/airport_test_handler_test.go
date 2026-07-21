package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/taliove/proxyhub/internal/airporttest"
	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

// noopHealthChecker 诊断阶段测试用:检活阶段不真正执行(异步 goroutine 里拿到 nil 结果)
type noopHealthChecker struct{}

func (noopHealthChecker) CheckAll(_ context.Context, _ []*subscription.Node) []*airporttest.HealthCheckResult {
	return nil
}

// noopPoolWriter 诊断阶段测试用:不写回节点池
type noopPoolWriter struct{}

func (noopPoolWriter) UpdateNodeTestResult(_, _ string, _ bool, _ int, _, _ float64) bool {
	return false
}

// fakePoolOps 池操作假实现:LoadPoolBySource 返回预设节点,Upsert 并入内存
type fakePoolOps struct {
	nodes    []*subscription.Node
	upserted []*subscription.Node
}

func (f *fakePoolOps) LoadPoolBySource(_ string) ([]*subscription.Node, error) {
	return f.nodes, nil
}

func (f *fakePoolOps) UpsertAirportNodes(_ string, fetched []*subscription.Node) error {
	f.upserted = fetched
	f.nodes = append(f.nodes, fetched...)
	return nil
}

// waitForRunTerminal 轮询 run 到终态(completed/failed)再返回。
// POST 后检活+评分在后台 goroutine 执行,不等它写完 DB 就结束测试,
// t.TempDir 清理会撞上正在写入的 SQLite 文件("directory not empty")。
func waitForRunTerminal(t *testing.T, st *store.Store, airportID, runID int64) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		run, err := st.GetAirportTestRun(context.Background(), airportID, runID)
		if err == nil && (run.Status == "completed" || run.Status == "failed") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("run %d did not reach terminal state within 5s", runID)
}

func TestHandleAirportTest_Success(t *testing.T) {
	nodes := []*subscription.Node{
		{Name: "test-vmess", Type: "vmess", Server: "example.com", Port: 443, Source: "test-airport"},
		{Name: "test-ss", Type: "ss", Server: "example.com", Port: 8080, Source: "test-airport"},
	}
	srv, st := newTestServer(t, nodes)

	// Mock subscription server
	mockSub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return base64 encoded subscription with 2 nodes
		content := `vmess://eyJhZGQiOiJleGFtcGxlLmNvbSIsInBvcnQiOjQ0MywiaWQiOiIwMDAwMDAwMC0wMDAwLTAwMDAtMDAwMC0wMDAwMDAwMDAwMDAiLCJhaWQiOjAsIm5ldCI6InRjcCIsInRscyI6InRscyIsInBzIjoiVGVzdCBWTWVzcyJ9
ss://YWVzLTI1Ni1nY206cGFzc3dvcmQ=@example.com:8080#Test%20SS`
		w.Write([]byte(content))
	}))
	defer mockSub.Close()

	airport, _ := st.CreateAirport("TestAirport", mockSub.URL)

	srv.testOrchestrator = airporttest.NewOrchestratorWithPoolOps(airporttest.NewStoreAdapter(st), noopHealthChecker{}, noopPoolWriter{}, &fakePoolOps{nodes: nodes})

	// Record node pool state before test
	nodesBefore := srv.nodes.Nodes()

	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/airports/%d/test", airport.ID), strings.NewReader(`{"full":true}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleAirportTest(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response: %v", err)
	}

	if resp["id"] == nil {
		t.Fatalf("missing run id in response")
	}
	// New flow: handler returns immediately, async RunTest may complete quickly in test env
	// Accept diagnosing, checking, scoring, or completed
	status := resp["status"].(string)
	validStatuses := []string{"diagnosing", "checking", "scoring", "completed"}
	found := false
	for _, s := range validStatuses {
		if status == s {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("status = %v, want one of %v", status, validStatuses)
	}

	dims := resp["dimensions_json"].(string)
	var diag airporttest.DiagnosticResult
	if err := json.Unmarshal([]byte(dims), &diag); err != nil {
		t.Fatalf("parse dimensions: %v", err)
	}

	if diag.NodeCount != 2 {
		t.Errorf("node_count = %d, want 2", diag.NodeCount)
	}
	if diag.HTTPStatus != 200 {
		t.Errorf("http_status = %d, want 200", diag.HTTPStatus)
	}

	// Verify node pool unchanged (diagnostic + test don't touch pool without poolOps)
	nodesAfter := srv.nodes.Nodes()
	if len(nodesAfter) != len(nodesBefore) {
		t.Errorf("node pool changed: before %d, after %d", len(nodesBefore), len(nodesAfter))
	}

	// 等后台检活 goroutine 写完 DB,避免 TempDir 清理竞态
	waitForRunTerminal(t, st, airport.ID, int64(resp["id"].(float64)))
}

func TestHandleAirportTest_FetchFailure(t *testing.T) {
	srv, st := newTestServer(t, nil)
	// URL that will fail to connect
	airport, _ := st.CreateAirport("BadAirport", "http://localhost:1")

	srv.testOrchestrator = airporttest.NewOrchestratorWithPoolOps(airporttest.NewStoreAdapter(st), noopHealthChecker{}, noopPoolWriter{}, &fakePoolOps{})

	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/airports/%d/test", airport.ID), nil)
	w := httptest.NewRecorder()
	srv.handleAirportTest(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (fetch failure doesn't block handler)", w.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response: %v", err)
	}

	// New flow: fetch failure is recorded in diagnostic but handler returns 200 with diagnosing status
	// Async RunTest will fail it if pool is also empty
	if resp["status"] != "diagnosing" {
		t.Errorf("status = %v, want diagnosing (fetch failure non-blocking)", resp["status"])
	}
	// Diagnostic result will have HTTPStatus=0 indicating fetch failure
	dims := resp["dimensions_json"].(string)
	var diag airporttest.DiagnosticResult
	if err := json.Unmarshal([]byte(dims), &diag); err != nil {
		t.Fatalf("parse dimensions: %v", err)
	}
	if diag.HTTPStatus != 0 {
		t.Errorf("http_status = %d, want 0 (fetch failed)", diag.HTTPStatus)
	}

	// 池空 + URL 不可达:后台 RunTest 应把 run 置为 failed;等终态避免 TempDir 清理竞态
	waitForRunTerminal(t, st, airport.ID, int64(resp["id"].(float64)))
	run, err := st.GetAirportTestRun(context.Background(), airport.ID, int64(resp["id"].(float64)))
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if run.Status != "failed" {
		t.Errorf("final status = %v, want failed (pool empty + URL unreachable)", run.Status)
	}
}

func TestHandleAirportTest_NotFound(t *testing.T) {
	srv, st := newTestServer(t, nil)
	srv.testOrchestrator = airporttest.NewOrchestrator(airporttest.NewStoreAdapter(st), noopHealthChecker{}, noopPoolWriter{})

	req := httptest.NewRequest(http.MethodPost, "/api/airports/99999/test", nil)
	w := httptest.NewRecorder()
	srv.handleAirportTest(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestHandleAirportTest_Disabled(t *testing.T) {
	srv, st := newTestServer(t, nil)
	airport, _ := st.CreateAirport("DisabledAirport", "https://example.com/sub")
	st.SetAirportEnabled(airport.ID, false)

	srv.testOrchestrator = airporttest.NewOrchestrator(airporttest.NewStoreAdapter(st), noopHealthChecker{}, noopPoolWriter{})

	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/airports/%d/test", airport.ID), nil)
	w := httptest.NewRecorder()
	srv.handleAirportTest(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestHandleGetAirportTestRun_Success(t *testing.T) {
	srv, st := newTestServer(t, nil)
	airport, _ := st.CreateAirport("TestAirport", "https://example.com/sub")

	run := &store.AirportTestRun{
		AirportID:      airport.ID,
		Status:         "completed",
		DimensionsJSON: `{"http_status":200,"node_count":5}`,
	}
	runID, _ := st.CreateAirportTestRun(context.Background(), run)

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/airports/%d/test/runs/%d", airport.ID, runID), nil)
	w := httptest.NewRecorder()
	srv.handleGetAirportTestRun(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response: %v", err)
	}

	if resp["id"] == nil {
		t.Error("missing id in response")
	}
	if resp["status"] != "completed" {
		t.Errorf("status = %v, want completed", resp["status"])
	}
}

func TestHandleListAirportTestRuns_Success(t *testing.T) {
	srv, st := newTestServer(t, nil)
	airport, _ := st.CreateAirport("TestAirport", "https://example.com/sub")

	// Create 3 test runs
	for i := 0; i < 3; i++ {
		run := &store.AirportTestRun{
			AirportID:      airport.ID,
			Status:         "completed",
			DimensionsJSON: fmt.Sprintf(`{"http_status":200,"node_count":%d}`, i+1),
			OverallScore:   floatPtr(float64(80 + i)),
		}
		st.CreateAirportTestRun(context.Background(), run)
	}

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/airports/%d/test/runs", airport.ID), nil)
	w := httptest.NewRecorder()
	srv.handleListAirportTestRuns(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var resp []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response: %v", err)
	}

	if len(resp) != 3 {
		t.Fatalf("got %d runs, want 3", len(resp))
	}

	// Verify descending order (newest first)
	for i := 0; i < len(resp)-1; i++ {
		id1 := resp[i]["id"].(float64)
		id2 := resp[i+1]["id"].(float64)
		if id1 < id2 {
			t.Errorf("runs not in descending order: run[%d].id=%v < run[%d].id=%v", i, id1, i+1, id2)
		}
	}
}

func TestHandleListAirportTestRuns_Limit(t *testing.T) {
	srv, st := newTestServer(t, nil)
	airport, _ := st.CreateAirport("TestAirport", "https://example.com/sub")

	// Create 35 runs
	for i := 0; i < 35; i++ {
		run := &store.AirportTestRun{
			AirportID:      airport.ID,
			Status:         "completed",
			DimensionsJSON: `{"http_status":200}`,
		}
		st.CreateAirportTestRun(context.Background(), run)
	}

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/airports/%d/test/runs", airport.ID), nil)
	w := httptest.NewRecorder()
	srv.handleListAirportTestRuns(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}

	var resp []map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)

	if len(resp) != 30 {
		t.Errorf("got %d runs, want 30 (limit)", len(resp))
	}
}

func TestHandleListAirportTestRuns_NotFound(t *testing.T) {
	srv, st := newTestServer(t, nil)
	srv.testOrchestrator = airporttest.NewOrchestrator(airporttest.NewStoreAdapter(st), noopHealthChecker{}, noopPoolWriter{})

	req := httptest.NewRequest(http.MethodGet, "/api/airports/99999/test/runs", nil)
	w := httptest.NewRecorder()
	srv.handleListAirportTestRuns(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func floatPtr(f float64) *float64 {
	return &f
}
