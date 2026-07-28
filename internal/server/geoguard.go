package server

import (
	"net/http"
	"strings"

	"github.com/taliove/proxyhub/internal/geoip"
	"github.com/taliove/proxyhub/internal/store"
)

// Geo allowlist guard (pull-guard ticket 07).
//
// The guard reads the three geo columns off the endpoint row the chain already
// fetched by path, so it costs no query - only an offline GeoIP lookup, which
// is an in-memory tree walk over the embedded database.
//
// Three modes, one judgement:
//   - off      -> allowPull, no lookup at all. Default for every address.
//   - observe  -> judge; on a miss record geo_would_block and still serve.
//   - enforce  -> judge; on a miss record geo_blocked and answer 403.
//
// FAIL-CLOSED ON UNKNOWN LOCATION: a lookup failure, a private/reserved address
// with no country record, or a province list that the embedded database cannot
// resolve all count as a MISS, not as a match. In observe mode that is free
// (the pull is served either way and the trace shows up), which is exactly why
// observe exists: an operator switching an address to enforce can first confirm
// from pull_logs that real clients are not landing in the unknown bucket. The
// alternative - treating unknown as a match - would make the allowlist trivially
// bypassable by anything the database does not cover.
type geoAllowlistGuard struct {
	// lookupCountry resolves an IP to an ISO 3166-1 alpha-2 code. Seam for
	// tests; production wires geoip.LookupCountry.
	lookupCountry func(ip string) (string, error)
	// lookupSubdivision resolves an IP to (subdivision code, English name).
	// Seam for tests; production wires geoip.LookupSubdivision, which with the
	// shipped Country-Lite database always reports "unavailable" (see
	// internal/geoip/subdivision.go for the measurement).
	lookupSubdivision func(ip string) (code, name string, err error)
}

// newGeoAllowlistGuard wires the guard to its two lookup seams. Nil seams fall
// back to the embedded offline database.
func newGeoAllowlistGuard(
	lookupCountry func(ip string) (string, error),
	lookupSubdivision func(ip string) (string, string, error),
) *geoAllowlistGuard {
	if lookupCountry == nil {
		lookupCountry = geoip.LookupCountry
	}
	if lookupSubdivision == nil {
		lookupSubdivision = geoip.LookupSubdivision
	}
	return &geoAllowlistGuard{lookupCountry: lookupCountry, lookupSubdivision: lookupSubdivision}
}

func (g *geoAllowlistGuard) name() string { return "geo_allowlist" }

// check applies the address's geo allowlist to one authenticated pull.
//
// The 403 deliberately does not explain itself beyond "not allowed from your
// location": the caller already holds a valid token, so hiding behind the stock
// 404 would only make a legitimate user's misconfiguration undebuggable, while
// naming the allowed countries would hand an attacker the allowlist.
func (g *geoAllowlistGuard) check(gr subGuardRequest) subGuardVerdict {
	mode := store.NormalizeGeoMode(gr.ep.GeoMode)
	if mode == store.GeoModeOff {
		return allowPull()
	}
	if g.matches(gr.ep, gr.ip) {
		return allowPull()
	}
	if mode == store.GeoModeObserve {
		return observePull(store.PullStatusGeoWouldBlock)
	}
	return blockPull(store.PullStatusGeoBlocked, func(w http.ResponseWriter) {
		http.Error(w, "subscription not allowed from your location", http.StatusForbidden)
	})
}

// matches reports whether ip satisfies every configured dimension of ep's
// allowlist. An empty list means that dimension is not judged, so an address in
// enforce mode with both lists empty still serves everybody - narrowing is an
// explicit act. Both dimensions must pass when both are configured (AND, not
// OR): a province list only ever makes an allowlist stricter.
func (g *geoAllowlistGuard) matches(ep *store.Endpoint, ip string) bool {
	countries := store.ParseGeoList(ep.GeoCountries)
	provinces := store.ParseGeoList(ep.GeoProvinces)

	if len(countries) > 0 && !g.countryMatches(countries, ip) {
		return false
	}
	if len(provinces) > 0 && !g.provinceMatches(provinces, ip) {
		return false
	}
	return true
}

// countryMatches resolves ip's country and reports whether it is on the list.
// An unresolvable address (lookup error, private range with no record) is a
// miss - see the fail-closed note on the guard type.
func (g *geoAllowlistGuard) countryMatches(allowed []string, ip string) bool {
	code, err := g.lookupCountry(ip)
	if err != nil || code == "" {
		return false
	}
	return containsFold(allowed, code)
}

// provinceMatches resolves ip's first-level subdivision and reports whether
// either its code or its English name is on the list (an operator may write
// "GD" or "Guangdong"). With the shipped Country-Lite database the lookup
// always reports unavailable, so a non-empty province list is a permanent miss:
// enforce would reject everybody. That is the conservative direction and is
// documented for operators; observe mode is how they find out.
func (g *geoAllowlistGuard) provinceMatches(allowed []string, ip string) bool {
	code, name, err := g.lookupSubdivision(ip)
	if err != nil {
		return false
	}
	return containsFold(allowed, code) || containsFold(allowed, name)
}

// containsFold reports whether value (non-empty) appears in list, ignoring case.
func containsFold(list []string, value string) bool {
	if value == "" {
		return false
	}
	for _, item := range list {
		if strings.EqualFold(item, value) {
			return true
		}
	}
	return false
}
