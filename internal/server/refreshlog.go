package server

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/taliove/proxyhub/internal/store"
)

// handleListRefreshRuns 按时间倒序返回刷新历史（store 只保留最近 MaxRefreshRuns 条）
func (s *Server) handleListRefreshRuns(w http.ResponseWriter, r *http.Request) {
	runs, err := s.st.ListRefreshRuns(store.MaxRefreshRuns)
	if err != nil {
		s.logger.Error("list refresh runs failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if runs == nil {
		runs = []*store.RefreshRun{}
	}
	writeJSON(w, runs)
}

// handleGetRefreshRun 返回单次刷新的汇总和全部事件（前端轮询进度用）
func (s *Server) handleGetRefreshRun(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	run, err := s.st.GetRefreshRun(id)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.logger.Error("get refresh run failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	events, err := s.st.ListRefreshEvents(id)
	if err != nil {
		s.logger.Error("list refresh events failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if events == nil {
		events = []*store.RefreshEvent{}
	}

	writeJSON(w, map[string]any{
		"run":    run,
		"events": events,
	})
}
