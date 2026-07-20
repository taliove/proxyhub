package jobs

import (
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
