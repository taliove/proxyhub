package server

import (
	"sync"
	"testing"
	"time"
)

func TestMFAPendingCreateAndConsume(t *testing.T) {
	m := NewMFAPendingManager()

	token, err := m.Create(7, "203.0.113.9")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(token) < 32 {
		t.Fatalf("token too short: %q", token)
	}
	if m.Len() != 1 {
		t.Fatalf("Len = %d, want 1", m.Len())
	}

	p, ok := m.Consume(token, "203.0.113.9")
	if !ok {
		t.Fatal("Consume rejected a fresh token")
	}
	if p.UserID != 7 {
		t.Fatalf("UserID = %d, want 7", p.UserID)
	}
	if p.IP != "203.0.113.9" {
		t.Fatalf("IP = %q, want 203.0.113.9", p.IP)
	}
	if m.Len() != 0 {
		t.Fatalf("Len = %d after consume, want 0", m.Len())
	}

	// One-shot: the same token must never work twice.
	if _, ok := m.Consume(token, "203.0.113.9"); ok {
		t.Fatal("token consumed twice")
	}
}

func TestMFAPendingTokensAreUnique(t *testing.T) {
	m := NewMFAPendingManager()
	a, err := m.Create(1, "203.0.113.1")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	b, err := m.Create(1, "203.0.113.1")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if a == b {
		t.Fatal("two pending tokens are identical")
	}
}

func TestMFAPendingUnknownToken(t *testing.T) {
	m := NewMFAPendingManager()
	if _, ok := m.Consume("", "203.0.113.1"); ok {
		t.Fatal("empty token accepted")
	}
	if _, ok := m.Consume("deadbeef", "203.0.113.1"); ok {
		t.Fatal("unknown token accepted")
	}
}

func TestMFAPendingExpires(t *testing.T) {
	m := NewMFAPendingManager()
	token, err := m.Create(3, "203.0.113.5")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Just inside the TTL.
	if _, ok := m.consumeAt(token, "203.0.113.5", time.Now().Add(mfaPendingTTL-time.Second)); !ok {
		t.Fatal("token rejected inside its TTL")
	}

	token, err = m.Create(3, "203.0.113.5")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, ok := m.consumeAt(token, "203.0.113.5", time.Now().Add(mfaPendingTTL+time.Second)); ok {
		t.Fatal("expired token accepted")
	}
	if m.Len() != 0 {
		t.Fatalf("expired token not dropped, Len = %d", m.Len())
	}
}

func TestMFAPendingIPMismatchRejected(t *testing.T) {
	m := NewMFAPendingManager()
	token, err := m.Create(4, "203.0.113.7")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, ok := m.Consume(token, "198.51.100.2"); ok {
		t.Fatal("token accepted from a different IP")
	}
	// A mismatch must not destroy the pending session: the legitimate
	// client on the original IP can still finish.
	if _, ok := m.Consume(token, "203.0.113.7"); !ok {
		t.Fatal("token rejected from its original IP after a mismatch attempt")
	}
}

func TestMFAPendingFailureBudget(t *testing.T) {
	m := NewMFAPendingManager()
	token, err := m.Create(5, "203.0.113.8")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	for i := 1; i < mfaPendingMaxFailures; i++ {
		alive := m.RecordFailure(token)
		if !alive {
			t.Fatalf("pending destroyed after %d failures, want it alive until %d", i, mfaPendingMaxFailures)
		}
		if m.Len() != 1 {
			t.Fatalf("Len = %d after %d failures, want 1", m.Len(), i)
		}
	}

	if alive := m.RecordFailure(token); alive {
		t.Fatalf("pending survived %d failures", mfaPendingMaxFailures)
	}
	if m.Len() != 0 {
		t.Fatalf("Len = %d after failure budget exhausted, want 0", m.Len())
	}
	if _, ok := m.Consume(token, "203.0.113.8"); ok {
		t.Fatal("token usable after failure budget exhausted")
	}
	// RecordFailure on an unknown token is a no-op reporting "not alive".
	if alive := m.RecordFailure(token); alive {
		t.Fatal("RecordFailure reported an unknown token as alive")
	}
}

