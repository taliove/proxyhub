package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// handleAirportTest executes diagnostic phase for an airport.
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

	run, err := s.testOrchestrator.RunDiagnostic(
		context.Background(),
		airport.ID,
		airport.Name,
		airport.URL,
		body.Full,
	)
	if err != nil {
		http.Error(w, fmt.Sprintf("test execution failed: %v", err), http.StatusInternalServerError)
		return
	}

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
