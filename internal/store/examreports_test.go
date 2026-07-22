package store

import (
	"testing"

	"github.com/taliove/proxyhub/internal/detection"
)

// TestLatestExamReports_BatchLatestPerNode 批量取每节点最近一条体检记录:
// 同一 node_key 多条历史只取最新一条(id 最大)。
func TestLatestExamReports_BatchLatestPerNode(t *testing.T) {
	st := openExamHistoryStore(t)
	nodeA := "a.example.com:443"
	nodeB := "b.example.com:443"

	// nodeA 写两条,最新分数 88 的那条应被取回。
	if err := st.SaveExamHistory(nodeA, sampleExamReport(40)); err != nil {
		t.Fatalf("saveA 1: %v", err)
	}
	if err := st.SaveExamHistory(nodeA, sampleExamReport(88)); err != nil {
		t.Fatalf("saveA 2: %v", err)
	}
	if err := st.SaveExamHistory(nodeB, sampleExamReport(0)); err != nil {
		t.Fatalf("saveB: %v", err)
	}

	got, err := st.LatestExamReports([]string{nodeA, nodeB})
	if err != nil {
		t.Fatalf("LatestExamReports: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[nodeA].Report.Stability == nil || got[nodeA].Report.Stability.Score != 88 {
		t.Fatalf("nodeA report = %+v, want latest score 88", got[nodeA].Report)
	}
	if got[nodeB].Report.Stability == nil || got[nodeB].Report.Stability.Score != 0 {
		t.Fatalf("nodeB report = %+v, want score 0", got[nodeB].Report)
	}
	if got[nodeA].NodeKey != nodeA || got[nodeA].ID == 0 {
		t.Fatalf("nodeA entry not fully populated: %+v", got[nodeA])
	}
}

// TestLatestExamReports_EmptyKeys 空 key 集返回空 map(不查库、不报错)。
func TestLatestExamReports_EmptyKeys(t *testing.T) {
	st := openExamHistoryStore(t)
	got, err := st.LatestExamReports(nil)
	if err != nil {
		t.Fatalf("LatestExamReports(nil): %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("reports = %v, want empty", got)
	}
}

// TestLatestExamReports_NoHistoryOmitted 无体检记录的节点不出现在结果中。
func TestLatestExamReports_NoHistoryOmitted(t *testing.T) {
	st := openExamHistoryStore(t)
	examined := "examined.example.com:443"
	if err := st.SaveExamHistory(examined, sampleExamReport(60)); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := st.LatestExamReports([]string{examined, "never-examined.example.com:443"})
	if err != nil {
		t.Fatalf("LatestExamReports: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1 (never-examined omitted)", len(got))
	}
	if _, ok := got["never-examined.example.com:443"]; ok {
		t.Fatalf("never-examined node must be omitted, got %+v", got)
	}
}

// TestLatestExamReports_ReportWithoutStabilityKept 报告无稳定性段也整条返回
// (与 LatestExamScores 不同:本方法返回 report 本体,是否可用由调用方判定)。
func TestLatestExamReports_ReportWithoutStabilityKept(t *testing.T) {
	st := openExamHistoryStore(t)
	nodeKey := "c.example.com:443"
	report := detection.ExamReport{
		Egress: &detection.EgressMetrics{
			IPv4: &detection.EgressIPv4{IP: "203.0.113.1", CountryCode: "SG"},
		},
	}
	if err := st.SaveExamHistory(nodeKey, report); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := st.LatestExamReports([]string{nodeKey})
	if err != nil {
		t.Fatalf("LatestExamReports: %v", err)
	}
	entry, ok := got[nodeKey]
	if !ok {
		t.Fatalf("egress-only report must be returned, got %v", got)
	}
	if entry.Report.Egress == nil || entry.Report.Egress.IPv4 == nil {
		t.Fatalf("egress section lost: %+v", entry.Report)
	}
}
