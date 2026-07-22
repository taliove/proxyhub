package server

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/taliove/proxyhub/internal/jobs"
	"github.com/taliove/proxyhub/internal/store"
)

// 任务结果端点(GET /api/jobs/{id}/result)的空结果原因(ticket 0022)。
const (
	// jobResultReasonNoReport 该任务未产生报告(如被中断,或窗口内无记录)。
	jobResultReasonNoReport = "no_report"
	// jobResultReasonKindHasNoResult 该 kind 本无"报告"产物(batch_detection/retag_all/未知 kind)。
	jobResultReasonKindHasNoResult = "kind_has_no_result"
)

// ExamJobReport 任务结果里某节点的体检报告条目。
// Fallback=true 表示该报告由任务时间窗回退派生(任务结果关联前的旧数据),
// 前端据此标注"非本次任务产出"。
type ExamJobReport struct {
	NodeKey  string                  `json:"node_key"`
	Fallback bool                    `json:"fallback"`
	Entry    *store.ExamHistoryEntry `json:"entry,omitempty"`
}

// JobResultResponse GET /api/jobs/{id}/result 的响应:按 kind 分发,
// refresh 填 RefreshRun;airport_test 填 AirportTestRun;exam/batch_exam 填 Reports;
// 无产物 kind 只带 Reason。
type JobResultResponse struct {
	Kind           string                `json:"kind"`
	JobID          int64                 `json:"job_id"`
	RefreshRun     *store.RefreshRun     `json:"refresh_run,omitempty"`
	AirportTestRun *store.AirportTestRun `json:"airport_test_run,omitempty"`
	Reports        []ExamJobReport       `json:"reports"`
	Reason         string                `json:"reason,omitempty"`
}

// batchExamResultParams 批量体检任务参数(只取结果聚合所需的 node_keys)。
type batchExamResultParams struct {
	NodeKeys []string `json:"node_keys"`
}

// handleGetJobResult GET /api/jobs/{id}/result 按 kind 返回"这次任务跑出了什么"。
// 未知 kind/无结果 kind 返回空结果语义(200 + reason),不报错。
func (s *Server) handleGetJobResult(w http.ResponseWriter, r *http.Request) {
	if s.st == nil {
		http.Error(w, "storage not initialized", http.StatusServiceUnavailable)
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	rec, err := s.st.Jobs().Get(id)
	if err != nil {
		s.logger.Error("get job for result failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if rec == nil {
		http.NotFound(w, r)
		return
	}

	res := JobResultResponse{
		Kind:    rec.Kind,
		JobID:   rec.ID,
		Reports: []ExamJobReport{},
	}

	switch rec.Kind {
	case "refresh":
		// 现有 refresh_runs 反查(job_id 列,ADR 0026),行为不变。
		run, err := s.st.GetRefreshRunByJobID(rec.ID)
		if err != nil {
			s.logger.Error("get refresh run by job failed", "job_id", rec.ID, "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		res.RefreshRun = run
		if run == nil {
			res.Reason = jobResultReasonNoReport
		}
	case "airport_test":
		// airport_test_runs 按 job_id 反查(issue 0025 迁入,对齐 refresh_runs 机制)。
		run, err := s.st.GetAirportTestRunByJobID(rec.ID)
		if err != nil {
			s.logger.Error("get airport test run by job failed", "job_id", rec.ID, "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		res.AirportTestRun = run
		if run == nil {
			res.Reason = jobResultReasonNoReport
		}
	case "exam":
		report, err := s.examReportForJob(rec, rec.Key)
		if err != nil {
			s.logger.Error("get exam report for job failed", "job_id", rec.ID, "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if report != nil {
			res.Reports = append(res.Reports, *report)
		} else {
			res.Reason = jobResultReasonNoReport
		}
	case "batch_exam":
		var p batchExamResultParams
		if len(rec.Params) > 0 {
			if err := json.Unmarshal(rec.Params, &p); err != nil {
				// 参数损坏按空结果处理(不报错):该任务无可聚合范围。
				s.logger.Warn("batch_exam job params unreadable", "job_id", rec.ID, "error", err)
			}
		}
		for _, nodeKey := range p.NodeKeys {
			report, err := s.examReportForJob(rec, nodeKey)
			if err != nil {
				s.logger.Error("get batch exam report failed", "job_id", rec.ID, "node_key", nodeKey, "error", err)
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			if report != nil {
				res.Reports = append(res.Reports, *report)
			}
		}
		if len(res.Reports) == 0 {
			res.Reason = jobResultReasonNoReport
		}
	default:
		// batch_detection/retag_all/未知 kind:无"报告"产物,空结果语义。
		res.Reason = jobResultReasonKindHasNoResult
	}

	writeJSON(w, res)
}

// examReportForJob 取某节点在某任务上下文里的体检报告:job_id 精确匹配优先
// (任务结果关联后的新数据);无则回退任务时间窗 [created_at, updated_at] 内最新
// 一条旧数据并标记 Fallback;窗口内亦无记录返回 nil(如任务被中断)。
func (s *Server) examReportForJob(rec *jobs.Record, nodeKey string) (*ExamJobReport, error) {
	entry, err := s.st.ExamHistoryByJob(nodeKey, rec.ID)
	if err != nil {
		return nil, err
	}
	if entry != nil {
		return &ExamJobReport{NodeKey: nodeKey, Entry: entry}, nil
	}

	legacy, err := s.st.LatestExamHistoryInWindow(nodeKey, rec.CreatedAt, rec.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if legacy == nil {
		return nil, nil
	}
	return &ExamJobReport{NodeKey: nodeKey, Fallback: true, Entry: legacy}, nil
}
