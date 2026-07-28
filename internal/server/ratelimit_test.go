package server

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/taliove/proxyhub/internal/store"
)

// pullOnce fires one authenticated pull from ip against ep.
func pullOnce(t *testing.T, h http.Handler, ep *store.Endpoint, ip string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", "/sub/"+ep.Path+"?token="+ep.Token, nil)
	req.RemoteAddr = ip + ":12345"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// TestRateLimit_DefaultThresholdTripsOn61st the shipped default is 60 pulls per
// hour per (IP, address): the first 60 are served, the 61st gets 429 with a
// Retry-After header and a rate_limited trace.
func TestRateLimit_DefaultThresholdTripsOn61st(t *testing.T) {
	srv, st := newTestServer(t, pullLogNodes())
	h := srv.Handler()
	ep, _ := st.CreateEndpoint("限频设备")

	for i := 1; i <= defaultPullRateLimitPerHour; i++ {
		if w := pullOnce(t, h, ep, "10.0.0.1"); w.Code != http.StatusOK {
			t.Fatalf("pull %d: status = %d, want 200 (body %s)", i, w.Code, w.Body.String())
		}
	}

	w := pullOnce(t, h, ep, "10.0.0.1")
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("pull 61: status = %d, want 429 (body %s)", w.Code, w.Body.String())
	}
	retryAfter := w.Header().Get("Retry-After")
	if retryAfter == "" {
		t.Fatal("429 response has no Retry-After header")
	}
	secs, err := strconv.Atoi(retryAfter)
	if err != nil {
		t.Fatalf("Retry-After = %q, want an integer number of seconds", retryAfter)
	}
	if secs <= 0 || secs > int(pullRateWindow.Seconds()) {
		t.Errorf("Retry-After = %d, want within (0, %d]", secs, int(pullRateWindow.Seconds()))
	}

	got := pullStatusesFor(t, st, ep.ID)
	if got["10.0.0.1"][store.PullStatusOK] != defaultPullRateLimitPerHour {
		t.Errorf("ok rows = %d, want %d (all: %+v)",
			got["10.0.0.1"][store.PullStatusOK], defaultPullRateLimitPerHour, got)
	}
	if got["10.0.0.1"][store.PullStatusRateLimited] != 1 {
		t.Errorf("rate_limited rows = %d, want 1 (all: %+v)",
			got["10.0.0.1"][store.PullStatusRateLimited], got)
	}
}

