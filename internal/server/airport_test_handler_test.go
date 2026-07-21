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

// mockFetcher is a fake fetcher for testing.
type mockFetcher struct {
	response *subscription.Subscription
	err      error
}

func (m *mockFetcher) Fetch(name, url string) (*subscription.Subscription, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.response, nil
}

func TestHandleAirportTest_Success(t *testing.T) {
	nodes := []*subscription.Node{
		{Name: "test-vmess", Type: "vmess", Server: "example.com", Port: 443, Source: "test-airport"},
		{Name: "test-ss", Type: "ss", Server: "example.com", Port: 8080, Source: "test-airport"},
	}
	srv, st := newTestServer(t, nodes)

	airport, _ := st.CreateAirport("TestAirport", "https://example.com/sub")

	// Inject mock fetcher
	mockFetch := &mockFetcher{
		response: &subscription.Subscription{
			Name: "TestAirport",
			URL:  "https://example.com/sub",
			Nodes: []*subscription.Node{
				{Name: "node1", Type: "vmess", Server: "example.com", Port: 443},
				{Name: "node2", Type: "ss", Server: "example.com", Port: 8080},
			},
		},
	}
	srv.testOrchestrator = airporttest.NewOrchestrator(mockFetch, airporttest.NewStoreAdapter(st))

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
	airport, _ := st.CreateAirport("BadAirport", "https://example.com/bad")

	mockFetch := &mockFetcher{
		err: fmt.Errorf("connection timeout"),
	}
	srv.testOrchestrator = airporttest.NewOrchestrator(mockFetch, airporttest.NewStoreAdapter(st))

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
	srv.testOrchestrator = airporttest.NewOrchestrator(&mockFetcher{}, airporttest.NewStoreAdapter(st))

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

	srv.testOrchestrator = airporttest.NewOrchestrator(&mockFetcher{}, airporttest.NewStoreAdapter(st))

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
