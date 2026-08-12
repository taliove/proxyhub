package jobs

import (
	"fmt"
	"testing"
	"time"
)

// mockClock implements a virtual clock for testing.
type mockClock struct {
	now time.Time
}

func (m *mockClock) Now() time.Time {
	return m.now
}

func (m *mockClock) Advance(d time.Duration) {
	m.now = m.now.Add(d)
}

// TestScheduler_ParsesSchedule tests schedule configuration parsing.
func TestScheduler_ParsesSchedule(t *testing.T) {
	tests := []struct {
		name     string
		schedule string
		wantHour int
		wantMin  int
		wantErr  bool
	}{
		{"valid", "03:30", 3, 30, false},
		{"midnight", "00:00", 0, 0, false},
		{"noon", "12:00", 12, 0, false},
		{"invalid format", "3:30", 0, 0, true},
		{"invalid hour", "25:00", 0, 0, true},
		{"invalid minute", "03:60", 0, 0, true},
		{"empty", "", 0, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hour, min, err := parseScheduleTime(tt.schedule)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseScheduleTime(%q) error = %v, wantErr %v", tt.schedule, err, tt.wantErr)
				return
			}
			if !tt.wantErr && (hour != tt.wantHour || min != tt.wantMin) {
				t.Errorf("parseScheduleTime(%q) = %d:%d, want %d:%d", tt.schedule, hour, min, tt.wantHour, tt.wantMin)
			}
		})
	}
}

// TestScheduler_ChecksScheduleAtExactTime tests scheduler triggers at exact configured time.
func TestScheduler_ChecksScheduleAtExactTime(t *testing.T) {
	clock := &mockClock{now: time.Date(2026, 7, 20, 3, 29, 0, 0, time.Local)}
	var triggered int
	trigger := func() { triggered++ }

	store := &mockSchedulerStore{
		schedule: "03:30",
		enabled:  true,
	}
	s := &scheduler{
		manager: nil,
		store:   store,
		trigger: trigger,
		clock:   clock.Now,
	}

	// Before scheduled time
	s.tick()
	if triggered != 0 {
		t.Errorf("triggered before schedule time = %d, want 0", triggered)
	}

	// Advance to exact time
	clock.Advance(1 * time.Minute) // Now 03:30
	s.tick()
	if triggered != 1 {
		t.Errorf("triggered at exact time = %d, want 1", triggered)
	}

	// Same minute again - should not trigger (already ran today)
	s.tick()
	if triggered != 1 {
		t.Errorf("triggered again in same minute = %d, want 1", triggered)
	}
}

// TestScheduler_DoesNotTriggerWhenDisabled tests scheduler respects enabled flag.
func TestScheduler_DoesNotTriggerWhenDisabled(t *testing.T) {
	clock := &mockClock{now: time.Date(2026, 7, 20, 3, 30, 0, 0, time.Local)}
	var triggered int
	trigger := func() { triggered++ }

	store := &mockSchedulerStore{
		schedule: "03:30",
		enabled:  false, // Disabled
	}
	s := &scheduler{
		manager: nil,
		store:   store,
		trigger: trigger,
		clock:   clock.Now,
	}

	s.tick()
	if triggered != 0 {
		t.Errorf("triggered when disabled = %d, want 0", triggered)
	}
}

// TestScheduler_SkipsIfAlreadyRanToday tests idempotency within same day.
func TestScheduler_SkipsIfAlreadyRanToday(t *testing.T) {
	clock := &mockClock{now: time.Date(2026, 7, 20, 3, 30, 0, 0, time.Local)}
	var triggered int
	trigger := func() { triggered++ }

	store := &mockSchedulerStore{
		schedule: "03:30",
		enabled:  true,
		// Simulate job already completed today
		lastJob: &Record{
			Kind:      "retag_all",
			Key:       "nightly",
			Status:    StatusDone,
			CreatedAt: time.Date(2026, 7, 20, 3, 30, 0, 0, time.Local),
		},
	}
	s := &scheduler{
		manager: nil,
		store:   store,
		trigger: trigger,
		clock:   clock.Now,
	}

	s.tick()
	if triggered != 0 {
		t.Errorf("triggered when job already ran today = %d, want 0", triggered)
	}
}

