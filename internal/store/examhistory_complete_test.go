package store

import (
	"testing"

	"github.com/taliove/proxyhub/internal/detection"
)

// stabilityCheckExamReport 构造"出网+稳定性"检查报告(带来源标记,缺多地域测速/解锁段)。
func stabilityCheckExamReport(score int) detection.ExamReport {
	r := sampleExamReport(score)
	r.Source = detection.ExamSourceStabilityCheck
	r.Egress = &detection.EgressMetrics{
		IPv4: &detection.EgressIPv4{IP: "203.0.113.7", CountryCode: "US", Hosting: true},
	}
	return r
}

// LatestCompleteExamHistory:最新一条是 stability_check 时,回退到最近的完整体检口径记录。
func TestLatestCompleteExamHistory_SkipsStabilityCheck(t *testing.T) {
	st := openExamHistoryStore(t)
	nodeKey := "example.com:443"

	if err := st.SaveExamHistory(nodeKey, sampleExamReport(90)); err != nil {
		t.Fatalf("save full: %v", err)
	}
	if err := st.SaveExamHistory(nodeKey, stabilityCheckExamReport(50)); err != nil {
		t.Fatalf("save stability check: %v", err)
	}

	entry, err := st.LatestCompleteExamHistory(nodeKey)
	if err != nil {
		t.Fatalf("LatestCompleteExamHistory: %v", err)
	}
	if entry == nil {
		t.Fatal("entry = nil, want the earlier full exam report")
	}
	if entry.Report.Source == detection.ExamSourceStabilityCheck {
		t.Error("entry is a stability_check report, want complete-exam caliber")
	}
	if entry.Report.Stability == nil || entry.Report.Stability.Score != 90 {
		t.Errorf("entry score = %+v, want the full exam with score 90", entry.Report.Stability)
	}
}

// LatestCompleteExamHistory:节点只有 stability_check 记录时返回 (nil, nil)——
// "最近体检"语义下该节点视为未体检。
func TestLatestCompleteExamHistory_OnlyStabilityCheckReturnsNil(t *testing.T) {
	st := openExamHistoryStore(t)
	nodeKey := "example.com:443"

	if err := st.SaveExamHistory(nodeKey, stabilityCheckExamReport(50)); err != nil {
		t.Fatalf("save: %v", err)
	}

	entry, err := st.LatestCompleteExamHistory(nodeKey)
	if err != nil {
		t.Fatalf("LatestCompleteExamHistory: %v", err)
	}
	if entry != nil {
		t.Errorf("entry = %+v, want nil (stability check is not a complete exam)", entry)
	}
}

// LatestCompleteExamHistory:最新一条就是完整体检时照常返回(主路径不回归)。
func TestLatestCompleteExamHistory_LatestIsComplete(t *testing.T) {
	st := openExamHistoryStore(t)
	nodeKey := "example.com:443"

	if err := st.SaveExamHistory(nodeKey, stabilityCheckExamReport(50)); err != nil {
		t.Fatalf("save stability check: %v", err)
	}
	if err := st.SaveExamHistory(nodeKey, sampleExamReport(90)); err != nil {
		t.Fatalf("save full: %v", err)
	}

	entry, err := st.LatestCompleteExamHistory(nodeKey)
	if err != nil {
		t.Fatalf("LatestCompleteExamHistory: %v", err)
	}
	if entry == nil || entry.Report.Stability == nil || entry.Report.Stability.Score != 90 {
		t.Errorf("entry = %+v, want latest full exam (score 90)", entry)
	}
}

