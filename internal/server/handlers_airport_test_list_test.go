package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/taliove/proxyhub/internal/store"
)

func TestHandleListAirports_WithTestRuns(t *testing.T) {
	srv, st := newTestServer(t, nil)
	ctx := context.Background()

	// Create airports
	a1, _ := st.CreateAirport("Tested Airport", "https://example.com/tested")
	_, _ = st.CreateAirport("Untested Airport", "https://example.com/untested")

	// Add test run for a1
	score := 87.5
	run := &store.AirportTestRun{
		AirportID:      a1.ID,
		CreatedAt:      time.Now().UTC(),
		SampleParams:   "{}",
		Status:         "completed",
		OverallScore:   &score,
		DimensionsJSON: "{}",
	}
	st.CreateAirportTestRun(ctx, run)

	// Make request
	req := httptest.NewRequest(http.MethodGet, "/airports", nil)
	w := httptest.NewRecorder()
	srv.handleListAirports(w, req)

	// Check response
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var results []store.AirportWithTestRun
	if err := json.NewDecoder(w.Body).Decode(&results); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("got %d airports, want 2", len(results))
	}

	// Find tested and untested airports
	var tested, untested *store.AirportWithTestRun
	for i := range results {
		if results[i].Name == "Tested Airport" {
			tested = &results[i]
		} else if results[i].Name == "Untested Airport" {
			untested = &results[i]
		}
	}

	if tested == nil || untested == nil {
		t.Fatal("missing expected airports in response")
	}

	// Validate tested airport has test data
	if tested.LastTestScore == nil {
		t.Error("tested airport missing last_test_score")
	} else if *tested.LastTestScore != 87.5 {
		t.Errorf("last_test_score = %v, want 87.5", *tested.LastTestScore)
	}

	if tested.LastTestStatus == nil || *tested.LastTestStatus != "completed" {
		t.Errorf("last_test_status = %v, want 'completed'", tested.LastTestStatus)
	}

	if tested.LastTestAt == nil {
		t.Error("tested airport missing last_test_at")
	}

	// Validate untested airport has null test fields
	if untested.LastTestScore != nil {
		t.Errorf("untested airport should have nil score, got %v", *untested.LastTestScore)
	}
	if untested.LastTestStatus != nil {
		t.Errorf("untested airport should have nil status, got %v", *untested.LastTestStatus)
	}
	if untested.LastTestAt != nil {
		t.Errorf("untested airport should have nil test_at, got %v", *untested.LastTestAt)
	}
}

func TestHandleListAirports_FailedTestRun(t *testing.T) {
	srv, st := newTestServer(t, nil)
	ctx := context.Background()

	// Create airport
	a, _ := st.CreateAirport("Failed Test", "https://example.com/failed")

	// Add failed test run (no score)
	run := &store.AirportTestRun{
		AirportID:      a.ID,
		CreatedAt:      time.Now().UTC(),
		SampleParams:   "{}",
		Status:         "failed",
		OverallScore:   nil,
		DimensionsJSON: "{}",
		ErrorMessage:   "timeout",
	}
	st.CreateAirportTestRun(ctx, run)

	// Make request
	req := httptest.NewRequest(http.MethodGet, "/airports", nil)
	w := httptest.NewRecorder()
	srv.handleListAirports(w, req)

	var results []store.AirportWithTestRun
	json.NewDecoder(w.Body).Decode(&results)

	if len(results) != 1 {
		t.Fatalf("got %d airports, want 1", len(results))
	}

	result := results[0]
	if result.LastTestScore != nil {
		t.Errorf("failed test should have nil score, got %v", *result.LastTestScore)
	}
	if result.LastTestStatus == nil || *result.LastTestStatus != "failed" {
		t.Errorf("status = %v, want 'failed'", result.LastTestStatus)
	}
	if result.LastTestAt == nil {
		t.Error("failed test should still have timestamp")
	}
}