// TestScheduler_TriggersNextDay tests scheduler resets daily.
func TestScheduler_TriggersNextDay(t *testing.T) {
	clock := &mockClock{now: time.Date(2026, 7, 20, 3, 30, 0, 0, time.Local)}
	var triggered int
	trigger := func() { triggered++ }

	store := &mockSchedulerStore{
		schedule: "03:30",
		enabled:  true,
		// Job ran yesterday
		lastJob: &Record{
			Kind:      "retag_all",
			Key:       "nightly",
			Status:    StatusDone,
			CreatedAt: time.Date(2026, 7, 19, 3, 30, 0, 0, time.Local),
		},
	}
	s := &scheduler{
		manager: nil,
		store:   store,
		trigger: trigger,
		clock:   clock.Now,
	}

	s.tick()
	if triggered != 1 {
		t.Errorf("triggered next day = %d, want 1", triggered)
	}
}

// TestScheduler_HandlesInvalidSchedule tests graceful handling of invalid config.
func TestScheduler_HandlesInvalidSchedule(t *testing.T) {
	clock := &mockClock{now: time.Date(2026, 7, 20, 3, 30, 0, 0, time.Local)}
	var triggered int
	trigger := func() { triggered++ }

	store := &mockSchedulerStore{
		schedule: "invalid",
		enabled:  true,
	}
	s := &scheduler{
		manager: nil,
		store:   store,
		trigger: trigger,
		clock:   clock.Now,
	}

	s.tick() // Should not panic, should not trigger
	if triggered != 0 {
		t.Errorf("triggered with invalid schedule = %d, want 0", triggered)
	}
}

// mockSchedulerStore provides test data for scheduler.
type mockSchedulerStore struct {
	schedule string
	enabled  bool
	lastJob  *Record
}

func (m *mockSchedulerStore) GetSetting(key string) (string, error) {
	if key == "schedule_retag_time" {
		return m.schedule, nil
	}
	if key == "schedule_retag_enabled" {
		if m.enabled {
			return "true", nil
		}
		return "false", nil
	}
	return "", nil
}

func (m *mockSchedulerStore) GetLatestJobByKindKey(kind, key string) (*Record, error) {
	return m.lastJob, nil
}

// mockExamStore 全员补齐定时档的 settings 桩:只认 schedule_exam_* 键,retag 键报不存在。
type mockExamStore struct {
	lastJob *Record
}

func (m *mockExamStore) GetSetting(key string) (string, error) {
	switch key {
	case "schedule_exam_time":
		return "04:00", nil
	case "schedule_exam_enabled":
		return "true", nil
	}
	return "", fmt.Errorf("no such setting: %s", key)
}

func (m *mockExamStore) GetLatestJobByKindKey(kind, key string) (*Record, error) {
	return m.lastJob, nil
}

// TestSchedulerTick_ExamAllTask 定时全员补齐:触发器已注入且配置齐备时到点触发;
// 当日已有 batch_exam 记录(含手动触发)则跳过(信息当日已产出)。
func TestSchedulerTick_ExamAllTask(t *testing.T) {
	clock := &mockClock{now: time.Date(2026, 7, 20, 4, 0, 0, 0, time.Local)}
	var triggered int

	store := &mockExamStore{}
	s := NewScheduler(nil, store, nil)
	s.clock = clock.Now
	s.SetExamAllTrigger(func() { triggered++ })

	s.tick()
	if triggered != 1 {
		t.Errorf("triggered at exam time = %d, want 1", triggered)
	}

	// 当日已有 batch_exam 记录(如手动补齐过):不再触发
	store.lastJob = &Record{
		Kind:      "batch_exam",
		Key:       "batch_exam",
		Status:    StatusDone,
		CreatedAt: time.Date(2026, 7, 20, 1, 0, 0, 0, time.Local),
	}
	s.tick()
	if triggered != 1 {
		t.Errorf("triggered with same-day exam job = %d, want 1 (skip)", triggered)
	}
}

// TestSchedulerTick_ExamAllWithoutTrigger 未注入触发器时 exam 档静默跳过(不 panic)。
func TestSchedulerTick_ExamAllWithoutTrigger(t *testing.T) {
	clock := &mockClock{now: time.Date(2026, 7, 20, 4, 0, 0, 0, time.Local)}
	s := NewScheduler(nil, &mockExamStore{}, nil)
	s.clock = clock.Now
	s.tick() // 不 panic 即通过
}
