package server

import (
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/taliove/proxyhub/internal/store"
)

const (
	// pullRateWindow is the sliding window the pull rate limit is expressed
	// in. The setting key is "per hour", so the window is one hour.
	pullRateWindow = time.Hour
	// defaultPullRateLimitPerHour is the shipped threshold: 60 pulls per hour
	// per (IP, subscription address). Clients are told to refresh hourly
	// (Profile-Update-Interval: 1), so this leaves ample room for manual
	// refreshes while cutting off scripted hammering.
	defaultPullRateLimitPerHour = 60
	// pullRateSweepEvery is how often (in checks) expired keys are dropped.
	// Cheap enough to do inline, which keeps the limiter free of background
	// goroutines. This reclaims dead keys but is NOT the memory bound - keys
	// created inside the live window are all still live, so the bound is
	// pullRateMaxKeys below.
	pullRateSweepEvery = 256
	// pullRateMaxKeys is the hard cap on tracked (IP, address) pairs. Without
	// it, one valid path+token pair plus a large source range (an IPv6 /64, or
	// spoofed X-Forwarded-For behind the trusted loopback proxy) would let a
	// client mint unbounded live keys inside a single window - the limiter
	// would amplify the abuse it exists to absorb. At the cap the oldest keys
	// are evicted, which can hand a slice of quota back to whoever gets
	// evicted; that is the accepted tradeoff of a bounded in-memory limiter
	// (the limit is best-effort burst protection, not an accounting ledger).
	pullRateMaxKeys = 100_000
	// pullRateEvictBatch is how many keys one over-cap insert evicts, so the
	// O(n) eviction scan is amortised over many inserts instead of running on
	// every request once the cap is reached.
	pullRateEvictBatch = 1024
	// pullRateThresholdTTL caches the threshold setting for this long. The
	// guard runs on the /sub hot path and the settings read is a full-table
	// SELECT on the single shared SQLite connection, so reading it per request
	// would make the rejected-pull path the most expensive path in the server.
	// Operator changes take effect within this window.
	pullRateThresholdTTL = 5 * time.Second
)

// pullRateLimiter is an in-memory sliding-window counter keyed by
// (client IP, subscription address). State is deliberately memory-only: the
// limit protects against bursts, so losing counters on restart is acceptable
// (transient semantics, ticket 04).
//
// Safe for concurrent use: every operation takes the mutex, and the whole
// prune-decide-append sequence happens under it so two simultaneous requests
// can never both be admitted as the Nth.
type pullRateLimiter struct {
	mu sync.Mutex
	// hits maps key -> timestamps of admitted pulls inside the window,
	// oldest first.
	hits   map[string][]time.Time
	checks int
	// now is the clock seam; tests drive the window without sleeping.
	now func() time.Time
}

// newPullRateLimiter creates an empty limiter on the wall clock.
func newPullRateLimiter() *pullRateLimiter {
	return &pullRateLimiter{hits: make(map[string][]time.Time), now: time.Now}
}

// pullRateKey scopes a counter to one IP on one subscription address. The two
// dimensions are independent on purpose: one client hammering one address must
// not throttle its pulls of another address.
func pullRateKey(ip string, endpointID int64) string {
	return ip + "|" + strconv.FormatInt(endpointID, 10)
}

