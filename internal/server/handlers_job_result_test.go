package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/taliove/proxyhub/internal/detection"
	"github.com/taliove/proxyhub/internal/jobs"
)

// jobResultFixture 任务结果端点测试响应(与 handler 的 JSON 形状对齐)。
type jobResultFixture struct {
	Kind       string `json:"kind"`
	JobID      int64  `json:"job_id"`
	RefreshRun *struct {
		ID     int64  `json:"id"`
		Status string `json:"status"`
	} `json:"refresh_run"`
	Reports []struct {
		NodeKey  string `json:"node_key"`
		Fallback bool   `json:"fallback"`
		Entry    *struct {
			ID    int64 `json:"id"`
			JobID int64 `json:"job_id"`
		} `json:"entry"`
	} `json:"reports"`
	Reason string `json:"reason"`
}

// getJobResult 调用 GET /api/jobs/{id}/result 并解码响应。
func getJobResult(t *testing.T, srv *Server, id string) (int, jobResultFixture) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/jobs/"+id+"/result", nil)
	req.SetPathValue("id", id)
	w := httptest.NewRecorder()
	srv.handleGetJobResult(w, req)
	var res jobResultFixture
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatalf("decode result: %v (body %s)", err, w.Body.String())
		}
	}
	return w.Code, res
}

// seedExamJob 插入一条 exam 任务记录并按需落终态,返回 job id。
func seedExamJob(t *testing.T, srv *Server, kind, key string, params json.RawMessage, finish bool) int64 {
	t.Helper()
	id, err := srv.st.Jobs().Insert(kind, key, params)
	if err != nil {
		t.Fatalf("Insert job: %v", err)
	}
	if finish {
		if err := srv.st.Jobs().Finish(id, jobs.StatusDone); err != nil {
			t.Fatalf("Finish job: %v", err)
		}
	}
	return id
}

// TestHandleGetJobResult_ExamExact 精确关联:exam_history 带本任务 job_id,
// 回应该节点报告且不带回退标记。
func TestHandleGetJobResult_ExamExact(t *testing.T) {
	srv, st := newTestServer(t, nil)
	nodeKey := "example.com:443"

	jobID := seedExamJob(t, srv, "exam", nodeKey, nil, false)
	if err := st.SaveExamHistoryWithJob(nodeKey, sampleExamReportForJob(88), jobID); err != nil {
		t.Fatalf("save with job: %v", err)
	}
	if err := srv.st.Jobs().Finish(jobID, jobs.StatusDone); err != nil {
		t.Fatalf("finish: %v", err)
	}

	code, res := getJobResult(t, srv, fmt.Sprint(jobID))
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if res.Kind != "exam" || res.JobID != jobID {
		t.Fatalf("kind/job_id = %q/%d, want exam/%d", res.Kind, res.JobID, jobID)
	}
	if len(res.Reports) != 1 {
		t.Fatalf("reports len = %d, want 1", len(res.Reports))
	}
	r := res.Reports[0]
	if r.NodeKey != nodeKey || r.Fallback {
		t.Fatalf("report = (%q, fallback=%v), want (%q, false)", r.NodeKey, r.Fallback, nodeKey)
	}
	if r.Entry == nil || r.Entry.JobID != jobID {
		t.Fatalf("entry = %+v, want job_id %d", r.Entry, jobID)
	}
	if res.Reason != "" {
		t.Fatalf("reason = %q, want empty", res.Reason)
	}
}

// TestHandleGetJobResult_ExamFallback 老数据回退:无 job_id 记录时取任务时间窗
// [created_at, updated_at] 内最新一条,响应带 fallback: true。
func TestHandleGetJobResult_ExamFallback(t *testing.T) {
	srv, st := newTestServer(t, nil)
	nodeKey := "example.com:443"

	// 时序:任务开始 -> 旧数据落库(任务化前口径,job_id=0) -> 任务结束。
	jobID := seedExamJob(t, srv, "exam", nodeKey, nil, false)
	if err := st.SaveExamHistory(nodeKey, sampleExamReportForJob(66)); err != nil {
		t.Fatalf("save legacy: %v", err)
	}
	if err := srv.st.Jobs().Finish(jobID, jobs.StatusDone); err != nil {
		t.Fatalf("finish: %v", err)
	}

	code, res := getJobResult(t, srv, fmt.Sprint(jobID))
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if len(res.Reports) != 1 {
		t.Fatalf("reports len = %d, want 1 (window fallback)", len(res.Reports))
	}
	r := res.Reports[0]
	if r.NodeKey != nodeKey || !r.Fallback {
		t.Fatalf("report = (%q, fallback=%v), want (%q, true)", r.NodeKey, r.Fallback, nodeKey)
	}
}

