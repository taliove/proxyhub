package server

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/taliove/proxyhub/internal/geoip"
	"github.com/taliove/proxyhub/internal/store"
)

// stubGeoLookups builds the two guard seams from a fixed IP -> location table.
// An address absent from the table behaves like the embedded database's "no
// record" answer, which is how private and reserved ranges show up in reality.
type geoFixture struct {
	country     string
	subdivision string
	subName     string
}

func stubGeoGuard(table map[string]geoFixture) *geoAllowlistGuard {
	return &geoAllowlistGuard{
		lookupCountry: func(ip string) (string, error) {
			if f, ok := table[ip]; ok && f.country != "" {
				return f.country, nil
			}
			return "", geoip.ErrCountryNotFound
		},
		lookupSubdivision: func(ip string) (string, string, error) {
			if f, ok := table[ip]; ok && (f.subdivision != "" || f.subName != "") {
				return f.subdivision, f.subName, nil
			}
			return "", "", geoip.ErrSubdivisionUnavailable
		},
	}
}

var geoTable = map[string]geoFixture{
	"1.2.4.8":    {country: "CN", subdivision: "BJ", subName: "Beijing"},
	"119.29.1.1": {country: "CN", subdivision: "GD", subName: "Guangdong"},
	"8.8.8.8":    {country: "US", subdivision: "CA", subName: "California"},
	// 192.168.x and 10.x are deliberately absent: unresolvable, like a private IP.
}

// checkGeo runs the guard against a synthetic endpoint configuration.
func checkGeo(g *geoAllowlistGuard, mode, countries, provinces, ip string) subGuardVerdict {
	ep := &store.Endpoint{
		ID: 42, GeoMode: mode,
		GeoCountries: store.NormalizeGeoCountries(countries),
		GeoProvinces: store.NormalizeGeoProvinces(provinces),
	}
	return g.check(subGuardRequest{ep: ep, ip: ip})
}

// TestGeoGuard_ThreeModes is the core behaviour table: off is inert, observe
// records without blocking, enforce blocks a miss and stays quiet on a hit.
func TestGeoGuard_ThreeModes(t *testing.T) {
	g := stubGeoGuard(geoTable)
	cases := []struct {
		name       string
		mode       string
		countries  string
		ip         string
		wantStatus string
		wantBlock  bool
	}{
		{"off ignores a foreign ip", store.GeoModeOff, "CN", "8.8.8.8", "", false},
		{"off with empty mode string", "", "CN", "8.8.8.8", "", false},
		{"observe hit stays silent", store.GeoModeObserve, "CN", "1.2.4.8", "", false},
		{"observe miss records only", store.GeoModeObserve, "CN", "8.8.8.8", store.PullStatusGeoWouldBlock, false},
		{"enforce hit stays silent", store.GeoModeEnforce, "CN", "1.2.4.8", "", false},
		{"enforce miss blocks", store.GeoModeEnforce, "CN", "8.8.8.8", store.PullStatusGeoBlocked, true},
	}
	for _, c := range cases {
		got := checkGeo(g, c.mode, c.countries, "", c.ip)
		if got.status != c.wantStatus {
			t.Errorf("%s: status = %q, want %q", c.name, got.status, c.wantStatus)
		}
		if got.block != c.wantBlock {
			t.Errorf("%s: block = %v, want %v", c.name, got.block, c.wantBlock)
		}
	}
}

// TestGeoGuard_EnforceBlockWrites403 the blocking verdict answers 403, not the
// stock 404: the caller holds a valid token, so it needs a debuggable answer.
func TestGeoGuard_EnforceBlockWrites403(t *testing.T) {
	g := stubGeoGuard(geoTable)
	verdict := checkGeo(g, store.GeoModeEnforce, "CN", "", "8.8.8.8")
	if !verdict.block || verdict.respond == nil {
		t.Fatalf("verdict = %+v, want a blocking verdict with a responder", verdict)
	}
	w := httptest.NewRecorder()
	verdict.respond(w)
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", w.Code)
	}
	if body := w.Body.String(); body == "" {
		t.Error("403 body is empty, want a reason for the operator")
	}
}