// TestRateLimit_ScopedPerEndpoint one IP exhausting one address must not
// throttle its pulls of another address.
func TestRateLimit_ScopedPerEndpoint(t *testing.T) {
	srv, st := newTestServer(t, pullLogNodes())
	if err := st.SaveSystemSettings(map[string]string{"pull_rate_limit_per_hour": "2"}); err != nil {
		t.Fatalf("SaveSystemSettings: %v", err)
	}
	h := srv.Handler()
	first, _ := st.CreateEndpoint("地址甲")
	second, _ := st.CreateEndpoint("地址乙")

	for i := 1; i <= 2; i++ {
		if w := pullOnce(t, h, first, "10.0.0.2"); w.Code != http.StatusOK {
			t.Fatalf("first address pull %d: status = %d", i, w.Code)
		}
	}
	if w := pullOnce(t, h, first, "10.0.0.2"); w.Code != http.StatusTooManyRequests {
		t.Fatalf("first address pull 3: status = %d, want 429", w.Code)
	}
	// Same IP, different subscription address: independent window.
	if w := pullOnce(t, h, second, "10.0.0.2"); w.Code != http.StatusOK {
		t.Errorf("second address pull: status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
	// Different IP, exhausted address: independent window too.
	if w := pullOnce(t, h, first, "10.0.0.3"); w.Code != http.StatusOK {
		t.Errorf("other ip pull: status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
}

// TestRateLimit_ThresholdZeroDisables 0 means the limit is off: well past the
// default threshold every pull is still served and nothing is recorded as
// rate_limited.
func TestRateLimit_ThresholdZeroDisables(t *testing.T) {
	srv, st := newTestServer(t, pullLogNodes())
	if err := st.SaveSystemSettings(map[string]string{"pull_rate_limit_per_hour": "0"}); err != nil {
		t.Fatalf("SaveSystemSettings: %v", err)
	}
	h := srv.Handler()
	ep, _ := st.CreateEndpoint("不限频设备")

	for i := 1; i <= defaultPullRateLimitPerHour+5; i++ {
		if w := pullOnce(t, h, ep, "10.0.0.4"); w.Code != http.StatusOK {
			t.Fatalf("pull %d: status = %d, want 200 (limit should be off)", i, w.Code)
		}
	}
	got := pullStatusesFor(t, st, ep.ID)
	if got["10.0.0.4"][store.PullStatusRateLimited] != 0 {
		t.Errorf("rate_limited rows = %d, want 0 with the limit off (all: %+v)",
			got["10.0.0.4"][store.PullStatusRateLimited], got)
	}
}

// TestRateLimit_ThresholdFromSettings the threshold is read per request, so an
// operator change takes effect on the next pull without a restart.
func TestRateLimit_ThresholdFromSettings(t *testing.T) {
	srv, st := newTestServer(t, pullLogNodes())
	h := srv.Handler()
	ep, _ := st.CreateEndpoint("阈值设备")

	if got := srv.pullRateLimitPerHour(); got != defaultPullRateLimitPerHour {
		t.Fatalf("default threshold = %d, want %d", got, defaultPullRateLimitPerHour)
	}
	if err := st.SaveSystemSettings(map[string]string{"pull_rate_limit_per_hour": "1"}); err != nil {
		t.Fatalf("SaveSystemSettings: %v", err)
	}
	if got := srv.pullRateLimitPerHour(); got != 1 {
		t.Fatalf("threshold after save = %d, want 1", got)
	}
	// The guard reads a cached copy; the settings save path invalidates it.
	srv.pullRateThreshold.invalidate()
	if w := pullOnce(t, h, ep, "10.0.0.5"); w.Code != http.StatusOK {
		t.Fatalf("pull 1: status = %d, want 200", w.Code)
	}
	if w := pullOnce(t, h, ep, "10.0.0.5"); w.Code != http.StatusTooManyRequests {
		t.Errorf("pull 2: status = %d, want 429 under threshold 1", w.Code)
	}
}

// TestRateLimit_InvalidSettingKeepsDefault a garbled or negative setting must
// not silently disable the guard.
func TestRateLimit_InvalidSettingKeepsDefault(t *testing.T) {
	srv, st := newTestServer(t, nil)
	for _, v := range []string{"abc", "-1", " "} {
		if err := st.SaveSystemSettings(map[string]string{"pull_rate_limit_per_hour": v}); err != nil {
			t.Fatalf("SaveSystemSettings(%q): %v", v, err)
		}
		if got := srv.pullRateLimitPerHour(); got != defaultPullRateLimitPerHour {
			t.Errorf("setting %q -> threshold %d, want the default %d",
				v, got, defaultPullRateLimitPerHour)
		}
	}
}

// TestPullRateLimiter_WindowSlides once the oldest hit leaves the window the
// client gets its quota back.
func TestPullRateLimiter_WindowSlides(t *testing.T) {
	now := time.Date(2026, 7, 28, 10, 0, 0, 0, time.UTC)
	l := newPullRateLimiter()
	l.now = func() time.Time { return now }

	// Two hits ten minutes apart, so they leave the window one at a time.
	key := pullRateKey("1.2.3.4", 7)
	if ok, _ := l.allow(key, 2); !ok {
		t.Fatal("hit 1 rejected inside quota")
	}
	now = now.Add(10 * time.Minute)
	if ok, _ := l.allow(key, 2); !ok {
		t.Fatal("hit 2 rejected inside quota")
	}

	ok, retryAfter := l.allow(key, 2)
	if ok {
		t.Fatal("hit 3 admitted over quota")
	}
	if want := pullRateWindow - 10*time.Minute; retryAfter != want {
		t.Errorf("retryAfter = %v, want %v (until hit 1 leaves the window)", retryAfter, want)
	}

	// Still inside the window: rejected, and the rejection did not extend it.
	now = now.Add(pullRateWindow - 11*time.Minute)
	if ok, retryAfter = l.allow(key, 2); ok {
		t.Error("hit inside the window admitted")
	} else if retryAfter != time.Minute {
		t.Errorf("retryAfter = %v, want 1m", retryAfter)
	}

	// Hit 1 falls out: room for exactly one more, hit 2 still holds a slot.
	now = now.Add(time.Minute + time.Second)
	if ok, _ = l.allow(key, 2); !ok {
		t.Error("hit after the window slid was rejected")
	}
	if ok, _ = l.allow(key, 2); ok {
		t.Error("second hit after the slide admitted over quota")
	}
}

// TestPullRateLimiter_RetryAfterAtLeastOneSecond never hand out
// "Retry-After: 0", which would invite an immediate rejected retry.
func TestPullRateLimiter_RetryAfterAtLeastOneSecond(t *testing.T) {
	now := time.Date(2026, 7, 28, 10, 0, 0, 0, time.UTC)
	l := newPullRateLimiter()
	l.now = func() time.Time { return now }

	key := pullRateKey("1.2.3.4", 1)
	if ok, _ := l.allow(key, 1); !ok {
		t.Fatal("first hit rejected")
	}
	// 1ms before the window expires: rounding must floor at one second.
	now = now.Add(pullRateWindow - time.Millisecond)
	ok, retryAfter := l.allow(key, 1)
	if ok {
		t.Fatal("hit admitted before the window expired")
	}
	if retryAfter != time.Second {
		t.Errorf("retryAfter = %v, want 1s", retryAfter)
	}
}

// TestPullRateLimiter_ConcurrentAdmitsExactlyThreshold the window is
// concurrency safe: with N goroutines racing on one key and threshold T, exactly
// T are admitted (run with -race).
func TestPullRateLimiter_ConcurrentAdmitsExactlyThreshold(t *testing.T) {
	const (
		goroutines = 200
		threshold  = 50
	)
	l := newPullRateLimiter()
	key := pullRateKey("1.2.3.4", 42)

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		admitted int
	)
	start := make(chan struct{})
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if ok, _ := l.allow(key, threshold); ok {
				mu.Lock()
				admitted++
				mu.Unlock()
			}
		}()
	}
	close(start)
	wg.Wait()

	if admitted != threshold {
		t.Errorf("admitted = %d, want exactly %d", admitted, threshold)
	}
}

// TestPullRateLimiter_ConcurrentDistinctKeys parallel traffic across many
// (IP, address) pairs stays independent and race free.
func TestPullRateLimiter_ConcurrentDistinctKeys(t *testing.T) {
	l := newPullRateLimiter()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := pullRateKey("10.1."+strconv.Itoa(i/256)+"."+strconv.Itoa(i%256), int64(i))
			for j := 0; j < 3; j++ {
				if ok, _ := l.allow(key, 3); !ok {
					t.Errorf("key %s hit %d rejected inside its own quota", key, j+1)
				}
			}
		}(i)
	}
	wg.Wait()
}

