package store

import (
	"testing"
)

func TestCreateRefreshRun(t *testing.T) {
	s := newTestStore(t)

	run, err := s.CreateRefreshRun(RefreshTriggerManual, 0)
	if err != nil {
		t.Fatalf("CreateRefreshRun() error = %v", err)
	}

	if run.ID == 0 {
		t.Error("run.ID should not be 0")
	}
	if run.Trigger != RefreshTriggerManual {
		t.Errorf("Trigger = %s, want %s", run.Trigger, RefreshTriggerManual)
	}
	if run.Status != RefreshStatusRunning {
		t.Errorf("Status = %s, want %s", run.Status, RefreshStatusRunning)
	}
	if run.FinishedAt != nil {
		t.Error("FinishedAt should be nil for a running run")
	}
}

func TestFinishRefreshRun(t *testing.T) {
	s := newTestStore(t)

	run, _ := s.CreateRefreshRun(RefreshTriggerScheduled, 0)
	if err := s.FinishRefreshRun(run.ID, RefreshStatusPartial, 100, 60, 30, "1 个机场失败"); err != nil {
		t.Fatalf("FinishRefreshRun() error = %v", err)
	}

	got, err := s.GetRefreshRun(run.ID)
	if err != nil {
		t.Fatalf("GetRefreshRun() error = %v", err)
	}
	if got.Status != RefreshStatusPartial {
		t.Errorf("Status = %s, want %s", got.Status, RefreshStatusPartial)
	}
	if got.TotalNodes != 100 || got.AvailableNodes != 60 || got.FinalNodes != 30 {
		t.Errorf("counts = %d/%d/%d, want 100/60/30", got.TotalNodes, got.AvailableNodes, got.FinalNodes)
	}
	if got.Error != "1 个机场失败" {
		t.Errorf("Error = %s", got.Error)
	}
	if got.FinishedAt == nil {
		t.Error("FinishedAt should be set after finish")
	}
}

func TestGetRefreshRun_NotFound(t *testing.T) {
	s := newTestStore(t)

	if _, err := s.GetRefreshRun(999); err != ErrNotFound {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestAppendAndListRefreshEvents(t *testing.T) {
	s := newTestStore(t)

	run, _ := s.CreateRefreshRun(RefreshTriggerManual, 0)
	events := []struct{ level, stage, message, data string }{
		{"info", "fetch", "开始拉取机场 A", ""},
		{"warn", "fetch", "机场 B 拉取失败", `{"error":"status 401"}`},
		{"info", "check", "健康检查完成", `{"available":12}`},
	}
	for _, e := range events {
		if err := s.AppendRefreshEvent(run.ID, e.level, e.stage, e.message, e.data); err != nil {
			t.Fatalf("AppendRefreshEvent() error = %v", err)
		}
	}

	got, err := s.ListRefreshEvents(run.ID)
	if err != nil {
		t.Fatalf("ListRefreshEvents() error = %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len(events) = %d, want 3", len(got))
	}
	// 事件按写入顺序返回
	if got[0].Message != "开始拉取机场 A" || got[2].Stage != "check" {
		t.Errorf("events out of order: %+v", got)
	}
	if got[1].Level != "warn" || got[1].Data != `{"error":"status 401"}` {
		t.Errorf("event[1] = %+v", got[1])
	}
}

func TestListRefreshRuns_OrderAndLimit(t *testing.T) {
	s := newTestStore(t)

	first, _ := s.CreateRefreshRun(RefreshTriggerStartup, 0)
	second, _ := s.CreateRefreshRun(RefreshTriggerManual, 0)

	runs, err := s.ListRefreshRuns(10)
	if err != nil {
		t.Fatalf("ListRefreshRuns() error = %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("len(runs) = %d, want 2", len(runs))
	}
	// 最新的在前
	if runs[0].ID != second.ID || runs[1].ID != first.ID {
		t.Errorf("runs order = [%d, %d], want [%d, %d]", runs[0].ID, runs[1].ID, second.ID, first.ID)
	}

	limited, _ := s.ListRefreshRuns(1)
	if len(limited) != 1 || limited[0].ID != second.ID {
		t.Errorf("limit=1 should return only the newest run")
	}
}

func TestRefreshRunCleanup_KeepsRecent(t *testing.T) {
	s := newTestStore(t)

	// 写满超过上限的 run，每个带一条事件
	var firstID int64
	for i := 0; i < MaxRefreshRuns+5; i++ {
		run, err := s.CreateRefreshRun(RefreshTriggerScheduled, 0)
		if err != nil {
			t.Fatalf("CreateRefreshRun() error = %v", err)
		}
		if i == 0 {
			firstID = run.ID
		}
		if err := s.AppendRefreshEvent(run.ID, "info", "fetch", "event", ""); err != nil {
			t.Fatalf("AppendRefreshEvent() error = %v", err)
		}
	}

	runs, err := s.ListRefreshRuns(MaxRefreshRuns * 2)
	if err != nil {
		t.Fatalf("ListRefreshRuns() error = %v", err)
	}
	if len(runs) != MaxRefreshRuns {
		t.Errorf("len(runs) = %d, want %d", len(runs), MaxRefreshRuns)
	}

	// 最旧的 run 及其事件已被级联清理
	if _, err := s.GetRefreshRun(firstID); err != ErrNotFound {
		t.Errorf("oldest run should be cleaned, err = %v", err)
	}
	events, _ := s.ListRefreshEvents(firstID)
	if len(events) != 0 {
		t.Errorf("orphan events = %d, want 0", len(events))
	}
}