// TestGeoGuard_EmptyCountryListDoesNotJudge an address switched to enforce with
// no list configured still serves everybody. Narrowing must be explicit, never
// a side effect of flipping the mode.
func TestGeoGuard_EmptyCountryListDoesNotJudge(t *testing.T) {
	g := stubGeoGuard(geoTable)
	for _, ip := range []string{"8.8.8.8", "192.168.1.9", "1.2.4.8"} {
		got := checkGeo(g, store.GeoModeEnforce, "", "", ip)
		if got.block || got.status != "" {
			t.Errorf("ip %s: verdict = %+v, want a silent allow", ip, got)
		}
	}
}

// TestGeoGuard_PrivateIPMissesUnderEnforce fail-closed: an address the database
// cannot place counts as a miss, so enforce rejects it and observe records it.
func TestGeoGuard_PrivateIPMissesUnderEnforce(t *testing.T) {
	g := stubGeoGuard(geoTable)
	for _, ip := range []string{"192.168.1.9", "10.0.0.5", "127.0.0.1"} {
		enforced := checkGeo(g, store.GeoModeEnforce, "CN", "", ip)
		if !enforced.block || enforced.status != store.PullStatusGeoBlocked {
			t.Errorf("enforce ip %s: verdict = %+v, want geo_blocked block", ip, enforced)
		}
		observed := checkGeo(g, store.GeoModeObserve, "CN", "", ip)
		if observed.block || observed.status != store.PullStatusGeoWouldBlock {
			t.Errorf("observe ip %s: verdict = %+v, want a would_block trace", ip, observed)
		}
	}
}

// TestGeoGuard_LookupErrorIsAMiss a broken database (not just an unmapped
// address) must not become a bypass.
func TestGeoGuard_LookupErrorIsAMiss(t *testing.T) {
	g := &geoAllowlistGuard{
		lookupCountry: func(string) (string, error) { return "", errors.New("database unreadable") },
		lookupSubdivision: func(string) (string, string, error) {
			return "", "", errors.New("database unreadable")
		},
	}
	got := checkGeo(g, store.GeoModeEnforce, "CN", "", "1.2.4.8")
	if !got.block || got.status != store.PullStatusGeoBlocked {
		t.Errorf("verdict = %+v, want geo_blocked", got)
	}
}

// TestGeoGuard_CountryMatchIsCaseInsensitive a hand-written "cn" behaves like
// "CN", even if it reached the column without going through normalisation.
func TestGeoGuard_CountryMatchIsCaseInsensitive(t *testing.T) {
	g := stubGeoGuard(geoTable)
	ep := &store.Endpoint{ID: 1, GeoMode: store.GeoModeEnforce, GeoCountries: "cn, jp"}
	if got := g.check(subGuardRequest{ep: ep, ip: "1.2.4.8"}); got.block {
		t.Errorf("verdict = %+v, want an allow for CN against a lowercase list", got)
	}
}

// TestGeoGuard_ProvinceListNarrowsWithinCountry the two dimensions are ANDed: a
// province list can only make an allowlist stricter, never wider.
func TestGeoGuard_ProvinceListNarrowsWithinCountry(t *testing.T) {
	g := stubGeoGuard(geoTable)
	cases := []struct {
		name      string
		provinces string
		ip        string
		wantBlock bool
	}{
		{"code hit", "GD", "119.29.1.1", false},
		{"name hit", "Guangdong", "119.29.1.1", false},
		{"lowercase name hit", "guangdong", "119.29.1.1", false},
		{"wrong province in allowed country", "GD", "1.2.4.8", true},
		{"right province wrong country", "CA", "8.8.8.8", true},
	}
	for _, c := range cases {
		got := checkGeo(g, store.GeoModeEnforce, "CN", c.provinces, c.ip)
		if got.block != c.wantBlock {
			t.Errorf("%s: block = %v, want %v", c.name, got.block, c.wantBlock)
		}
	}
}

