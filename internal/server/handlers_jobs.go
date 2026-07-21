package server

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/taliove/proxyhub/internal/jobs"
)

// JobInfo 通用任务信息(供任务中心展示)。
type JobInfo struct {
	ID        int64           `json:"id"`
	Kind      string          `json:"kind"`
	Key       string          `json:"key"`
	Status    jobs.Status     `json:"status"`
	Cursor    string          `json:"cursor,omitempty"` // 游标进度
	Params    json.RawMessage `json:"params,omitempty"` // 启动参数(前端据此生成可读范围标识)
	CreatedAt string          `json:"created_at"`
	UpdatedAt string          `json:"updated_at"`
}

// handleListJobs GET /api/jobs 列出所有任务(从 jobs 表读取)。
func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	if s.st == nil {
		http.Error(w, "storage not initialized", http.StatusServiceUnavailable)
		return
	}

	// 查询参数:kind/status 筛选(可选)
	kindFilter := r.URL.Query().Get("kind")
	statusFilter := r.URL.Query().Get("status")

	// 从 jobs 表加载所有记录(当前无分页,任务表行数可控)
	records, err := s.st.Jobs().LoadAll()
	if err != nil {
		s.logger.Error("list jobs failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// 筛选与转换
	var jobs []JobInfo
	for _, rec := range records {
		if kindFilter != "" && rec.Kind != kindFilter {
			continue
		}
		if statusFilter != "" && string(rec.Status) != statusFilter {
			continue
		}
		jobs = append(jobs, JobInfo{
			ID:        rec.ID,
			Kind:      rec.Kind,
			Key:       rec.Key,
			Status:    rec.Status,
			Cursor:    rec.Cursor,
			Params:    rec.Params,
			CreatedAt: rec.CreatedAt.Format("2006-01-02 15:04:05"),
			UpdatedAt: rec.UpdatedAt.Format("2006-01-02 15:04:05"),
		})
	}

	if jobs == nil {
		jobs = []JobInfo{} // 返回空数组而非 null
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(jobs)
}

// handleCancelJob POST /api/jobs/{kind}/{key}/cancel 取消指定任务。
func (s *Server) handleCancelJob(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	key := r.PathValue("key")

	if kind == "" || key == "" {
		http.Error(w, "missing kind or key", http.StatusBadRequest)
		return
	}

	// 根据 kind 分发到对应管理器
	var cancelled bool
	switch kind {
	case "batch_detection":
		if s.detectionJobs != nil {
			if err := s.detectionJobs.CancelDetection(); err == nil {
				cancelled = true
			}
		}
	case "batch_exam":
		if s.batchExamJobs != nil {
			cancelled = s.batchExamJobs.Cancel(key)
		}
	case "exam":
		if s.examJobs != nil {
			cancelled = s.examJobs.Cancel(key)
		}
	default:
		http.Error(w, "unknown kind", http.StatusBadRequest)
		return
	}

	if !cancelled {
		writeJSONStatus(w, http.StatusConflict, map[string]string{"error": "no active job"})
		return
	}

	writeJSON(w, map[string]string{"status": "cancelled"})
}

// handleGetJobDetail GET /api/jobs/{id} 查询单个任务详情(从 jobs 表读取)。
func (s *Server) handleGetJobDetail(w http.ResponseWriter, r *http.Request) {
	if s.st == nil {
		http.Error(w, "storage not initialized", http.StatusServiceUnavailable)
		return
	}

	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	rec, err := s.st.Jobs().Get(id)
	if err != nil {
		s.logger.Error("get job failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if rec == nil {
		http.NotFound(w, r)
		return
	}

	jobInfo := JobInfo{
		ID:        rec.ID,
		Kind:      rec.Kind,
		Key:       rec.Key,
		Status:    rec.Status,
		Cursor:    rec.Cursor,
		Params:    rec.Params,
		CreatedAt: rec.CreatedAt.Format("2006-01-02 15:04:05"),
		UpdatedAt: rec.UpdatedAt.Format("2006-01-02 15:04:05"),
	}

	writeJSON(w, jobInfo)
}
