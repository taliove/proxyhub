package server

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/taliove/proxyhub/internal/store"
)

// spyGuard records how often it ran and answers with a fixed verdict. Used to
// pin down the ordering rules of the chain without depending on a real guard.
type spyGuard struct {
	id      string
	verdict subGuardVerdict

	mu    sync.Mutex
	calls int
}

func (g *spyGuard) name() string { return g.id }

func (g *spyGuard) check(subGuardRequest) subGuardVerdict {
	g.mu.Lock()
	g.calls++
	g.mu.Unlock()
	return g.verdict
}

func (g *spyGuard) callCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.calls
}

// blockingSpy is a guard that would answer 429 if it were ever reached.
func blockingSpy(id string) *spyGuard {
	return &spyGuard{id: id, verdict: blockPull(store.PullStatusRateLimited, func(w http.ResponseWriter) {
		http.Error(w, "blocked by spy", http.StatusTooManyRequests)
	})}
}

// TestSubGuards_InvalidTokenSkipsGuards is the iron rule of the chain: a
// request that fails path lookup, token comparison or the enabled check never
// reaches a guard. It keeps getting the uniform 404, so no guard response
// (429/403) can leak to a caller without a valid token.
func TestSubGuards_InvalidTokenSkipsGuards(t *testing.T) {
	srv, st := newTestServer(t, pullLogNodes())
	spy := blockingSpy("spy")
	srv.subGuards = []subGuard{spy}
	h := srv.Handler()

	ep, _ := st.CreateEndpoint("守卫顺序设备")
	disabled, _ := st.CreateEndpoint("禁用设备")
	if err := st.SetEndpointEnabled(disabled.ID, false); err != nil {
		t.Fatalf("SetEndpointEnabled: %v", err)
	}

	cases := []struct {
		name string
		url  string
	}{
		{"unknown path", "/sub/nonexistent?token=x"},
		{"wrong token", "/sub/" + ep.Path + "?token=wrong"},
		{"missing token", "/sub/" + ep.Path},
		{"disabled endpoint", "/sub/" + disabled.Path + "?token=" + disabled.Token},
	}
	for _, c := range cases {
		req := httptest.NewRequest("GET", c.url, nil)
		req.RemoteAddr = "5.5.5.5:1111"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404 (body %s)", c.name, w.Code, w.Body.String())
		}
		if body := w.Body.String(); body != "404 page not found\n" {
			t.Errorf("%s: body = %q, want the stock 404 body", c.name, body)
		}
	}

	if got := spy.callCount(); got != 0 {
		t.Errorf("guard ran %d times for unauthenticated pulls, want 0", got)
	}
	// No guard status may be recorded for those attempts either.
	for _, id := range []int64{0, ep.ID, disabled.ID} {
		for ip, byStatus := range pullStatusesFor(t, st, id) {
			if byStatus[store.PullStatusRateLimited] != 0 {
				t.Errorf("endpoint %d ip %s got a rate_limited trace: %+v", id, ip, byStatus)
			}
		}
	}
}

// TestSubGuards_RunAfterValidTokenAndRecordStatus a valid pull does reach the
// chain, and a blocking verdict owns both the response and the pull_logs trace.
func TestSubGuards_RunAfterValidTokenAndRecordStatus(t *testing.T) {
	srv, st := newTestServer(t, pullLogNodes())
	spy := blockingSpy("spy")
	srv.subGuards = []subGuard{spy}
	h := srv.Handler()

	ep, _ := st.CreateEndpoint("有效 token 设备")
	req := httptest.NewRequest("GET", "/sub/"+ep.Path+"?token="+ep.Token, nil)
	req.RemoteAddr = "6.6.6.6:2222"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 (body %s)", w.Code, w.Body.String())
	}
	if got := spy.callCount(); got != 1 {
		t.Errorf("guard calls = %d, want 1", got)
	}
	got := pullStatusesFor(t, st, ep.ID)
	if got["6.6.6.6"][store.PullStatusRateLimited] != 1 {
		t.Errorf("pull statuses = %+v, want one rate_limited row", got)
	}
	if got["6.6.6.6"][store.PullStatusOK] != 0 {
		t.Errorf("blocked pull must not count as ok: %+v", got)
	}
}

