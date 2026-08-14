package store

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/taliove/proxyhub/internal/detection"
)

func openExamHistoryStore(t *testing.T) *Store {
	t.Helper()
	st, err := OpenForTesting(filepath.Join(t.TempDir(), "examhist.db"))
	if err != nil {
		t.Fatalf("OpenForTesting() error = %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// sampleExamReport 构造一个稳定性段体检报告(fixture:不含任何凭证字段)。
func sampleExamReport(score int) detection.ExamReport {
	return detection.ExamReport{
		Stability: &detection.StabilityMetrics{
			Total:     10,
			Succeeded: 10,
			LossRate:  0,
			MeanMs:    42,
			MedianMs:  40,
			P95Ms:     55,
			P99Ms:     60,
			JitterMs:  3,
			Score:     score,
		},
	}
}

// TestExamHistoryMigrationApplied 证明 010 迁移真正接入迁移链路并执行了
// (exam_history 表在裸库 Open 后即存在)。
func TestExamHistoryMigrationApplied(t *testing.T) {
	st := openExamHistoryStore(t)
	var name string
	err := st.db.QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name='exam_history'`).Scan(&name)
	if err != nil {
		t.Fatalf("exam_history table not created by migration: %v", err)
	}
	if name != "exam_history" {
		t.Fatalf("table name = %q, want exam_history", name)
	}
}

func TestSaveExamHistory_AndLatest(t *testing.T) {
	st := openExamHistoryStore(t)
	nodeKey := "example.com:443"

	if err := st.SaveExamHistory(nodeKey, sampleExamReport(80)); err != nil {
		t.Fatalf("save 1: %v", err)
	}
	if err := st.SaveExamHistory(nodeKey, sampleExamReport(95)); err != nil {
		t.Fatalf("save 2: %v", err)
	}

	latest, err := st.LatestExamHistory(nodeKey)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if latest == nil {
		t.Fatalf("latest = nil, want the most recent entry")
	}
	if latest.Report.Stability == nil || latest.Report.Stability.Score != 95 {
		t.Fatalf("latest score = %+v, want 95 (most recent)", latest.Report.Stability)
	}
	if latest.NodeKey != nodeKey {
		t.Errorf("latest node_key = %q, want %q", latest.NodeKey, nodeKey)
	}
	if latest.CreatedAt.IsZero() {
		t.Errorf("latest created_at is zero")
	}
}

func TestLatestExamHistory_EmptyReturnsNilNotError(t *testing.T) {
	st := openExamHistoryStore(t)
	latest, err := st.LatestExamHistory("no-such-node")
	if err != nil {
		t.Fatalf("latest on empty: unexpected error %v", err)
	}
	if latest != nil {
		t.Fatalf("latest on empty = %+v, want nil", latest)
	}
}

func TestListExamHistory_EmptyReturnsEmptyNotError(t *testing.T) {
	st := openExamHistoryStore(t)
	list, err := st.ListExamHistory("no-such-node")
	if err != nil {
		t.Fatalf("list on empty: unexpected error %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("list on empty = %d entries, want 0", len(list))
	}
}

func TestListExamHistory_TimeReverseOrder(t *testing.T) {
	st := openExamHistoryStore(t)
	nodeKey := "example.com:8443"
	// 依次写入 score 1,2,3 —— 最新(3)应排最前。
	for _, s := range []int{1, 2, 3} {
		if err := st.SaveExamHistory(nodeKey, sampleExamReport(s)); err != nil {
			t.Fatalf("save %d: %v", s, err)
		}
	}
	list, err := st.ListExamHistory(nodeKey)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("list len = %d, want 3", len(list))
	}
	wantScores := []int{3, 2, 1}
	for i, w := range wantScores {
		if list[i].Report.Stability == nil || list[i].Report.Stability.Score != w {
			t.Errorf("list[%d] score = %+v, want %d (time-reverse)", i, list[i].Report.Stability, w)
		}
	}
}

func TestSaveExamHistory_TrimsToRetentionLimit(t *testing.T) {
	st := openExamHistoryStore(t)
	nodeKey := "example.com:443"
	// 写 51 条,score = 1..51。修剪后应剩 50 条,最旧(score=1)被删。
	for s := 1; s <= examHistoryRetention+1; s++ {
		if err := st.SaveExamHistory(nodeKey, sampleExamReport(s)); err != nil {
			t.Fatalf("save %d: %v", s, err)
		}
	}
	list, err := st.ListExamHistory(nodeKey)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != examHistoryRetention {
		t.Fatalf("after 51 writes, count = %d, want %d", len(list), examHistoryRetention)
	}
	// 最新是 51,最旧保留的是 2(score=1 被修剪)。
	if list[0].Report.Stability.Score != examHistoryRetention+1 {
		t.Errorf("newest score = %d, want %d", list[0].Report.Stability.Score, examHistoryRetention+1)
	}
	oldest := list[len(list)-1]
	if oldest.Report.Stability.Score != 2 {
		t.Errorf("oldest retained score = %d, want 2 (score=1 should be trimmed)", oldest.Report.Stability.Score)
	}
	// 显式断言 score=1 不再存在。
	for _, e := range list {
		if e.Report.Stability.Score == 1 {
			t.Fatalf("oldest entry (score=1) was not trimmed")
		}
	}
}

func TestSaveExamHistory_TrimIsPerNode(t *testing.T) {
	st := openExamHistoryStore(t)
	nodeA := "a.example.com:443"
	nodeB := "b.example.com:443"

	// nodeA 写满并溢出(触发修剪),nodeB 只写 3 条,不应受影响。
	for s := 1; s <= examHistoryRetention+5; s++ {
		if err := st.SaveExamHistory(nodeA, sampleExamReport(s)); err != nil {
			t.Fatalf("saveA %d: %v", s, err)
		}
	}
	for s := 1; s <= 3; s++ {
		if err := st.SaveExamHistory(nodeB, sampleExamReport(s)); err != nil {
			t.Fatalf("saveB %d: %v", s, err)
		}
	}

	listA, err := st.ListExamHistory(nodeA)
	if err != nil {
		t.Fatalf("listA: %v", err)
	}
	if len(listA) != examHistoryRetention {
		t.Errorf("nodeA count = %d, want %d", len(listA), examHistoryRetention)
	}
	listB, err := st.ListExamHistory(nodeB)
	if err != nil {
		t.Fatalf("listB: %v", err)
	}
	if len(listB) != 3 {
		t.Errorf("nodeB count = %d, want 3 (trim on A must not touch B)", len(listB))
	}
}

// TestSaveExamHistory_ReportJSONHasNoCredentials 断言落盘的报告 JSON
// 不含任何会话凭证类敏感模式(体检报告按设计只含稳定性指标,不含节点身份/密钥)。
func TestSaveExamHistory_ReportJSONHasNoCredentials(t *testing.T) {
	st := openExamHistoryStore(t)
	nodeKey := "example.com:443"
	if err := st.SaveExamHistory(nodeKey, sampleExamReport(90)); err != nil {
		t.Fatalf("save: %v", err)
	}

	var reportJSON string
	err := st.db.QueryRow(
		`SELECT report_json FROM exam_history WHERE node_key = ? ORDER BY id DESC LIMIT 1`, nodeKey).
		Scan(&reportJSON)
	if err != nil {
		t.Fatalf("read report_json: %v", err)
	}

	lower := strings.ToLower(reportJSON)
	for _, pattern := range []string{"password", "uuid", "\"server\"", "cipher", "\"token\"", "\"secret\"", "privatekey", "private_key"} {
		if strings.Contains(lower, strings.ToLower(pattern)) {
			t.Errorf("report_json contains sensitive pattern %q: %s", pattern, reportJSON)
		}
	}
}