// allow books one pull attempt against key and reports whether it is admitted.
// threshold <= 0 disables the limit entirely and drops all accumulated state,
// so re-enabling the limit really does start from a clean window (and an
// operator who disables it to relieve memory pressure gets the memory back).
//
// retryAfter is the time the caller should wait before the window has room
// again, and is only meaningful when admitted is false. Rejected attempts are
// not appended to the window: a client that keeps hammering while limited must
// not push its own recovery time further out.
func (l *pullRateLimiter) allow(key string, threshold int) (admitted bool, retryAfter time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if threshold <= 0 {
		if len(l.hits) > 0 {
			l.hits = make(map[string][]time.Time)
		}
		return true, 0
	}

	now := l.now()
	l.checks++
	if l.checks%pullRateSweepEvery == 0 {
		l.sweepLocked(now)
	}

	window := pruneBefore(l.hits[key], now.Add(-pullRateWindow))
	if len(window) >= threshold {
		l.hits[key] = window
		// Room opens when the hit that is `threshold` slots from the end
		// leaves the window - not when the oldest one does. The two differ
		// after an operator lowers the threshold mid-window, and pointing at
		// the oldest hit there would promise a retry time that is still
		// rejected on arrival.
		return false, retryAfterFor(window[len(window)-threshold], now)
	}

	if _, tracked := l.hits[key]; !tracked {
		l.admitNewKeyLocked(now)
	}
	l.hits[key] = append(window, now)
	return true, 0
}

// admitNewKeyLocked makes room for one new key when the map is at capacity: it
// first reclaims dead keys, and only if that is not enough evicts a batch of
// the least recently active ones. Callers must hold the mutex.
func (l *pullRateLimiter) admitNewKeyLocked(now time.Time) {
	if len(l.hits) < pullRateMaxKeys {
		return
	}
	l.sweepLocked(now)
	if len(l.hits) < pullRateMaxKeys {
		return
	}
	l.evictOldestLocked(pullRateEvictBatch)
}

// evictOldestLocked removes up to n keys, preferring the least recently active.
// Callers must hold the mutex.
//
// Approximate by sampling (Redis-style) rather than sorting the whole map: this
// runs while holding the lock that every /sub pull needs, during exactly the
// flood the cap exists to survive. Sorting 100k keys there would allocate
// megabytes and stall every concurrent pull for the duration. Sampling makes
// the cost O(n * sample) with no large allocation, at the price of evicting
// something merely old rather than provably the oldest - fine, because the
// limit is best-effort burst protection.
func (l *pullRateLimiter) evictOldestLocked(n int) {
	const sample = 8
	for ; n > 0; n-- {
		var (
			victim string
			oldest time.Time
			seen   int
		)
		// Go randomises map iteration order, so this is a fresh sample per
		// round without tracking any cursor.
		for key, hits := range l.hits {
			// Empty slices are not a reachable state (both allow paths store a
			// non-empty window, sweepLocked deletes instead of emptying); this
			// only keeps the indexing below from panicking if that ever changes.
			if len(hits) == 0 {
				victim = key
				break
			}
			if last := hits[len(hits)-1]; victim == "" || last.Before(oldest) {
				victim, oldest = key, last
			}
			if seen++; seen >= sample {
				break
			}
		}
		if victim == "" {
			return
		}
		delete(l.hits, victim)
	}
}

// retryAfterFor is how long until oldest falls out of the window, rounded up to
// a whole second and never below one second (a "Retry-After: 0" invites an
// immediate retry that would just be rejected again).
func retryAfterFor(oldest, now time.Time) time.Duration {
	remaining := oldest.Add(pullRateWindow).Sub(now)
	if remaining < time.Second {
		return time.Second
	}
	return time.Duration(math.Ceil(remaining.Seconds())) * time.Second
}

// pruneBefore returns the suffix of hits strictly after cutoff. hits is ordered
// oldest first, so this is a prefix drop; the result aliases the input slice
// rather than copying, which is safe because the caller immediately stores it
// back under the same key.
//
// The boundary is exclusive on purpose: a hit exactly one window old has left
// the window. Keeping it would make retryAfterFor off by one tick - a client
// that waits exactly as long as it was told would be rejected again.
func pruneBefore(hits []time.Time, cutoff time.Time) []time.Time {
	i := 0
	for i < len(hits) && !hits[i].After(cutoff) {
		i++
	}
	return hits[i:]
}

// sweepLocked reclaims keys whose window has fully expired. This is not the
// memory bound (keys created inside the live window are all still live) - see
// pullRateMaxKeys and admitNewKeyLocked for that. Callers must hold the mutex.
func (l *pullRateLimiter) sweepLocked(now time.Time) {
	cutoff := now.Add(-pullRateWindow)
	for key, hits := range l.hits {
		if live := pruneBefore(hits, cutoff); len(live) == 0 {
			delete(l.hits, key)
		} else {
			l.hits[key] = live
		}
	}
}