// TestGeoGuard_ProvinceListWithoutCountryList a province-only configuration is
// judged on its own; no country list means that dimension is not judged.
func TestGeoGuard_ProvinceListWithoutCountryList(t *testing.T) {
	g := stubGeoGuard(geoTable)
	if got := checkGeo(g, store.GeoModeEnforce, "", "GD", "119.29.1.1"); got.block {
		t.Errorf("verdict = %+v, want an allow for the listed province", got)
	}
	if got := checkGeo(g, store.GeoModeEnforce, "", "GD", "1.2.4.8"); !got.block {
		t.Errorf("verdict = %+v, want a block for an unlisted province", got)
	}
}

// TestGeoGuard_ProvinceUnavailableIsAMiss with the shipped Country-Lite
// database no address resolves to a province, so a non-empty province list
// rejects everybody under enforce. This is the documented conservative
// direction, and it is what observe mode exists to reveal before an operator
// commits to it.
func TestGeoGuard_ProvinceUnavailableIsAMiss(t *testing.T) {
	g := &geoAllowlistGuard{
		lookupCountry: func(string) (string, error) { return "CN", nil },
		lookupSubdivision: func(string) (string, string, error) {
			return "", "", geoip.ErrSubdivisionUnavailable
		},
	}
	enforced := checkGeo(g, store.GeoModeEnforce, "CN", "GD", "1.2.4.8")
	if !enforced.block || enforced.status != store.PullStatusGeoBlocked {
		t.Errorf("enforce verdict = %+v, want geo_blocked", enforced)
	}
	observed := checkGeo(g, store.GeoModeObserve, "CN", "GD", "1.2.4.8")
	if observed.block || observed.status != store.PullStatusGeoWouldBlock {
		t.Errorf("observe verdict = %+v, want a would_block trace", observed)
	}
	// Same address, no province list: the country alone is enough to pass.
	if got := checkGeo(g, store.GeoModeEnforce, "CN", "", "1.2.4.8"); got.block {
		t.Errorf("country-only verdict = %+v, want an allow", got)
	}
}

// TestGeoGuard_OffModeSkipsLookupEntirely the default costs nothing: no lookup
// happens at all, so an off address cannot be slowed down by geo resolution.
func TestGeoGuard_OffModeSkipsLookupEntirely(t *testing.T) {
	calls := 0
	g := &geoAllowlistGuard{
		lookupCountry: func(string) (string, error) { calls++; return "US", nil },
		lookupSubdivision: func(string) (string, string, error) {
			calls++
			return "", "", geoip.ErrSubdivisionUnavailable
		},
	}
	if got := checkGeo(g, store.GeoModeOff, "CN", "GD", "8.8.8.8"); got.block || got.status != "" {
		t.Errorf("verdict = %+v, want a silent allow", got)
	}
	if calls != 0 {
		t.Errorf("lookups = %d, want 0 for an off address", calls)
	}
}

// TestNewGeoAllowlistGuard_DefaultsToEmbeddedDB nil seams fall back to the
// offline database rather than to a nil call.
func TestNewGeoAllowlistGuard_DefaultsToEmbeddedDB(t *testing.T) {
	g := newGeoAllowlistGuard(nil, nil)
	if g.lookupCountry == nil || g.lookupSubdivision == nil {
		t.Fatal("nil seams were not defaulted")
	}
	if got := g.check(subGuardRequest{
		ep: &store.Endpoint{ID: 1, GeoMode: store.GeoModeEnforce, GeoCountries: "US"},
		ip: "8.8.8.8",
	}); got.block {
		t.Errorf("verdict = %+v, want 8.8.8.8 allowed by a US allowlist", got)
	}
}
