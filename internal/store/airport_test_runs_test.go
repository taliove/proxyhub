package store

import (
	"context"
	"testing"
	"time"
)

func newTestRun(airportID int64, status string) *AirportTestRun {
	return &AirportTestRun{
		AirportID:      airportID,
		CreatedAt:      time.Now(),
		SampleParams:   "{}",
		Status:         status,
		DimensionsJSON: "{}",
	}
}

// airport_test_runs.job_id 迁移列:可空语义对齐 refresh_runs(0 = 未关联任务),
// 老数据(迁移前建行)读出为 0,新行可回填关联的 jobs 任务 id。
func TestAirportTestRun_JobID(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	run := newTestRun(1, "completed")
	run.JobID = 42
	id, err := s.CreateAirportTestRun(ctx, run)
	if err != nil {
		t.Fatalf("CreateAirportTestRun() error = %v", err)
	}

	got, err := s.GetAirportTestRun(ctx, 1, id)
	if err != nil {
		t.Fatalf("GetAirportTestRun() error = %v", err)
	}
	if got.JobID != 42 {
		t.Errorf("JobID = %d, want 42", got.JobID)
	}

	// 未关联任务的行:JobID 读出 0(对齐 refresh_runs 的 0 语义)
	plain := newTestRun(1, "completed")
	plainID, err := s.CreateAirportTestRun(ctx, plain)
	if err != nil {
		t.Fatalf("CreateAirportTestRun() error = %v", err)
	}
	runs, err := s.ListAirportTestRuns(ctx, 1, 10)
	if err != nil {
		t.Fatalf("ListAirportTestRuns() error = %v", err)
	}
	byID := make(map[int64]int64, len(runs))
	for _, r := range runs {
		byID[r.ID] = r.JobID
	}
	if byID[id] != 42 {
		t.Errorf("List JobID for run %d = %d, want 42", id, byID[id])
	}
	if byID[plainID] != 0 {
		t.Errorf("List JobID for run %d = %d, want 0 (unlinked)", plainID, byID[plainID])
	}
}

// 启动残留收口:进行态(diagnosing/checking/scoring)标 failed 并写错误信息,
// 已终态(completed/failed)不动。对齐 FailRunningRefreshRuns 的模式与取值。
func TestFailRunningAirportTestRuns(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	statuses := []string{"diagnosing", "checking", "scoring", "completed", "failed"}
	ids := make(map[string]int64, len(statuses))
	for _, st := range statuses {
		id, err := s.CreateAirportTestRun(ctx, newTestRun(1, st))
		if err != nil {
			t.Fatalf("CreateAirportTestRun(%s) error = %v", st, err)
		}
		ids[st] = id
	}

	const errMsg = "process restarted"
	if err := s.FailRunningAirportTestRuns(errMsg); err != nil {
		t.Fatalf("FailRunningAirportTestRuns() error = %v", err)
	}

	for _, st := range []string{"diagnosing", "checking", "scoring"} {
		got, err := s.GetAirportTestRun(ctx, 1, ids[st])
		if err != nil {
			t.Fatalf("GetAirportTestRun(%s) error = %v", st, err)
		}
		if got.Status != "failed" {
			t.Errorf("run %s: Status = %s, want failed", st, got.Status)
		}
		if got.ErrorMessage != errMsg {
			t.Errorf("run %s: ErrorMessage = %q, want %q", st, got.ErrorMessage, errMsg)
		}
	}
	for _, st := range []string{"completed", "failed"} {
		got, err := s.GetAirportTestRun(ctx, 1, ids[st])
		if err != nil {
			t.Fatalf("GetAirportTestRun(%s) error = %v", st, err)
		}
		if got.Status != st {
			t.Errorf("terminal run %s: Status = %s, want unchanged %s", st, got.Status, st)
		}
		if got.ErrorMessage != "" {
			t.Errorf("terminal run %s: ErrorMessage = %q, want empty (untouched)", st, got.ErrorMessage)
		}
	}

	// 幂等:再跑一次不该有任何变化
	if err := s.FailRunningAirportTestRuns(errMsg); err != nil {
		t.Fatalf("FailRunningAirportTestRuns() second call error = %v", err)
	}
	got, _ := s.GetAirportTestRun(ctx, 1, ids["completed"])
	if got.Status != "completed" {
		t.Errorf("completed run changed after second call: %s", got.Status)
	}
}

// 进度镜像列:Update 必须持久化 sample_params(编排层检活进度反复更新该列;
// 此前 UPDATE 不含它,进度镜像被静默丢弃——jobs cursor 是主进度源,本列为镜像)。
func TestUpdateAirportTestRun_PersistsSampleParams(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id, err := s.CreateAirportTestRun(ctx, newTestRun(1, "checking"))
	if err != nil {
		t.Fatalf("CreateAirportTestRun() error = %v", err)
	}

	run, err := s.GetAirportTestRun(ctx, 1, id)
	if err != nil {
		t.Fatalf("GetAirportTestRun() error = %v", err)
	}
	run.SampleParams = `{"checked":3,"total_sample":5}`
	if err := s.UpdateAirportTestRun(ctx, run); err != nil {
		t.Fatalf("UpdateAirportTestRun() error = %v", err)
	}

	got, err := s.GetAirportTestRun(ctx, 1, id)
	if err != nil {
		t.Fatalf("GetAirportTestRun() error = %v", err)
	}
	if got.SampleParams != `{"checked":3,"total_sample":5}` {
		t.Errorf("SampleParams = %q, want persisted progress mirror", got.SampleParams)
	}
}

// 任务结果端点反查:按 job_id 取最新一条关联 run;无关联返回 (nil, nil)。
func TestGetAirportTestRunByJobID(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// 无关联:返回 nil 而非错误
	run, err := s.GetAirportTestRunByJobID(999)
	if err != nil || run != nil {
		t.Fatalf("GetAirportTestRunByJobID(999) = (%v, %v), want (nil, nil)", run, err)
	}

	first := newTestRun(1, "completed")
	first.JobID = 42
	if _, err := s.CreateAirportTestRun(ctx, first); err != nil {
		t.Fatalf("CreateAirportTestRun() error = %v", err)
	}
	second := newTestRun(1, "cancelled")
	second.JobID = 42
	secondID, err := s.CreateAirportTestRun(ctx, second)
	if err != nil {
		t.Fatalf("CreateAirportTestRun() error = %v", err)
	}
	other := newTestRun(1, "completed")
	other.JobID = 7
	if _, err := s.CreateAirportTestRun(ctx, other); err != nil {
		t.Fatalf("CreateAirportTestRun() error = %v", err)
	}

	got, err := s.GetAirportTestRunByJobID(42)
	if err != nil {
		t.Fatalf("GetAirportTestRunByJobID(42) error = %v", err)
	}
	if got == nil || got.ID != secondID {
		t.Fatalf("GetAirportTestRunByJobID(42) = %+v, want latest run id %d", got, secondID)
	}
	if got.Status != "cancelled" {
		t.Errorf("Status = %s, want cancelled", got.Status)
	}
}
