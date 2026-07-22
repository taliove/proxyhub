package store

import (
	"testing"
	"time"
)

// TestExamHistoryJobIDColumnMigration 证明 ticket 0022 迁移接入链路:
// 裸库 Open 后 exam_history 有 job_id 列,旧路径写入(SaveExamHistory)默认 0。
func TestExamHistoryJobIDColumnMigration(t *testing.T) {
	st := openExamHistoryStore(t)

	rows, err := st.db.Query(`SELECT name FROM pragma_table_info('exam_history')`)
	if err != nil {
		t.Fatalf("inspect exam_history columns: %v", err)
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan column: %v", err)
		}
		if name == "job_id" {
			found = true
		}
	}
	if !found {
		t.Fatal("exam_history.job_id column missing after migration")
	}

	nodeKey := "legacy.example.com:443"
	if err := st.SaveExamHistory(nodeKey, sampleExamReport(80)); err != nil {
		t.Fatalf("save legacy: %v", err)
	}
	var jobID int64
	if err := st.db.QueryRow(
		`SELECT job_id FROM exam_history WHERE node_key = ?`, nodeKey).Scan(&jobID); err != nil {
		t.Fatalf("read job_id: %v", err)
	}
	if jobID != 0 {
		t.Fatalf("legacy write job_id = %d, want 0 (unassociated)", jobID)
	}
}

// TestSaveExamHistoryWithJob_PersistsJobID 新体检落库带 job_id,读回可见。
func TestSaveExamHistoryWithJob_PersistsJobID(t *testing.T) {
	st := openExamHistoryStore(t)
	nodeKey := "example.com:443"

	if err := st.SaveExamHistoryWithJob(nodeKey, sampleExamReport(90), 42); err != nil {
		t.Fatalf("save with job: %v", err)
	}

	latest, err := st.LatestExamHistory(nodeKey)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if latest == nil {
		t.Fatal("latest = nil, want entry")
	}
	if latest.JobID != 42 {
		t.Fatalf("latest job_id = %d, want 42", latest.JobID)
	}
}

// TestExamHistoryByJob 精确关联:按 (node_key, job_id) 取该任务产出的最新一条,
// 不混入其他任务/旧数据;无匹配返回 (nil, nil)。
func TestExamHistoryByJob(t *testing.T) {
	st := openExamHistoryStore(t)
	nodeKey := "example.com:443"

	// 同一节点:旧数据(job_id=0)、任务 7 的两条、任务 8 的一条。
	if err := st.SaveExamHistory(nodeKey, sampleExamReport(10)); err != nil {
		t.Fatalf("save legacy: %v", err)
	}
	if err := st.SaveExamHistoryWithJob(nodeKey, sampleExamReport(70), 7); err != nil {
		t.Fatalf("save job7 #1: %v", err)
	}
	if err := st.SaveExamHistoryWithJob(nodeKey, sampleExamReport(71), 7); err != nil {
		t.Fatalf("save job7 #2: %v", err)
	}
	if err := st.SaveExamHistoryWithJob(nodeKey, sampleExamReport(80), 8); err != nil {
		t.Fatalf("save job8: %v", err)
	}
	if err := st.SaveExamHistoryWithJob("other.example.com:443", sampleExamReport(99), 7); err != nil {
		t.Fatalf("save other node: %v", err)
	}

	entry, err := st.ExamHistoryByJob(nodeKey, 7)
	if err != nil {
		t.Fatalf("by job: %v", err)
	}
	if entry == nil {
		t.Fatal("by job = nil, want job 7 entry")
	}
	if entry.JobID != 7 || entry.Report.Stability.Score != 71 {
		t.Fatalf("by job = (job %d, score %d), want (7, 71 latest of job 7)",
			entry.JobID, entry.Report.Stability.Score)
	}

	miss, err := st.ExamHistoryByJob(nodeKey, 999)
	if err != nil {
		t.Fatalf("by job miss: %v", err)
	}
	if miss != nil {
		t.Fatalf("by job 999 = %+v, want nil", miss)
	}
}

// TestLatestExamHistoryInWindow 时间窗回退:取窗口内该节点最新一条"旧数据"
// (job_id=0);带 job_id 的新数据与窗口外记录都不得命中。
func TestLatestExamHistoryInWindow(t *testing.T) {
	st := openExamHistoryStore(t)
	nodeKey := "example.com:443"

	base := time.Now()
	// 窗口外(早于起点)旧数据。
	if err := st.SaveExamHistory(nodeKey, sampleExamReport(10)); err != nil {
		t.Fatalf("save before window: %v", err)
	}
	backdateExamHistory(t, st, nodeKey, base.Add(-time.Hour))

	// 窗口内两条旧数据 + 一条带 job_id 的新数据(新数据不参与回退)。
	if err := st.SaveExamHistory(nodeKey, sampleExamReport(50)); err != nil {
		t.Fatalf("save in window #1: %v", err)
	}
	if err := st.SaveExamHistory(nodeKey, sampleExamReport(60)); err != nil {
		t.Fatalf("save in window #2: %v", err)
	}
	if err := st.SaveExamHistoryWithJob(nodeKey, sampleExamReport(99), 123); err != nil {
		t.Fatalf("save job-linked: %v", err)
	}

	start := base.Add(-time.Minute)
	end := base.Add(time.Minute)
	entry, err := st.LatestExamHistoryInWindow(nodeKey, start, end)
	if err != nil {
		t.Fatalf("in window: %v", err)
	}
	if entry == nil {
		t.Fatal("in window = nil, want latest legacy entry")
	}
	if entry.JobID != 0 || entry.Report.Stability.Score != 60 {
		t.Fatalf("in window = (job %d, score %d), want (0, 60 latest legacy)",
			entry.JobID, entry.Report.Stability.Score)
	}

	// 窗口内无旧数据:起点推到所有旧数据之后。
	miss, err := st.LatestExamHistoryInWindow(nodeKey, base.Add(2*time.Minute), base.Add(3*time.Minute))
	if err != nil {
		t.Fatalf("window miss: %v", err)
	}
	if miss != nil {
		t.Fatalf("window miss = %+v, want nil", miss)
	}
}

// backdateExamHistory 把某节点全部体检记录的 created_at 改为指定时刻(测试造窗口外用)。
func backdateExamHistory(t *testing.T, st *Store, nodeKey string, at time.Time) {
	t.Helper()
	if _, err := st.db.Exec(
		`UPDATE exam_history SET created_at = ? WHERE node_key = ?`, at, nodeKey); err != nil {
		t.Fatalf("backdate exam history: %v", err)
	}
}
