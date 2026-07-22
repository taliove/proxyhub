package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/taliove/proxyhub/internal/aggregator"
	"github.com/taliove/proxyhub/internal/airporttest"
)

// handleAirportTest POST /api/airports/{id}/test 经 jobs 运行时发起机场测试任务
// (issue 0025 迁入:与 /airports/{id}/refresh 同构,返回任务句柄)。
// 诊断拉取与建行随任务执行(单实例语义下不再同步建行——连点附加到进行中任务,
// 同步建行会产生永不推进的孤儿 run);同机场刷新在跑时返回 409(跨 kind 互斥);
// 取消走通用 POST /api/jobs/{kind}/{key}/cancel(kind=airport_test)。
func (s *Server) handleAirportTest(w http.ResponseWriter, r *http.Request) {
	airportID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || airportID <= 0 {
		http.Error(w, "invalid airport id", http.StatusBadRequest)
		return
	}

	airport, err := s.st.GetAirportByID(airportID)
	if err != nil {
		http.Error(w, "airport not found", http.StatusNotFound)
		return
	}

	// 禁用机场可测(与订阅测试对称,ADR 0027 决策 4):测"如果启用会怎样",不被启停拦截。

	var body struct {
		Full bool `json:"full"`
	}
	if r.Body != nil {
		json.NewDecoder(r.Body).Decode(&body)
	}

	params, err := json.Marshal(airporttest.JobParams{
		AirportID:   airport.ID,
		AirportName: airport.Name,
		AirportURL:  airport.URL,
		Full:        body.Full,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("marshal test params: %v", err), http.StatusInternalServerError)
		return
	}

	startFn := func() (int64, string, bool, error) {
		key := airporttest.JobKey(airport.ID)
		rowID, started, err := s.airportTestJobs.OpenIDForce(airporttest.JobKindName, key, params)
		return rowID, key, started, err
	}

	var jobID int64
	var key string
	var started bool
	if c, ok := s.nodes.(airportTestCoordinator); ok {
		// 跨 kind 互斥临界区:同机场/全量刷新在跑 → 409
		jobID, key, started, err = c.StartAirportTestExclusive(airport.ID, startFn)
	} else {
		// 无协调器(单测 fake):退化为无跨 kind 互斥,kind+key 单实例仍生效
		jobID, key, started, err = startFn()
	}
	if err != nil {
		if errors.Is(err, aggregator.ErrAirportTestConflict) {
			http.Error(w, "conflicts with a running refresh", http.StatusConflict)
			return
		}
		s.logger.Error("trigger airport test failed", "airport_id", airportID, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	s.logger.Info("airport test triggered", "airport_id", airportID, "job_id", jobID, "started", started)
	writeJSON(w, map[string]any{
		"ok":      true,
		"jobId":   jobID,
		"kind":    airporttest.JobKindName,
		"key":     key,
		"started": started,
	})
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
