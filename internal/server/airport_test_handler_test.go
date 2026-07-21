package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

	srv.testOrchestrator = airporttest.NewOrchestrator(airporttest.NewStoreAdapter(st), noopHealthChecker{}, noopPoolWriter{})

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
	if resp["status"] != "completed" {
		t.Fatalf("status = %v, want completed", resp["status"])
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

	// Verify node pool unchanged
	nodesAfter := srv.nodes.Nodes()
	if len(nodesAfter) != len(nodesBefore) {
		t.Errorf("node pool changed: before %d, after %d (diagnostic should be read-only)", len(nodesBefore), len(nodesAfter))
	}
}

func TestHandleAirportTest_FetchFailure(t *testing.T) {
	srv, st := newTestServer(t, nil)
	// URL that will fail to connect
	airport, _ := st.CreateAirport("BadAirport", "http://localhost:1")

	srv.testOrchestrator = airporttest.NewOrchestrator(airporttest.NewStoreAdapter(st), noopHealthChecker{}, noopPoolWriter{})

	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/airports/%d/test", airport.ID), nil)
	w := httptest.NewRecorder()
	srv.handleAirportTest(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (failed run should return 200 with status=failed)", w.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response: %v", err)
	}

	if resp["status"] != "failed" {
		t.Errorf("status = %v, want failed", resp["status"])
	}
	if resp["error_message"] == nil || resp["error_message"] == "" {
		t.Error("expected error_message in failed run")
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
