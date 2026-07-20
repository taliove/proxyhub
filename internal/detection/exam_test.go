package detection

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestExamOrchestrator_SerialOrderAndExclusivity(t *testing.T) {
	var order []string
	var active atomic.Int32
	var overlap atomic.Bool

	makeStage := func(name string) examStage {
		return examStage{
			name: name,
			run: func(_ context.Context, emit func(ExamEvent), _ *ExamReport) {
				// 进入时若已有其他段在跑 -> 记录重叠(违反独占)。
				if active.Add(1) != 1 {
					overlap.Store(true)
				}
				order = append(order, name)
				emit(ExamEvent{Phase: "section_done", Section: name})
				active.Add(-1)
			},
		}
	}

	orch := &ExamOrchestrator{stages: []examStage{
		makeStage("stability"),
		makeStage("speed"),
		makeStage("unlock"),
	}}

	var events []ExamEvent
	report := orch.Run(context.Background(), func(e ExamEvent) { events = append(events, e) })

	if overlap.Load() {
		t.Error("stages overlapped: sampling exclusivity violated")
	}
	want := []string{"stability", "speed", "unlock"}
	for i, w := range want {
		if i >= len(order) || order[i] != w {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
	// 事件序列:3 个 section_done 后跟 1 个 done。
	if len(events) != 4 {
		t.Fatalf("events = %d, want 4", len(events))
	}
	for i, w := range want {
		if events[i].Phase != "section_done" || events[i].Section != w {
			t.Errorf("event[%d] = %+v, want section_done/%s", i, events[i], w)
		}
	}
	last := events[len(events)-1]
	if last.Phase != "done" || last.Report == nil {
		t.Errorf("last event = %+v, want done with report", last)
	}
	_ = report
}

func TestExamOrchestrator_StopsOnCancel(t *testing.T) {
	var ran []string
	ctx, cancel := context.WithCancel(context.Background())

	orch := &ExamOrchestrator{stages: []examStage{
		{name: "a", run: func(_ context.Context, _ func(ExamEvent), _ *ExamReport) {
			ran = append(ran, "a")
			cancel() // 第一段结束即取消
		}},
		{name: "b", run: func(_ context.Context, _ func(ExamEvent), _ *ExamReport) {
			ran = append(ran, "b")
		}},
	}}

	orch.Run(ctx, func(ExamEvent) {})

	if len(ran) != 1 || ran[0] != "a" {
		t.Errorf("ran = %v, want [a] (cancel should stop before b)", ran)
	}
}

func TestStabilityStage_EmitsSamplesThenMetrics(t *testing.T) {
	fc := &fakeClock{cur: time.Unix(0, 0)}
	probe := func(_ context.Context) (int, bool) {
		fc.sleep(0)
		return 25, true
	}
	cfg := ExamConfig{StabilityDurationSec: 3, StabilityIntervalMs: 1000}

	stage := stabilityStage(cfg, fc.clock(), probe)
	if stage.name != "stability" {
		t.Fatalf("stage name = %q, want stability", stage.name)
	}

	var samples, sectionDone int
	var report ExamReport
	stage.run(context.Background(), func(e ExamEvent) {
		switch e.Phase {
		case "sample":
			samples++
			if e.Section != "stability" || e.Sample == nil {
				t.Errorf("bad sample event: %+v", e)
			}
		case "section_done":
			sectionDone++
			if e.Metrics == nil {
				t.Error("section_done missing metrics")
			}
		}
	}, &report)

	if samples != 3 {
		t.Errorf("sample events = %d, want 3 (3s / 1Hz)", samples)
	}
	if sectionDone != 1 {
		t.Errorf("section_done events = %d, want 1", sectionDone)
	}
	if report.Stability == nil || report.Stability.Succeeded != 3 {
		t.Errorf("report.Stability = %+v, want 3 succeeded", report.Stability)
	}
}