// LatestCompleteExamScores:稳定性分取自最近完整体检口径,不被 stability_check 的新鲜分抢占。
func TestLatestCompleteExamScores_NotPreemptedByStabilityCheck(t *testing.T) {
	st := openExamHistoryStore(t)
	keyA := "a.example.com:443"
	keyB := "b.example.com:443"

	// A:先完整体检 90 分,后 stability_check 50 分 -> 分数仍取 90。
	if err := st.SaveExamHistory(keyA, sampleExamReport(90)); err != nil {
		t.Fatalf("save A full: %v", err)
	}
	if err := st.SaveExamHistory(keyA, stabilityCheckExamReport(50)); err != nil {
		t.Fatalf("save A check: %v", err)
	}
	// B:只有 stability_check -> 视为无分(不出现在结果中)。
	if err := st.SaveExamHistory(keyB, stabilityCheckExamReport(50)); err != nil {
		t.Fatalf("save B check: %v", err)
	}

	scores, err := st.LatestCompleteExamScores([]string{keyA, keyB})
	if err != nil {
		t.Fatalf("LatestCompleteExamScores: %v", err)
	}
	if got, ok := scores[keyA]; !ok || got != 90 {
		t.Errorf("scores[%q] = %d, %v; want 90, true", keyA, got, ok)
	}
	if _, ok := scores[keyB]; ok {
		t.Errorf("scores[%q] present, want absent (only stability_check history)", keyB)
	}
}

// LatestCompleteExamReports:报告本体取自最近完整体检口径(总分聚合消费方)。
func TestLatestCompleteExamReports_NotPreemptedByStabilityCheck(t *testing.T) {
	st := openExamHistoryStore(t)
	keyA := "a.example.com:443"

	if err := st.SaveExamHistory(keyA, sampleExamReport(90)); err != nil {
		t.Fatalf("save full: %v", err)
	}
	if err := st.SaveExamHistory(keyA, stabilityCheckExamReport(50)); err != nil {
		t.Fatalf("save check: %v", err)
	}

	reports, err := st.LatestCompleteExamReports([]string{keyA})
	if err != nil {
		t.Fatalf("LatestCompleteExamReports: %v", err)
	}
	entry, ok := reports[keyA]
	if !ok {
		t.Fatalf("reports[%q] absent, want the full exam report", keyA)
	}
	if entry.Report.Source == detection.ExamSourceStabilityCheck {
		t.Error("report is stability_check sourced, want complete-exam caliber")
	}
	if entry.Report.Stability == nil || entry.Report.Stability.Score != 90 {
		t.Errorf("report score = %+v, want 90", entry.Report.Stability)
	}
}

// ListCompleteExamHistory:时间线只含完整体检口径记录,stability_check 条目被过滤。
func TestListCompleteExamHistory_FiltersStabilityCheck(t *testing.T) {
	st := openExamHistoryStore(t)
	nodeKey := "example.com:443"

	if err := st.SaveExamHistory(nodeKey, sampleExamReport(80)); err != nil {
		t.Fatalf("save full 1: %v", err)
	}
	if err := st.SaveExamHistory(nodeKey, stabilityCheckExamReport(50)); err != nil {
		t.Fatalf("save check: %v", err)
	}
	if err := st.SaveExamHistory(nodeKey, sampleExamReport(95)); err != nil {
		t.Fatalf("save full 2: %v", err)
	}

	list, err := st.ListCompleteExamHistory(nodeKey)
	if err != nil {
		t.Fatalf("ListCompleteExamHistory: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("list len = %d, want 2 (stability_check filtered out)", len(list))
	}
	for _, e := range list {
		if e.Report.Source == detection.ExamSourceStabilityCheck {
			t.Error("timeline contains stability_check entry, want filtered")
		}
	}
	// 时间倒序:最新完整体检(95)在前。
	if list[0].Report.Stability == nil || list[0].Report.Stability.Score != 95 {
		t.Errorf("list[0] score = %+v, want 95 (time desc)", list[0].Report.Stability)
	}
}

// 对照组:原有不过滤的查询仍把 stability_check 当最新一条(标签派生等需要新鲜数据的消费方)。
func TestLatestExamHistory_StillSeesStabilityCheck(t *testing.T) {
	st := openExamHistoryStore(t)
	nodeKey := "example.com:443"

	if err := st.SaveExamHistory(nodeKey, sampleExamReport(90)); err != nil {
		t.Fatalf("save full: %v", err)
	}
	if err := st.SaveExamHistory(nodeKey, stabilityCheckExamReport(50)); err != nil {
		t.Fatalf("save check: %v", err)
	}

	entry, err := st.LatestExamHistory(nodeKey)
	if err != nil {
		t.Fatalf("LatestExamHistory: %v", err)
	}
	if entry == nil || entry.Report.Source != detection.ExamSourceStabilityCheck {
		t.Errorf("LatestExamHistory = %+v, want the fresh stability_check report", entry)
	}
}
