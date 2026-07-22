package server

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestProbeRunRegistry_CreateAndGet(t *testing.T) {
	reg := newProbeRunRegistry(time.Minute)
	run := reg.create(7, true, 5)

	if run.RunID == "" {
		t.Fatal("run id must not be empty")
	}
	if run.Status != probeRunRunning {
		t.Errorf("status = %q, want %q", run.Status, probeRunRunning)
	}
	if run.EndpointID != 7 || run.Total != 5 || !run.Full {
		t.Errorf("unexpected run fields: %+v", run)
	}

	got, ok := reg.get(run.RunID)
	if !ok {
		t.Fatal("created run must be retrievable")
	}
	if got != run {
		t.Errorf("get = %+v, want %+v", got, run)
	}
}

func TestProbeRunRegistry_UnknownRun(t *testing.T) {
	reg := newProbeRunRegistry(time.Minute)
	if _, ok := reg.get("no-such-run"); ok {
		t.Error("unknown run id must not be found")
	}
}

func TestProbeRunRegistry_ProgressAndFinish(t *testing.T) {
	reg := newProbeRunRegistry(time.Minute)
	run := reg.create(1, false, 10)

	reg.markSampled(run.RunID, 4)
	reg.markChecked(run.RunID, 2)
	reg.finish(run.RunID, nil)

	got, ok := reg.get(run.RunID)
	if !ok {
		t.Fatal("run lost after finish")
	}
	if got.Sampled != 4 || got.Checked != 2 {
		t.Errorf("progress = %d/%d, want 2/4", got.Checked, got.Sampled)
	}
	if got.Status != probeRunCompleted {
		t.Errorf("status = %q, want %q", got.Status, probeRunCompleted)
	}
	if got.Error != "" {
		t.Errorf("error = %q, want empty on success", got.Error)
	}
}

func TestProbeRunRegistry_FinishWithError(t *testing.T) {
	reg := newProbeRunRegistry(time.Minute)
	run := reg.create(1, false, 3)

	reg.finish(run.RunID, errors.New("boom"))

	got, _ := reg.get(run.RunID)
	if got.Status != probeRunFailed {
		t.Errorf("status = %q, want %q", got.Status, probeRunFailed)
	}
	if got.Error != "boom" {
		t.Errorf("error = %q, want %q", got.Error, "boom")
	}
}

func TestProbeRunRegistry_TTLExpiry(t *testing.T) {
	now := time.Now()
	reg := newProbeRunRegistry(time.Minute)
	reg.now = func() time.Time { return now }

	run := reg.create(1, false, 1)
	now = now.Add(2 * time.Minute)

	if _, ok := reg.get(run.RunID); ok {
		t.Error("run idle past TTL must be evicted (restart-equivalent: poller gets 404)")
	}
}

func TestProbeRunRegistry_ProgressKeepsRunAlive(t *testing.T) {
	now := time.Now()
	reg := newProbeRunRegistry(time.Minute)
	reg.now = func() time.Time { return now }

	run := reg.create(1, false, 2)
	now = now.Add(50 * time.Second)
	reg.markChecked(run.RunID, 1) // 活跃更新刷新过期基准
	now = now.Add(50 * time.Second)

	if _, ok := reg.get(run.RunID); !ok {
		t.Error("run with recent progress must not expire")
	}
}

func TestProbeRunRegistry_GetReturnsCopy(t *testing.T) {
	reg := newProbeRunRegistry(time.Minute)
	run := reg.create(1, false, 1)

	got, _ := reg.get(run.RunID)
	got.Checked = 99

	again, _ := reg.get(run.RunID)
	if again.Checked == 99 {
		t.Error("get must return a copy; caller mutation leaked into registry")
	}
}

func TestProbeRunRegistry_Concurrent(t *testing.T) {
	reg := newProbeRunRegistry(time.Minute)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			run := reg.create(id, false, 10)
			for c := 1; c <= 10; c++ {
				reg.markChecked(run.RunID, c)
			}
			reg.finish(run.RunID, nil)
			got, ok := reg.get(run.RunID)
			if !ok {
				t.Errorf("run %s lost under concurrency", run.RunID)
				return
			}
			if got.Checked != 10 || got.Status != probeRunCompleted {
				t.Errorf("run %s state = checked %d status %q, want 10/completed",
					run.RunID, got.Checked, got.Status)
			}
		}(int64(i))
	}
	wg.Wait()
}
