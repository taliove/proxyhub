package store

import (
	"reflect"
	"testing"

	"github.com/taliove/proxyhub/internal/detection"
)

// TestLatestExamScores_BatchLatestPerNode 批量取每节点最近体检稳定性分:
// 只取每节点最新一条,且只对含稳定性段的报告返回分数。
func TestLatestExamScores_BatchLatestPerNode(t *testing.T) {
	st := openExamHistoryStore(t)
	nodeA := "a.example.com:443"
	nodeB := "b.example.com:443"

	// nodeA 写两条,最新分数 88 应覆盖旧的 40。
	if err := st.SaveExamHistory(nodeA, sampleExamReport(40)); err != nil {
		t.Fatalf("saveA 1: %v", err)
	}
	if err := st.SaveExamHistory(nodeA, sampleExamReport(88)); err != nil {
		t.Fatalf("saveA 2: %v", err)
	}
	// nodeB 单条,分数 0(合法的"差"档,不能被当作缺省丢弃)。
	if err := st.SaveExamHistory(nodeB, sampleExamReport(0)); err != nil {
		t.Fatalf("saveB: %v", err)
	}

	got, err := st.LatestExamScores([]string{nodeA, nodeB})
	if err != nil {
		t.Fatalf("LatestExamScores: %v", err)
	}
	want := map[string]int{nodeA: 88, nodeB: 0}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("scores = %v, want %v", got, want)
	}
}

// TestLatestExamScores_EmptyKeys 空 key 集返回空 map(不查库、不报错)。
func TestLatestExamScores_EmptyKeys(t *testing.T) {
	st := openExamHistoryStore(t)
	got, err := st.LatestExamScores(nil)
	if err != nil {
		t.Fatalf("LatestExamScores(nil): %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("scores = %v, want empty", got)
	}
}

// TestLatestExamScores_NoHistoryOmitted 无体检记录的节点不出现在结果中。
func TestLatestExamScores_NoHistoryOmitted(t *testing.T) {
	st := openExamHistoryStore(t)
	got, err := st.LatestExamScores([]string{"no-such-node"})
	if err != nil {
		t.Fatalf("LatestExamScores: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("scores = %v, want empty (no history)", got)
	}
}

// TestLatestExamScores_ReportWithoutStabilityOmitted 报告无稳定性段时不返回分数。
func TestLatestExamScores_ReportWithoutStabilityOmitted(t *testing.T) {
	st := openExamHistoryStore(t)
	nodeKey := "c.example.com:443"
	// 只含出网段、无稳定性段的报告。
	report := detection.ExamReport{
		Egress: &detection.EgressMetrics{
			IPv4: &detection.EgressIPv4{IP: "203.0.113.1", CountryCode: "SG"},
		},
	}
	if err := st.SaveExamHistory(nodeKey, report); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := st.LatestExamScores([]string{nodeKey})
	if err != nil {
		t.Fatalf("LatestExamScores: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("scores = %v, want empty (no stability section)", got)
	}
}
