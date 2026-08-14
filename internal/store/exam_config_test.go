package store

import (
	"path/filepath"
	"testing"
)

func openExamStore(t *testing.T) *Store {
	t.Helper()
	st, err := OpenForTesting(filepath.Join(t.TempDir(), "exam.db"))
	if err != nil {
		t.Fatalf("OpenForTesting() error = %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestGetExamConfig_Default(t *testing.T) {
	st := openExamStore(t)
	cfg := st.GetExamConfig()
	if cfg.StabilityDurationSec != 30 {
		t.Errorf("duration = %d, want default 30", cfg.StabilityDurationSec)
	}
	if cfg.StabilityIntervalMs != 1000 {
		t.Errorf("interval = %d, want default 1000", cfg.StabilityIntervalMs)
	}
}

func TestGetExamConfig_ValidOverride(t *testing.T) {
	st := openExamStore(t)
	if err := st.SaveSystemSettings(map[string]string{settingExamStabilityDuration: "45"}); err != nil {
		t.Fatalf("set: %v", err)
	}
	cfg := st.GetExamConfig()
	if cfg.StabilityDurationSec != 45 {
		t.Errorf("duration = %d, want 45", cfg.StabilityDurationSec)
	}
}

func TestGetExamConfig_IllegalFallsBackToDefault(t *testing.T) {
	for _, bad := range []string{"abc", "-5", "0", "3.5", ""} {
		st := openExamStore(t)
		if err := st.SaveSystemSettings(map[string]string{settingExamStabilityDuration: bad}); err != nil {
			t.Fatalf("set %q: %v", bad, err)
		}
		cfg := st.GetExamConfig()
		if cfg.StabilityDurationSec != 30 {
			t.Errorf("duration for %q = %d, want default 30", bad, cfg.StabilityDurationSec)
		}
	}
}