// TestPullRateLimiter_RetryAfterAfterThresholdLowered the threshold is read per
// request, so a window can hold more hits than the current threshold. Retry-After
// must then point at the hit that actually frees a slot, not at the oldest one -
// otherwise the client retries on schedule and is rejected again.
func TestPullRateLimiter_RetryAfterAfterThresholdLowered(t *testing.T) {
	now := time.Date(2026, 7, 28, 10, 0, 0, 0, time.UTC)
	l := newPullRateLimiter()
	l.now = func() time.Time { return now }
	key := pullRateKey("1.2.3.4", 9)

	// Three hits at 0, 10 and 20 minutes under a generous threshold.
	for i := 0; i < 3; i++ {
		if ok, _ := l.allow(key, 10); !ok {
			t.Fatalf("hit %d rejected under threshold 10", i+1)
		}
		now = now.Add(10 * time.Minute)
	}
	// now = 30min. Operator lowers the threshold to 2: the window holds 3 hits,
	// so a slot only opens when the second hit (at 10min) expires.
	ok, retryAfter := l.allow(key, 2)
	if ok {
		t.Fatal("hit admitted with 3 hits in the window under threshold 2")
	}
	want := pullRateWindow - 20*time.Minute // hit at 10min expires at 70min, now is 30min
	if retryAfter != want {
		t.Fatalf("retryAfter = %v, want %v (until the 2nd hit expires)", retryAfter, want)
	}

	// Honouring that Retry-After must actually get the client in.
	now = now.Add(retryAfter)
	if ok, retryAfter = l.allow(key, 2); !ok {
		t.Errorf("still rejected after honouring Retry-After (next wait %v)", retryAfter)
	}
}

