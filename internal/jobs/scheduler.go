package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"
)

// Scheduler manages periodic task execution based on settings-driven schedules.
// Currently supports a single nightly retag_all task.
type Scheduler struct {
	manager *Manager
	store   schedulerStore
	logger  *slog.Logger
	clock   func() time.Time
}

// schedulerStore defines the minimal store interface needed by the scheduler.
type schedulerStore interface {
	GetSetting(key string) (string, error)
	GetLatestJobByKindKey(kind, key string) (*Record, error)
}

// NewScheduler creates a scheduler that checks settings every minute.
func NewScheduler(mgr *Manager, store schedulerStore, logger *slog.Logger) *Scheduler {
	return &Scheduler{
		manager: mgr,
		store:   store,
		logger:  logger,
		clock:   time.Now,
	}
}

// Run starts the scheduler loop, checking every minute until context is cancelled.
func (s *Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.tick()
		}
	}
}

// tick performs one scheduler check: read config, check if time matches, check if already ran, trigger if needed.
func (s *Scheduler) tick() {
	// Read schedule configuration
	timeStr, err := s.store.GetSetting("schedule_retag_time")
	if err != nil {
		return // No config = no schedule
	}

	enabledStr, err := s.store.GetSetting("schedule_retag_enabled")
	if err != nil || enabledStr != "true" {
		return // Disabled or missing
	}

	// Parse schedule time
	schedHour, schedMin, err := parseScheduleTime(timeStr)
	if err != nil {
		s.logger.Warn("invalid schedule time", "value", timeStr, "error", err)
		return
	}

	// Check if current time matches schedule
	now := s.clock()
	if now.Hour() != schedHour || now.Minute() != schedMin {
		return // Not time yet
	}

	// Check if already ran today
	lastJob, err := s.store.GetLatestJobByKindKey("retag_all", "nightly")
	if err == nil && lastJob != nil {
		// Job exists - check if it ran today
		if sameDay(lastJob.CreatedAt, now) {
			return // Already ran today
		}
	}

	// Trigger the job
	s.triggerRetagAll()
}

// triggerRetagAll opens a retag_all job with key "nightly".
func (s *Scheduler) triggerRetagAll() {
	_, err := s.manager.Open("retag_all", "nightly", nil)
	if err != nil {
		s.logger.Error("failed to trigger nightly retag", "error", err)
	} else {
		s.logger.Info("triggered nightly retag")
	}
}

// parseScheduleTime parses "HH:MM" format into hour and minute.
// Requires zero-padded format (e.g., "03:30", not "3:30").
func parseScheduleTime(s string) (hour, minute int, err error) {
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid time format %q, expected HH:MM", s)
	}

	// Require exactly 2 digits for each part
	if len(parts[0]) != 2 || len(parts[1]) != 2 {
		return 0, 0, fmt.Errorf("invalid time format %q, expected HH:MM with zero-padding", s)
	}

	hour, err = strconv.Atoi(parts[0])
	if err != nil || hour < 0 || hour > 23 {
		return 0, 0, fmt.Errorf("invalid hour %q", parts[0])
	}

	minute, err = strconv.Atoi(parts[1])
	if err != nil || minute < 0 || minute > 59 {
		return 0, 0, fmt.Errorf("invalid minute %q", parts[1])
	}

	return hour, minute, nil
}

// sameDay returns true if two times fall on the same calendar day (local timezone).
func sameDay(t1, t2 time.Time) bool {
	y1, m1, d1 := t1.Date()
	y2, m2, d2 := t2.Date()
	return y1 == y2 && m1 == m2 && d1 == d2
}

// scheduler is a test-friendly wrapper used in tests.
type scheduler struct {
	manager     *Manager
	store       schedulerStore
	trigger     func()
	clock       func() time.Time
	lastTrigger time.Time
}

func (s *scheduler) tick() {
	timeStr, err := s.store.GetSetting("schedule_retag_time")
	if err != nil {
		return
	}

	enabledStr, err := s.store.GetSetting("schedule_retag_enabled")
	if err != nil || enabledStr != "true" {
		return
	}

	schedHour, schedMin, err := parseScheduleTime(timeStr)
	if err != nil {
		return
	}

	now := s.clock()
	if now.Hour() != schedHour || now.Minute() != schedMin {
		return
	}

	// Prevent multiple triggers in same minute
	if !s.lastTrigger.IsZero() && sameMinute(s.lastTrigger, now) {
		return
	}

	lastJob, err := s.store.GetLatestJobByKindKey("retag_all", "nightly")
	if err == nil && lastJob != nil {
		if sameDay(lastJob.CreatedAt, now) {
			return
		}
	}

	s.lastTrigger = now
	s.trigger()
}

// sameMinute returns true if two times are in the same minute.
func sameMinute(t1, t2 time.Time) bool {
	return t1.Year() == t2.Year() &&
		t1.Month() == t2.Month() &&
		t1.Day() == t2.Day() &&
		t1.Hour() == t2.Hour() &&
		t1.Minute() == t2.Minute()
}
