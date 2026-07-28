package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/taliove/proxyhub/internal/geoip"
	"github.com/taliove/proxyhub/internal/store"
)

// newGeoTestServer builds a server whose geo guard resolves addresses from a
// fixed table, then rebuilds the chain so the override is the one in use.
// countryLookup is a Server field, so it must be replaced before the rebuild.
func newGeoTestServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	srv, st := newTestServer(t, pullLogNodes())
	srv.countryLookup = func(ip string) (string, error) {
		if f, ok := geoTable[ip]; ok {
			return f.country, nil
		}
		return "", geoip.ErrCountryNotFound
	}
	srv.subGuards = srv.newSubGuardChain()
	return srv, st
}

// pullFrom performs one authenticated pull from ip.
func pullFrom(t *testing.T, h http.Handler, ep *store.Endpoint, ip string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", "/sub/"+ep.Path+"?token="+ep.Token, nil)
	req.RemoteAddr = ip + ":41000"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// TestGeoPull_OffModeServesEveryone the default address is untouched by the
// guard: this is the compatibility guarantee for existing subscriptions.
func TestGeoPull_OffModeServesEveryone(t *testing.T) {
	srv, st := newGeoTestServer(t)
	h := srv.Handler()
	ep, _ := st.CreateEndpoint("地域关闭设备")

	if w := pullFrom(t, h, ep, "8.8.8.8"); w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
	got := pullStatusesFor(t, st, ep.ID)
	if got["8.8.8.8"][store.PullStatusOK] != 1 {
		t.Errorf("statuses = %+v, want one ok row", got)
	}
	if got["8.8.8.8"][store.PullStatusGeoWouldBlock] != 0 || got["8.8.8.8"][store.PullStatusGeoBlocked] != 0 {
		t.Errorf("off mode left a geo trace: %+v", got)
	}
}

// TestGeoPull_ObserveRecordsAndStillServes the dry run: the payload is
// delivered and a geo_would_block row lands next to the ok row, which is how an
// operator measures a rule before enforcing it.
func TestGeoPull_ObserveRecordsAndStillServes(t *testing.T) {
	srv, st := newGeoTestServer(t)
	h := srv.Handler()
	ep, _ := st.CreateEndpoint("地域观察设备")
	if err := st.UpdateEndpointGeoConfig(ep.ID, store.GeoModeObserve, "CN", ""); err != nil {
		t.Fatalf("UpdateEndpointGeoConfig: %v", err)
	}

	if w := pullFrom(t, h, ep, "8.8.8.8"); w.Code != http.StatusOK {
		t.Fatalf("miss under observe: status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
	if w := pullFrom(t, h, ep, "1.2.4.8"); w.Code != http.StatusOK {
		t.Fatalf("hit under observe: status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}

	got := pullStatusesFor(t, st, ep.ID)
	if got["8.8.8.8"][store.PullStatusGeoWouldBlock] != 1 {
		t.Errorf("want one geo_would_block for the foreign ip: %+v", got)
	}
	if got["8.8.8.8"][store.PullStatusOK] != 1 {
		t.Errorf("observed pull must still be delivered: %+v", got)
	}
	if got["1.2.4.8"][store.PullStatusGeoWouldBlock] != 0 {
		t.Errorf("allowed ip must leave no would_block trace: %+v", got)
	}
	if got["1.2.4.8"][store.PullStatusOK] != 1 {
		t.Errorf("allowed ip must be served: %+v", got)
	}
}

// TestGeoPull_EnforceBlocksMissServesHit the enforced address answers 403 to a
// location outside the list and 200 to one on it, with the matching traces.
func TestGeoPull_EnforceBlocksMissServesHit(t *testing.T) {
	srv, st := newGeoTestServer(t)
	h := srv.Handler()
	ep, _ := st.CreateEndpoint("地域强制设备")
	if err := st.UpdateEndpointGeoConfig(ep.ID, store.GeoModeEnforce, "CN", ""); err != nil {
		t.Fatalf("UpdateEndpointGeoConfig: %v", err)
	}

	if w := pullFrom(t, h, ep, "8.8.8.8"); w.Code != http.StatusForbidden {
		t.Fatalf("miss: status = %d, want 403 (body %s)", w.Code, w.Body.String())
	}
	if w := pullFrom(t, h, ep, "1.2.4.8"); w.Code != http.StatusOK {
		t.Fatalf("hit: status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}

	got := pullStatusesFor(t, st, ep.ID)
	if got["8.8.8.8"][store.PullStatusGeoBlocked] != 1 {
		t.Errorf("want one geo_blocked row: %+v", got)
	}
	if got["8.8.8.8"][store.PullStatusOK] != 0 {
		t.Errorf("blocked pull must not count as ok: %+v", got)
	}
	if got["1.2.4.8"][store.PullStatusOK] != 1 {
		t.Errorf("allowed ip must be served: %+v", got)
	}
}

// TestGeoPull_EnforceBlocksUnresolvableIP a private address behind a
// misconfigured proxy is a miss, not a bypass.
func TestGeoPull_EnforceBlocksUnresolvableIP(t *testing.T) {
	srv, st := newGeoTestServer(t)
	h := srv.Handler()
	ep, _ := st.CreateEndpoint("私网强制设备")
	if err := st.UpdateEndpointGeoConfig(ep.ID, store.GeoModeEnforce, "CN", ""); err != nil {
		t.Fatalf("UpdateEndpointGeoConfig: %v", err)
	}

	if w := pullFrom(t, h, ep, "192.168.1.20"); w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body %s)", w.Code, w.Body.String())
	}
	if got := pullStatusesFor(t, st, ep.ID); got["192.168.1.20"][store.PullStatusGeoBlocked] != 1 {
		t.Errorf("want one geo_blocked row for the private ip: %+v", got)
	}
}

// TestGeoPull_InvalidTokenStillNeverReachesGeoGuard the iron rule of the chain
// holds for this guard too: an unauthenticated caller gets the uniform 404, so
// the 403 can never be used to probe which paths exist.
func TestGeoPull_InvalidTokenStillNeverReachesGeoGuard(t *testing.T) {
	srv, st := newGeoTestServer(t)
	h := srv.Handler()
	ep, _ := st.CreateEndpoint("地域鉴权顺序设备")
	if err := st.UpdateEndpointGeoConfig(ep.ID, store.GeoModeEnforce, "CN", ""); err != nil {
		t.Fatalf("UpdateEndpointGeoConfig: %v", err)
	}

	req := httptest.NewRequest("GET", "/sub/"+ep.Path+"?token=wrong", nil)
	req.RemoteAddr = "8.8.8.8:41000"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	if got := pullStatusesFor(t, st, ep.ID); got["8.8.8.8"][store.PullStatusGeoBlocked] != 0 {
		t.Errorf("bad-token pull left a geo trace: %+v", got)
	}
}