// TestPullRateLimiter_ThresholdZeroClearsState disabling the limit drops the
// accumulated windows, so re-enabling starts clean and an operator disabling it
// to relieve memory pressure gets the memory back.
func TestPullRateLimiter_ThresholdZeroClearsState(t *testing.T) {
	l := newPullRateLimiter()
	key := pullRateKey("1.2.3.4", 3)
	if ok, _ := l.allow(key, 1); !ok {
		t.Fatal("first hit rejected")
	}
	if ok, _ := l.allow(key, 1); ok {
		t.Fatal("second hit admitted over threshold 1")
	}

	if ok, _ := l.allow(key, 0); !ok {
		t.Fatal("hit rejected with the limit off")
	}
	l.mu.Lock()
	size := len(l.hits)
	l.mu.Unlock()
	if size != 0 {
		t.Errorf("limiter holds %d keys with the limit off, want 0", size)
	}
	// Re-enabled: the previous window is gone, so the client gets a full quota.
	if ok, _ := l.allow(key, 1); !ok {
		t.Error("hit rejected after re-enabling, want a clean window")
	}
}

// TestPullRateLimiter_KeyCountIsBounded distinct keys created inside one window
// are all live, so expiry alone is no bound: the cap has to hold.
func TestPullRateLimiter_KeyCountIsBounded(t *testing.T) {
	now := time.Date(2026, 7, 28, 10, 0, 0, 0, time.UTC)
	l := newPullRateLimiter()
	l.now = func() time.Time { return now }

	// All hits land inside one window (no expiry helps here), from far more
	// distinct sources than the cap allows.
	for i := 0; i < pullRateMaxKeys+pullRateEvictBatch*3; i++ {
		l.allow(pullRateKey("10.3."+strconv.Itoa(i/256)+"."+strconv.Itoa(i%256), int64(i)), 10)
		now = now.Add(time.Millisecond)
	}

	l.mu.Lock()
	size := len(l.hits)
	l.mu.Unlock()
	if size > pullRateMaxKeys {
		t.Errorf("limiter holds %d keys, want at most the cap %d", size, pullRateMaxKeys)
	}
	if size == 0 {
		t.Error("eviction emptied the limiter entirely")
	}
}

