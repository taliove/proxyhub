package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/taliove/proxyhub/internal/detection"
	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

// seedFullExam 落一条完整体检历史(无来源标记 = 完整体检口径)。
func seedFullExam(t *testing.T, st *store.Store, nodeKey string, score int) {
	t.Helper()
	report := detection.ExamReport{
		Stability:   &detection.StabilityMetrics{Total: 30, Succeeded: 30, Score: score},
		RegionSpeed: &detection.RegionSpeedMetrics{Regions: []detection.RegionResult{{Code: "baseline", DownMbps: 80}}},
		Egress:      &detection.EgressMetrics{IPv4: &detection.EgressIPv4{IP: "203.0.113.9", CountryCode: "US", Hosting: true}},
	}
	if err := st.SaveExamHistory(nodeKey, report); err != nil {
		t.Fatalf("seed full exam: %v", err)
	}
}

// seedStabilityCheck 落一条"出网+稳定性"检查历史(带 source=stability_check 来源标记)。
func seedStabilityCheck(t *testing.T, st *store.Store, nodeKey string, score int) {
	t.Helper()
	report := detection.ExamReport{
		Source:    detection.ExamSourceStabilityCheck,
		Stability: &detection.StabilityMetrics{Total: 30, Succeeded: 20, Score: score},
		Egress:    &detection.EgressMetrics{IPv4: &detection.EgressIPv4{IP: "198.51.100.9", CountryCode: "JP"}},
	}
	if err := st.SaveExamHistory(nodeKey, report); err != nil {
		t.Fatalf("seed stability check: %v", err)
	}
}

// "最近体检"接口:最新一条是 stability_check 时仍返回之前的完整体检(语义保护)。
func TestHandleGetExamLatest_NotPreemptedByStabilityCheck(t *testing.T) {
	node := examNode()
	srv, st := newTestServer(t, []*subscription.Node{node})
	seedFullExam(t, st, node.NodeKey(), 90)
	seedStabilityCheck(t, st, node.NodeKey(), 40)

	req := httptest.NewRequest(http.MethodGet, "/api/nodes/exam/latest?node_key="+node.NodeKey(), nil)
	w := httptest.NewRecorder()
	srv.handleGetExamLatest(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var entry store.ExamHistoryEntry
	if err := json.Unmarshal(w.Body.Bytes(), &entry); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if entry.Report.Source == detection.ExamSourceStabilityCheck {
		t.Error("latest exam is stability_check sourced, want complete-exam caliber")
	}
	if entry.Report.Stability == nil || entry.Report.Stability.Score != 90 {
		t.Errorf("latest score = %+v, want the full exam with score 90", entry.Report.Stability)
	}
}

// "最近体检"接口:节点只有 stability_check 历史时返回 null(视为未体检)。
func TestHandleGetExamLatest_OnlyStabilityCheckReturnsNull(t *testing.T) {
	node := examNode()
	srv, st := newTestServer(t, []*subscription.Node{node})
	seedStabilityCheck(t, st, node.NodeKey(), 40)

	req := httptest.NewRequest(http.MethodGet, "/api/nodes/exam/latest?node_key="+node.NodeKey(), nil)
	w := httptest.NewRecorder()
	srv.handleGetExamLatest(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if got := w.Body.String(); got != "null\n" && got != "null" {
		t.Errorf("body = %q, want null (stability check is not a complete exam)", got)
	}
}

// 体检历史时间线接口:stability_check 条目被过滤,只含完整体检口径记录。
func TestHandleGetExamHistory_FiltersStabilityCheck(t *testing.T) {
	node := examNode()
	srv, st := newTestServer(t, []*subscription.Node{node})
	seedFullExam(t, st, node.NodeKey(), 90)
	seedStabilityCheck(t, st, node.NodeKey(), 40)

	req := httptest.NewRequest(http.MethodGet, "/api/nodes/exam/history?node_key="+node.NodeKey(), nil)
	w := httptest.NewRecorder()
	srv.handleGetExamHistory(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var list []store.ExamHistoryEntry
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("history len = %d, want 1 (stability_check filtered out)", len(list))
	}
	if list[0].Report.Source == detection.ExamSourceStabilityCheck {
		t.Error("timeline contains stability_check entry, want filtered")
	}
}

// 节点列表 stability_score:不被 stability_check 的新鲜分抢占。
func TestHandleListNodes_StabilityScoreNotPreempted(t *testing.T) {
	node := examNode()
	srv, st := newTestServer(t, []*subscription.Node{node})
	seedFullExam(t, st, node.NodeKey(), 90)
	seedStabilityCheck(t, st, node.NodeKey(), 40)

	req := httptest.NewRequest(http.MethodGet, "/api/nodes", nil)
	w := httptest.NewRecorder()
	srv.handleListNodes(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp struct {
		Nodes []struct {
			NodeKey        string `json:"node_key"`
			StabilityScore *int   `json:"stability_score"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Nodes) != 1 {
		t.Fatalf("nodes len = %d, want 1", len(resp.Nodes))
	}
	score := resp.Nodes[0].StabilityScore
	if score == nil || *score != 90 {
		t.Errorf("stability_score = %v, want 90 (full exam), not preempted by stability check 40", score)
	}
}

// 优质节点聚合(总分):报告本体取完整体检口径,stability_check 缺段报告不进入聚合。
func TestHandleDashboardTopNodes_NotPreemptedByStabilityCheck(t *testing.T) {
	node := examNode()
	srv, st := newTestServer(t, []*subscription.Node{node})
	seedFullExam(t, st, node.NodeKey(), 90)
	seedStabilityCheck(t, st, node.NodeKey(), 40)

	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/top-nodes", nil)
	w := httptest.NewRecorder()
	srv.handleDashboardTopNodes(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var views []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &views); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(views) != 1 {
		t.Fatalf("top nodes len = %d, want 1 (full exam history present)", len(views))
	}
	report, ok := views[0]["report"].(map[string]any)
	if !ok {
		t.Fatalf("top node view missing report: %v", views[0])
	}
	if report["source"] == detection.ExamSourceStabilityCheck {
		t.Error("top node report is stability_check sourced, want complete-exam caliber")
	}
}