// rateLimitGuard is the first guard of the chain (see subguard.go): it caps how
// often one IP may pull one subscription address.
//
// The threshold is resolved per request through threshold(), not captured at
// construction, so an operator changing the setting takes effect without a
// restart. In production that function is a cachedThreshold read (bounded by
// pullRateThresholdTTL, invalidated on settings save) rather than a DB query -
// the chain runs on every pull, rejected ones included.
type rateLimitGuard struct {
	limiter   *pullRateLimiter
	threshold func() int
}

// newRateLimitGuard wires the guard to a limiter and a threshold source.
func newRateLimitGuard(limiter *pullRateLimiter, threshold func() int) *rateLimitGuard {
	return &rateLimitGuard{limiter: limiter, threshold: threshold}
}

func (g *rateLimitGuard) name() string { return "rate_limit" }

// check admits the pull unless the (IP, address) window is full. Over the
// limit the client gets 429 with a Retry-After header - unlike the other
// guards this one deliberately does not hide behind the uniform 404, because
// the request holds a valid token and a well-behaved client needs to learn to
// back off.
//
// Note that an admitted pull books its slot here, before generation, so an
// attempt that then fails on the server side (503 empty pool, 500 render
// error) still costs quota. Kept deliberately, for cost not correctness: a
// refund path (drop the last hit for that key under the same mutex) would work
// and would not race, it is just more moving parts than a burst limit needs.
// What must NOT be done is splitting check from book - that would let
// concurrent requests all pass the check before any is booked, the exact race
// the single locked prune-decide-append prevents. 60/hour leaves a client room
// to ride out a transient pool outage.
func (g *rateLimitGuard) check(gr subGuardRequest) subGuardVerdict {
	admitted, retryAfter := g.limiter.allow(pullRateKey(gr.ip, gr.ep.ID), g.threshold())
	if admitted {
		return allowPull()
	}
	return blockPull(store.PullStatusRateLimited, func(w http.ResponseWriter) {
		w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())))
		http.Error(w, "too many pulls, slow down", http.StatusTooManyRequests)
	})
}

// pullRateLimitPerHour resolves the pull rate limit threshold from the super
// admin settings, falling back to the shipped default. Same fallback chain as
// loadSecurityPolicy (whose securityPolicy carries the parsed value), so both
// read one source of truth. 0 means the limit is off.
//
// Uncached: the guard reads through cachedThreshold instead (see
// pullRateThresholdTTL), because this hits the DB.
func (s *Server) pullRateLimitPerHour() int {
	return s.loadSecurityPolicy().PullRateLimitPerHour
}

// cachedThreshold memoises an int setting for a TTL. It exists so a guard on
// the /sub hot path can honour live setting changes without paying a DB read
// per request - including per *rejected* request, which is the path an abusive
// client controls.
type cachedThreshold struct {
	mu     sync.Mutex
	load   func() int
	ttl    time.Duration
	now    func() time.Time
	value  int
	loaded time.Time
}

// newCachedThreshold wraps load with a ttl-bounded cache on the wall clock.
func newCachedThreshold(ttl time.Duration, load func() int) *cachedThreshold {
	return &cachedThreshold{load: load, ttl: ttl, now: time.Now}
}

// get returns the cached value, refreshing it when the TTL has elapsed. The
// load call happens under the mutex so a burst of concurrent misses collapses
// into one DB read rather than one per goroutine.
func (c *cachedThreshold) get() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	if c.loaded.IsZero() || now.Sub(c.loaded) >= c.ttl {
		c.value = c.load()
		c.loaded = now
	}
	return c.value
}

// invalidate forces the next get to reload. Called when settings are saved so
// an operator change is visible immediately instead of after the TTL.
func (c *cachedThreshold) invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.loaded = time.Time{}
}