// TestPullRateLimiter_EvictionPrefersStaleKeys eviction is sampled, not an exact
// LRU, so the contract is statistical: evicting most of the map must clear out
// the long-idle keys well ahead of the ones active right now.
func TestPullRateLimiter_EvictionPrefersStaleKeys(t *testing.T) {
	now := time.Date(2026, 7, 28, 10, 0, 0, 0, time.UTC)
	l := newPullRateLimiter()
	l.now = func() time.Time { return now }

	// 50 keys idle since the start of the window, then 50 active keys.
	var stale, fresh []string
	for i := 0; i < 50; i++ {
		k := pullRateKey("10.4.0."+strconv.Itoa(i), int64(i))
		stale = append(stale, k)
		l.allow(k, 10)
	}
	now = now.Add(30 * time.Minute)
	for i := 0; i < 50; i++ {
		k := pullRateKey("10.4.1."+strconv.Itoa(i), int64(100+i))
		fresh = append(fresh, k)
		l.allow(k, 10)
	}

	// Evict half the map; the stale half should absorb most of it.
	l.mu.Lock()
	l.evictOldestLocked(50)
	staleLeft, freshLeft := 0, 0
	for _, k := range stale {
		if _, ok := l.hits[k]; ok {
			staleLeft++
		}
	}
	for _, k := range fresh {
		if _, ok := l.hits[k]; ok {
			freshLeft++
		}
	}
	l.mu.Unlock()

	if staleLeft >= freshLeft {
		t.Errorf("eviction did not prefer stale keys: %d stale vs %d fresh survived",
			staleLeft, freshLeft)
	}
	if freshLeft < 25 {
		t.Errorf("only %d of 50 active keys survived, want most of them", freshLeft)
	}
}

// TestPullRateLimiter_EvictionTerminatesOnSmallMap evicting more keys than exist
// must stop instead of spinning.
func TestPullRateLimiter_EvictionTerminatesOnSmallMap(t *testing.T) {
	l := newPullRateLimiter()
	l.allow(pullRateKey("10.4.2.1", 1), 10)
	l.mu.Lock()
	l.evictOldestLocked(pullRateEvictBatch)
	size := len(l.hits)
	l.mu.Unlock()
	if size != 0 {
		t.Errorf("limiter holds %d keys after over-evicting, want 0", size)
	}
}

// TestCachedThreshold_ReadsOncePerTTL the guard must not hit the DB per pull:
// the value is cached for the TTL and refreshed after it.
func TestCachedThreshold_ReadsOncePerTTL(t *testing.T) {
	now := time.Date(2026, 7, 28, 10, 0, 0, 0, time.UTC)
	loads := 0
	value := 60
	c := newCachedThreshold(pullRateThresholdTTL, func() int {
		loads++
		return value
	})
	c.now = func() time.Time { return now }

	for i := 0; i < 100; i++ {
		if got := c.get(); got != 60 {
			t.Fatalf("get() = %d, want 60", got)
		}
	}
	if loads != 1 {
		t.Errorf("loads = %d within the TTL, want 1", loads)
	}

	// A change is invisible until the TTL elapses...
	value = 5
	if got := c.get(); got != 60 {
		t.Errorf("get() = %d before TTL, want the cached 60", got)
	}
	now = now.Add(pullRateThresholdTTL)
	if got := c.get(); got != 5 {
		t.Errorf("get() = %d after TTL, want the refreshed 5", got)
	}
	// ...or until an explicit invalidate (what the settings save path does).
	value = 7
	c.invalidate()
	if got := c.get(); got != 7 {
		t.Errorf("get() = %d after invalidate, want 7", got)
	}
}

// TestPullRateLimiter_SweepDropsExpiredKeys the map must not grow without
// bound when many distinct IPs pull once each.
func TestPullRateLimiter_SweepDropsExpiredKeys(t *testing.T) {
	now := time.Date(2026, 7, 28, 10, 0, 0, 0, time.UTC)
	l := newPullRateLimiter()
	l.now = func() time.Time { return now }

	for i := 0; i < pullRateSweepEvery; i++ {
		l.allow(pullRateKey("10.2.0."+strconv.Itoa(i), 1), 10)
	}
	// Move past the window, then trip the sweep with one more batch of checks.
	now = now.Add(pullRateWindow + time.Second)
	live := pullRateKey("10.9.9.9", 1)
	for i := 0; i < pullRateSweepEvery; i++ {
		l.allow(live, 1000)
	}

	l.mu.Lock()
	size := len(l.hits)
	l.mu.Unlock()
	if size != 1 {
		t.Errorf("limiter holds %d keys after the sweep, want 1 (only the live key)", size)
	}
}
