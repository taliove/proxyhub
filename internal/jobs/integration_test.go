package jobs

import (
	"encoding/json"
	"testing"
	"time"
)

// TestIntegration_RetagAllWithManager tests the full flow:
// Manager + retag_all kind executing successfully.
func TestIntegration_RetagAllWithManager(t *testing.T) {
	// Setup in-memory manager (no persistence)
	mgr := NewManager(nil)

	// Track recompute calls
	var recomputedNodes []string
	mockStore := &mockRetagStore{
		nodeKeys: []string{"node1", "node2", "node3"},
		recompute: func(key string) error {
			recomputedNodes = append(recomputedNodes, key)
			return nil
		},
	}

	// Register retag_all kind
	retagKind := &retagAllKind{
		nodePool:  func() []string { return mockStore.nodeKeys },
		recompute: mockStore.recompute,
	}
	mgr.Register(retagKind)

	// Open job (simulates scheduler trigger)
	sub, err := mgr.Open("retag_all", "nightly", nil)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer sub.Close()

	// Consume events until done
	eventCount := 0
	for event := range sub.Live {
		eventCount++
		var data map[string]interface{}
		if err := json.Unmarshal(event.Data, &data); err != nil {
			t.Fatalf("unmarshal event: %v", err)
		}
		phase := data["phase"]
		if phase == "done" {
			break
		}
	}

	// Verify all nodes were recomputed
	if len(recomputedNodes) != len(mockStore.nodeKeys) {
		t.Errorf("recomputed %d nodes, want %d", len(recomputedNodes), len(mockStore.nodeKeys))
	}

	// Verify events were emitted (start + 3 progress + done = 5)
	if eventCount < 4 {
		t.Errorf("got %d events, want at least 4", eventCount)
	}
}

// TestIntegration_SchedulerTriggersRetag tests scheduler triggering retag job.
func TestIntegration_SchedulerTriggersRetag(t *testing.T) {
	mgr := NewManager(nil)

	triggerCh := make(chan struct{}, 1)
	mockStore := &mockRetagStore{
		nodeKeys: []string{"node1"},
		recompute: func(key string) error {
			select {
			case triggerCh <- struct{}{}:
			default:
			}
			return nil
		},
	}

	retagKind := &retagAllKind{
		nodePool:  func() []string { return mockStore.nodeKeys },
		recompute: mockStore.recompute,
	}
	mgr.Register(retagKind)

	// Create scheduler with test clock
	clock := &mockClock{now: time.Date(2026, 7, 20, 3, 30, 0, 0, time.Local)}
	schedStore := &mockSchedulerStore{
		schedule: "03:30",
		enabled:  true,
	}

	sched := &scheduler{
		manager: mgr,
		store:   schedStore,
		trigger: func() {
			mgr.Open("retag_all", "nightly", nil)
		},
		clock: clock.Now,
	}

	// Tick should trigger the job
	sched.tick()

	// Wait for job to execute
	select {
	case <-triggerCh:
		// Success - job ran
	case <-time.After(200 * time.Millisecond):
		t.Error("scheduler did not trigger retag job within timeout")
	}
}

// mockRetagStore provides test doubles for retag_all kind.
type mockRetagStore struct {
	nodeKeys  []string
	recompute func(string) error
}

func (m *mockRetagStore) AllNodeKeys() ([]string, error) {
	return m.nodeKeys, nil
}

func (m *mockRetagStore) RecomputeNodeTags(key string) error {
	return m.recompute(key)
}