// TestHandleGetJobResult_ExamNoReport 中断任务无产出:窗口内亦无记录,
// 返回空结果 + reason=no_report(不报错)。
func TestHandleGetJobResult_ExamNoReport(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	nodeKey := "example.com:443"

	jobID := seedExamJob(t, srv, "exam", nodeKey, nil, false)
	if err := srv.st.Jobs().Finish(jobID, jobs.StatusInterrupted); err != nil {
		t.Fatalf("finish: %v", err)
	}

	code, res := getJobResult(t, srv, fmt.Sprint(jobID))
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if len(res.Reports) != 0 {
		t.Fatalf("reports len = %d, want 0", len(res.Reports))
	}
	if res.Reason != "no_report" {
		t.Fatalf("reason = %q, want no_report", res.Reason)
	}
}

// TestHandleGetJobResult_ExamWindowMiss 窗口外旧数据不得被回退命中。
func TestHandleGetJobResult_ExamWindowMiss(t *testing.T) {
	srv, st := newTestServer(t, nil)
	nodeKey := "example.com:443"

	// 任务先开始并结束;旧数据落库于任务结束之后(窗口 [created_at, updated_at] 之外)。
	jobID := seedExamJob(t, srv, "exam", nodeKey, nil, true)
	// 与 Finish 拉开墙钟间隔,确保旧数据 created_at 严格晚于 updated_at。
	time.Sleep(5 * time.Millisecond)
	if err := st.SaveExamHistory(nodeKey, sampleExamReportForJob(55)); err != nil {
		t.Fatalf("save legacy: %v", err)
	}

	code, res := getJobResult(t, srv, fmt.Sprint(jobID))
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if len(res.Reports) != 0 || res.Reason != "no_report" {
		t.Fatalf("reports = %d reason = %q, want 0 + no_report (out-of-window must not match)",
			len(res.Reports), res.Reason)
	}
}

// TestHandleGetJobResult_BatchExamAggregate 批量体检:按 params.node_keys
// 聚合各节点报告;精确关联与时间窗回退可混存。
func TestHandleGetJobResult_BatchExamAggregate(t *testing.T) {
	srv, st := newTestServer(t, nil)
	nodeA := "a.example.com:443"
	nodeB := "b.example.com:443"

	params := json.RawMessage(fmt.Sprintf(`{"node_keys":[%q,%q],"scope":"selected"}`, nodeA, nodeB))
	jobID := seedExamJob(t, srv, "batch_exam", "batch_exam", params, false)
	// nodeA 精确关联(带 job_id);nodeB 回退(旧数据落窗口内)。
	if err := st.SaveExamHistoryWithJob(nodeA, sampleExamReportForJob(91), jobID); err != nil {
		t.Fatalf("save A: %v", err)
	}
	if err := st.SaveExamHistory(nodeB, sampleExamReportForJob(72)); err != nil {
		t.Fatalf("save B: %v", err)
	}
	if err := srv.st.Jobs().Finish(jobID, jobs.StatusDone); err != nil {
		t.Fatalf("finish: %v", err)
	}

	code, res := getJobResult(t, srv, fmt.Sprint(jobID))
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if len(res.Reports) != 2 {
		t.Fatalf("reports len = %d, want 2", len(res.Reports))
	}
	got := map[string]bool{}
	for _, r := range res.Reports {
		got[r.NodeKey] = r.Fallback
	}
	if fb, ok := got[nodeA]; !ok || fb {
		t.Fatalf("nodeA fallback = %v (present=%v), want false", fb, ok)
	}
	if fb, ok := got[nodeB]; !ok || !fb {
		t.Fatalf("nodeB fallback = %v (present=%v), want true", fb, ok)
	}
}

