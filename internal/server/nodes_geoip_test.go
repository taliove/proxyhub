package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/taliove/proxyhub/internal/geoip"
	"github.com/taliove/proxyhub/internal/store"
)

// The save-time region resolution now runs entirely through the embedded
// offline database: a resolvable IP yields its ISO country code directly, and
// the online ip-api.com resolver is never contacted.
func TestResolveSelfNodeRegion_OfflineHitNoOnlineCall(t *testing.T) {
	srv, _ := newTestServer(t, nil)

	hits := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.Write([]byte(`{"status":"success","country":"美国"}`))
	}))
	defer ts.Close()
	// If the self-node path ever fell back to the online resolver, this would
	// register a hit. The node name is deliberately unrecognizable, so the result
	// must come from the fake countryLookup (avoiding DB-IP content dependency).
	srv.geo = geoip.NewResolver(nil, ts.URL)
	srv.countryLookup = func(ip string) (string, error) {
		return "FR", nil // fake returns fixed country code
	}

	n := &store.SelfHostedNode{Name: "zzz-nomatch", Protocol: "vless", Server: "8.8.8.8", Port: 443}
	if got := srv.resolveSelfNodeRegion(n); got != "FR" {
		t.Errorf("RegionCode = %q, want FR (from fake countryLookup)", got)
	}
	if hits != 0 {
		t.Errorf("online resolver was contacted %d times, want 0", hits)
	}
}

// When the offline lookup misses (private IP), resolution falls back to
// recognizing the node name.
func TestResolveSelfNodeRegion_OfflineMissFallsBackToName(t *testing.T) {
	srv, _ := newTestServer(t, nil)

	n := &store.SelfHostedNode{Name: "香港VIP", Protocol: "vless", Server: "192.168.0.1", Port: 443}
	if got := srv.resolveSelfNodeRegion(n); got != "HK" {
		t.Errorf("RegionCode = %q, want HK (name fallback)", got)
	}
}

// Offline miss plus an unrecognizable name yields "Unknown".
func TestResolveSelfNodeRegion_OfflineMissUnknownName(t *testing.T) {
	srv, _ := newTestServer(t, nil)

	n := &store.SelfHostedNode{Name: "zzz-nomatch", Protocol: "vless", Server: "10.0.0.1", Port: 443}
	if got := srv.resolveSelfNodeRegion(n); got != "Unknown" {
		t.Errorf("RegionCode = %q, want Unknown", got)
	}
}