// TestSubGuards_FirstBlockWinsAndStopsChain registration order is behaviour:
// the earliest blocking guard decides the status and later guards do not run.
func TestSubGuards_FirstBlockWinsAndStopsChain(t *testing.T) {
	srv, st := newTestServer(t, pullLogNodes())
	first := blockingSpy("first")
	second := blockingSpy("second")
	srv.subGuards = []subGuard{first, second}
	h := srv.Handler()

	ep, _ := st.CreateEndpoint("短路设备")
	req := httptest.NewRequest("GET", "/sub/"+ep.Path+"?token="+ep.Token, nil)
	req.RemoteAddr = "7.1.1.1:3333"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if got := first.callCount(); got != 1 {
		t.Errorf("first guard calls = %d, want 1", got)
	}
	if got := second.callCount(); got != 0 {
		t.Errorf("second guard ran after a block: calls = %d, want 0", got)
	}
	got := pullStatusesFor(t, st, ep.ID)
	if got["7.1.1.1"][store.PullStatusRateLimited] != 1 {
		t.Errorf("want exactly one status row from the first guard: %+v", got)
	}
}

// TestSubGuards_ObserveVerdictRecordsButServes the dry-run shape ticket 07's
// observe mode needs: a trace is written, the payload is still delivered.
func TestSubGuards_ObserveVerdictRecordsButServes(t *testing.T) {
	srv, st := newTestServer(t, pullLogNodes())
	srv.subGuards = []subGuard{
		&spyGuard{id: "observer", verdict: observePull(store.PullStatusGeoWouldBlock)},
	}
	h := srv.Handler()

	ep, _ := st.CreateEndpoint("观察模式设备")
	req := httptest.NewRequest("GET", "/sub/"+ep.Path+"?token="+ep.Token, nil)
	req.RemoteAddr = "8.1.1.1:4444"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
	got := pullStatusesFor(t, st, ep.ID)
	if got["8.1.1.1"][store.PullStatusGeoWouldBlock] != 1 {
		t.Errorf("want a geo_would_block trace: %+v", got)
	}
	if got["8.1.1.1"][store.PullStatusOK] != 1 {
		t.Errorf("observed pull must still be delivered and counted ok: %+v", got)
	}
}

// TestSubGuards_NilResponderFallsBackTo404 a guard that does not want to reveal
// itself omits respond and gets the uniform 404.
func TestSubGuards_NilResponderFallsBackTo404(t *testing.T) {
	srv, st := newTestServer(t, pullLogNodes())
	srv.subGuards = []subGuard{
		&spyGuard{id: "silent", verdict: blockPull(store.PullStatusBlacklisted, nil)},
	}
	h := srv.Handler()

	ep, _ := st.CreateEndpoint("静默拦截设备")
	req := httptest.NewRequest("GET", "/sub/"+ep.Path+"?token="+ep.Token, nil)
	req.RemoteAddr = "9.1.1.1:5555"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	if body := w.Body.String(); body != "404 page not found\n" {
		t.Errorf("body = %q, want the stock 404 body", body)
	}
	got := pullStatusesFor(t, st, ep.ID)
	if got["9.1.1.1"][store.PullStatusBlacklisted] != 1 {
		t.Errorf("want a blacklisted trace: %+v", got)
	}
}

// TestSubGuardChain_DefaultRegistration the shipped chain carries the guards a
// fresh server must enforce without extra wiring, in the order that decides
// which reason a blocked pull is recorded under. geo sits ahead of the rate
// limit so a pull from a disallowed location is recorded as geo_blocked instead
// of having that reason masked by rate_limited once the client starts hammering.
func TestSubGuardChain_DefaultRegistration(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	names := make([]string, 0, len(srv.subGuards))
	for _, g := range srv.subGuards {
		names = append(names, g.name())
	}
	for _, want := range []string{"geo_allowlist", "rate_limit"} {
		if indexOfGuard(names, want) < 0 {
			t.Errorf("guard %q missing from the default chain %v", want, names)
		}
	}
	if geo, rate := indexOfGuard(names, "geo_allowlist"), indexOfGuard(names, "rate_limit"); geo >= 0 && rate >= 0 && geo > rate {
		t.Errorf("chain order = %v, want geo_allowlist before rate_limit", names)
	}
}

// indexOfGuard returns the position of name in names, or -1.
func indexOfGuard(names []string, name string) int {
	for i, n := range names {
		if n == name {
			return i
		}
	}
	return -1
}