// TestHandleGetJobResult_Refresh 回归:refresh 走 refresh_runs 按 job_id 反查。
func TestHandleGetJobResult_Refresh(t *testing.T) {
	srv, st := newTestServer(t, nil)

	jobID := seedExamJob(t, srv, "refresh", "all", nil, true)
	run, err := st.CreateRefreshRun("manual", jobID)
	if err != nil {
		t.Fatalf("create refresh run: %v", err)
	}
	if err := st.FinishRefreshRun(run.ID, "success", 10, 8, 8, ""); err != nil {
		t.Fatalf("finish refresh run: %v", err)
	}

	code, res := getJobResult(t, srv, fmt.Sprint(jobID))
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if res.RefreshRun == nil || res.RefreshRun.ID != run.ID {
		t.Fatalf("refresh_run = %+v, want id %d", res.RefreshRun, run.ID)
	}
	if res.Reason != "" {
		t.Fatalf("reason = %q, want empty", res.Reason)
	}
}

// TestHandleGetJobResult_UnsupportedKind 无报告产物的 kind 返回空结果语义(不报错)。
func TestHandleGetJobResult_UnsupportedKind(t *testing.T) {
	srv, _ := newTestServer(t, nil)

	for _, kind := range []string{"batch_detection", "retag_all", "some_future_kind"} {
		jobID := seedExamJob(t, srv, kind, "k", nil, true)
		code, res := getJobResult(t, srv, fmt.Sprint(jobID))
		if code != http.StatusOK {
			t.Fatalf("kind %s: status = %d, want 200", kind, code)
		}
		if len(res.Reports) != 0 || res.RefreshRun != nil {
			t.Fatalf("kind %s: result not empty: %+v", kind, res)
		}
		if res.Reason != "kind_has_no_result" {
			t.Fatalf("kind %s: reason = %q, want kind_has_no_result", kind, res.Reason)
		}
	}
}

// TestHandleGetJobResult_NotFound 任务不存在 404;非法 id 400。
func TestHandleGetJobResult_NotFound(t *testing.T) {
	srv, _ := newTestServer(t, nil)

	if code, _ := getJobResult(t, srv, "99999"); code != http.StatusNotFound {
		t.Fatalf("missing job status = %d, want 404", code)
	}
	if code, _ := getJobResult(t, srv, "abc"); code != http.StatusBadRequest {
		t.Fatalf("invalid id status = %d, want 400", code)
	}
}

// TestOnExamComplete_PersistsJobID 完成链路落 job_id(写入侧反查,ADR 0026 样板):
// 回调触发时任务行仍 running,同 kind+key 单实例保证反查唯一。
func TestOnExamComplete_PersistsJobID(t *testing.T) {
	srv, st := newTestServer(t, nil)
	nodeKey := "example.com:443"

	jobID := seedExamJob(t, srv, "exam", nodeKey, nil, false)

	// 与 server.go 布线同款表达式:反查 running 行 id 再落历史。
	srv.onExamComplete(nodeKey, sampleExamReportForJob(77), srv.findRunningJobID("exam", nodeKey))

	entry, err := st.ExamHistoryByJob(nodeKey, jobID)
	if err != nil {
		t.Fatalf("by job: %v", err)
	}
	if entry == nil {
		t.Fatalf("no exam history linked to job %d", jobID)
	}
	if entry.Report.Stability == nil || entry.Report.Stability.Score != 77 {
		t.Fatalf("entry score = %+v, want 77", entry.Report.Stability)
	}
}

// sampleExamReportForJob 构造稳定性段体检报告(fixture:example.com + 无凭证)。
func sampleExamReportForJob(score int) detection.ExamReport {
	return detection.ExamReport{
		Stability: &detection.StabilityMetrics{
			Total:     10,
			Succeeded: 10,
			MeanMs:    42,
			Score:     score,
		},
	}
}
