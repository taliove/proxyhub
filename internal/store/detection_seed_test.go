package store

import (
	"testing"

	"github.com/taliove/proxyhub/internal/detection"
)

// TestSeedDetectionTargets_FreshDB 首次启动(setting 不存在)播种 6 个解锁 Target。
func TestSeedDetectionTargets_FreshDB(t *testing.T) {
	s := newTestStore(t)

	// 裸库不含 detection_targets(播种是应用层首次启动职责,非 store.Open)
	if _, err := s.GetSetting("detection_targets"); err == nil {
		t.Fatal("fresh store unexpectedly has detection_targets before seeding")
	}

	if err := s.SeedDetectionTargets(); err != nil {
		t.Fatalf("SeedDetectionTargets() error = %v", err)
	}

	targets, err := s.GetDetectionTargets()
	if err != nil {
		t.Fatalf("GetDetectionTargets() error = %v", err)
	}
	if len(targets) != 6 {
		t.Fatalf("seeded targets len = %d, want 6", len(targets))
	}

	kinds := make(map[detection.Kind]bool)
	for _, tg := range targets {
		kinds[tg.Kind] = true
	}
	for _, want := range []detection.Kind{
		detection.KindNetflix, detection.KindYouTubePremium, detection.KindDisneyPlus,
		detection.KindOpenAI, detection.KindClaude, detection.KindGemini,
	} {
		if !kinds[want] {
			t.Errorf("seeded targets missing kind %q", want)
		}
	}
}

// TestSeedDetectionTargets_NoOverride 已有用户配置时,再次播种(模拟重启)不覆盖。
func TestSeedDetectionTargets_NoOverride(t *testing.T) {
	s := newTestStore(t)

	// 用户改成一个自定义 generic 目标
	custom := []detection.Target{
		{Name: "MyOnly", URL: "http://example.com/probe", Method: "GET", ExpectStatus: []int{200}},
	}
	if err := s.SetDetectionTargets(custom); err != nil {
		t.Fatalf("SetDetectionTargets() error = %v", err)
	}

	// 再次播种(模拟进程重启):setting 已存在,不得覆盖
	if err := s.SeedDetectionTargets(); err != nil {
		t.Fatalf("SeedDetectionTargets() error = %v", err)
	}

	got, err := s.GetDetectionTargets()
	if err != nil {
		t.Fatalf("GetDetectionTargets() error = %v", err)
	}
	if len(got) != 1 || got[0].Name != "MyOnly" {
		t.Errorf("user config overridden by seeding: got %+v, want single MyOnly", got)
	}
}

// TestSeedDetectionTargets_EmptyListRespected 用户显式清空为空列表也不应被重新播种。
func TestSeedDetectionTargets_EmptyListRespected(t *testing.T) {
	s := newTestStore(t)

	if err := s.SetDetectionTargets([]detection.Target{}); err != nil {
		t.Fatalf("SetDetectionTargets() error = %v", err)
	}

	if err := s.SeedDetectionTargets(); err != nil {
		t.Fatalf("SeedDetectionTargets() error = %v", err)
	}

	got, err := s.GetDetectionTargets()
	if err != nil {
		t.Fatalf("GetDetectionTargets() error = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("explicit empty list re-seeded: got %d targets, want 0", len(got))
	}
}

// TestSeedDetectionTargets_Idempotent 多次播种结果稳定(不累加、不报错)。
func TestSeedDetectionTargets_Idempotent(t *testing.T) {
	s := newTestStore(t)

	for i := 0; i < 3; i++ {
		if err := s.SeedDetectionTargets(); err != nil {
			t.Fatalf("SeedDetectionTargets() call %d error = %v", i, err)
		}
	}

	got, err := s.GetDetectionTargets()
	if err != nil {
		t.Fatalf("GetDetectionTargets() error = %v", err)
	}
	if len(got) != 6 {
		t.Errorf("after repeated seeding len = %d, want 6 (must not accumulate)", len(got))
	}
}
