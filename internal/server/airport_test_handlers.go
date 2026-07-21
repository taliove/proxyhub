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

// handleAirportTest executes diagnostic phase for an airport and triggers async test.
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

	// 阶段1:诊断(同步)
	run, err := s.testOrchestrator.RunDiagnostic(
		context.Background(),
		airport.ID,
		airport.Name,
		airport.URL,
		body.Full,
	)
	if err != nil {
		http.Error(w, fmt.Sprintf("diagnostic failed: %v", err), http.StatusInternalServerError)
		return
	}

	// 阶段2-4:异步执行(抽样 + 检活 + 评分)
	// 解析诊断结果以获取节点
	var diagResult airporttest.DiagnosticResult
	json.Unmarshal([]byte(run.DimensionsJSON), &diagResult)

	// 重新拉取节点(诊断已验证可拉取)
	go func() {
		ctx := context.Background()
		sub, err := subscription.NewFetcher(30 * time.Second).Fetch(airport.Name, airport.URL)
		if err != nil {
			s.logger.Warn("async fetch failed", "airport", airport.Name, "error", err)
			return
		}
		s.testOrchestrator.RunTest(ctx, run, sub.Nodes, &diagResult)
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