func TestMFAPendingRecordFailureOnExpiredToken(t *testing.T) {
	m := NewMFAPendingManager()
	token, err := m.Create(21, "203.0.113.40")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	m.expireForTest(token)

	if alive := m.RecordFailure(token); alive {
		t.Fatal("RecordFailure reported an expired token as alive")
	}
	if m.Len() != 0 {
		t.Fatalf("Len = %d, want 0 (expired entry should be dropped)", m.Len())
	}
	if alive := m.RecordFailure(""); alive {
		t.Fatal("RecordFailure reported an empty token as alive")
	}
}

func TestMFAPendingIPMismatchDoesNotSpendBudget(t *testing.T) {
	m := NewMFAPendingManager()
	token, err := m.Create(6, "203.0.113.10")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	for i := 0; i < mfaPendingMaxFailures+3; i++ {
		if _, ok := m.Consume(token, "198.51.100.9"); ok {
			t.Fatal("token accepted from wrong IP")
		}
	}
	if _, ok := m.Consume(token, "203.0.113.10"); !ok {
		t.Fatal("wrong-IP attempts consumed the failure budget")
	}
}

func TestMFAPendingDestroy(t *testing.T) {
	m := NewMFAPendingManager()
	token, err := m.Create(8, "203.0.113.11")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	m.Destroy(token)
	if m.Len() != 0 {
		t.Fatalf("Len = %d after Destroy, want 0", m.Len())
	}
	if _, ok := m.Consume(token, "203.0.113.11"); ok {
		t.Fatal("destroyed token accepted")
	}
	m.Destroy("nonexistent") // must not panic
}

func TestMFAPendingConcurrentConsumeSingleWinner(t *testing.T) {
	m := NewMFAPendingManager()
	token, err := m.Create(9, "203.0.113.12")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	const goroutines = 64
	var (
		start sync.WaitGroup
		done  sync.WaitGroup
		mu    sync.Mutex
		wins  int
	)
	start.Add(1)
	for i := 0; i < goroutines; i++ {
		done.Add(1)
		go func() {
			defer done.Done()
			start.Wait()
			if _, ok := m.Consume(token, "203.0.113.12"); ok {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}()
	}
	start.Done()
	done.Wait()

	if wins != 1 {
		t.Fatalf("concurrent Consume succeeded %d times, want exactly 1", wins)
	}
	if m.Len() != 0 {
		t.Fatalf("Len = %d after concurrent consume, want 0", m.Len())
	}
}

func TestMFAPendingConcurrentMixedOperations(t *testing.T) {
	m := NewMFAPendingManager()

	const workers = 32
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			token, err := m.Create(int64(n), "203.0.113.20")
			if err != nil {
				t.Errorf("Create: %v", err)
				return
			}
			m.RecordFailure(token)
			m.Len()
			if _, ok := m.Consume(token, "203.0.113.20"); !ok {
				t.Errorf("worker %d could not consume its own token", n)
			}
		}(i)
	}
	wg.Wait()

	if m.Len() != 0 {
		t.Fatalf("Len = %d, want 0", m.Len())
	}
}

func TestMFAPendingLazyCleanupOnCreate(t *testing.T) {
	m := NewMFAPendingManager()
	stale, err := m.Create(11, "203.0.113.30")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Force the entry to look expired, then create a new pending session:
	// the sweep must drop the stale entry.
	m.expireForTest(stale)
	if _, err := m.Create(12, "203.0.113.31"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if m.Len() != 1 {
		t.Fatalf("Len = %d, want 1 (stale entry should have been swept)", m.Len())
	}
	if _, ok := m.Consume(stale, "203.0.113.30"); ok {
		t.Fatal("stale token still consumable")
	}
}

// expireForTest backdates a pending entry so cleanup paths treat it as stale.
func (m *MFAPendingManager) expireForTest(token string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.pending[token]
	if !ok {
		return
	}
	p.expiry = time.Now().Add(-time.Second)
	m.pending[token] = p
}

func TestMFAPendingTTLAndBudgetConstants(t *testing.T) {
	if mfaPendingTTL != 5*time.Minute {
		t.Fatalf("mfaPendingTTL = %v, want 5m", mfaPendingTTL)
	}
	if mfaPendingMaxFailures != 5 {
		t.Fatalf("mfaPendingMaxFailures = %d, want 5", mfaPendingMaxFailures)
	}
}
