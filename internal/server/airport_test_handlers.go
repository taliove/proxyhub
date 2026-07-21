package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/taliove/proxyhub/internal/airporttest"
	"github.com/taliove/proxyhub/internal/subscription"
)

// handleAirportTest executes airport test with pool-first logic.
// Diagnostic runs sync, then dispatches async test (sampling + health check + scoring).
// New flow: diagnostic non-blocking, pool-aware branching in RunTest.
func (s *Server) handleAirportTest(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/api/airports/")
	idStr = strings.TrimSuffix(idStr, "/test")
	airportID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid airport id", http.StatusBadRequest)
		return
	}

	airport, err := s.st.GetAirportByID(airportID)
	if err != nil {
		http.Error(w, "airport not found", http.StatusNotFound)
		return
	}

	if !airport.Enabled {
		http.Error(w, "airport is disabled", http.StatusBadRequest)
		return
	}

	var body struct {
		Full bool `json:"full"`
	}
	if r.Body != nil {
		json.NewDecoder(r.Body).Decode(&body)
	}

	// Diagnostic phase: attempt fetch (non-blocking on failure in pool-aware mode)
	ctx := context.Background()
	start := time.Now()

	var fetchedNodes []*subscription.Node
	diagResult := &airporttest.DiagnosticResult{}

	sub, fetchErr := subscription.NewFetcher(30 * time.Second).Fetch(airport.Name, airport.URL)
	elapsed := time.Since(start)

	if fetchErr != nil {
		// Fetch failed: record diagnostic failure, but don't block run
		diagResult.HTTPStatus = 0 // Will be interpreted as non-2xx
		diagResult.DurationMs = elapsed.Milliseconds()
		diagResult.NodeCount = 0
		diagResult.ParseFailures = 0
		diagResult.ProtocolCounts = make(map[string]int)
	} else {
		// Fetch succeeded: populate diagnostic result
		diagResult.HTTPStatus = 200
		diagResult.DurationMs = elapsed.Milliseconds()
		diagResult.NodeCount = len(sub.Nodes)
		diagResult.ParseFailures = 0 // subscription.Fetch doesn't expose parse stats, assume 0
		diagResult.ProtocolCounts = make(map[string]int)
		for _, node := range sub.Nodes {
			diagResult.ProtocolCounts[node.Type]++
		}
		fetchedNodes = sub.Nodes
	}

	// Create run with diagnostic result
	run := &airporttest.TestRun{
		AirportID:    airport.ID,
		CreatedAt:    time.Now().UTC(),
		SampleParams: "{}",
		IsFull:       body.Full,
		Status:       airporttest.StatusDiagnosing,
	}

	dimsJSON, _ := json.Marshal(diagResult)
	run.DimensionsJSON = string(dimsJSON)

	// Use store adapter to create run (converts TestRun to store.AirportTestRun)
	storeAdapter := airporttest.NewStoreAdapter(s.st)
	runID, err := storeAdapter.CreateTestRun(ctx, run)
	if err != nil {
		http.Error(w, fmt.Sprintf("create test run: %v", err), http.StatusInternalServerError)
		return
	}
	run.ID = runID

	// Async: pool-aware test execution (sampling + health check + scoring)
	go func() {
		ctx := context.Background()
		s.testOrchestrator.RunTest(ctx, run, airport.Name, fetchedNodes, diagResult)
	}()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(run)
}

// handleGetAirportTestRun retrieves a test run by ID.
func (s *Server) handleGetAirportTestRun(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/airports/"), "/")
	if len(parts) < 4 {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	airportID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		http.Error(w, "invalid airport id", http.StatusBadRequest)
		return
	}

	runID, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil {
		http.Error(w, "invalid run id", http.StatusBadRequest)
		return
	}

	run, err := s.st.GetAirportTestRun(context.Background(), airportID, runID)
	if err != nil {
		http.Error(w, "run not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(run)
}

// handleListAirportTestRuns retrieves recent test runs for an airport.
func (s *Server) handleListAirportTestRuns(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/api/airports/")
	idStr = strings.TrimSuffix(idStr, "/test/runs")
	airportID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid airport id", http.StatusBadRequest)
		return
	}

	// Verify airport exists
	_, err = s.st.GetAirportByID(airportID)
	if err != nil {
		http.Error(w, "airport not found", http.StatusNotFound)
		return
	}

	runs, err := s.st.ListAirportTestRuns(context.Background(), airportID, 30)
	if err != nil {
		http.Error(w, "failed to retrieve runs", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(runs)
}
