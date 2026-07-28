package store

import (
	"fmt"
	"strings"
)

// Per-address geo allowlist configuration (pull-guard ticket 07).
//
// A subscription address may restrict which locations are allowed to pull it.
// The setting has three states so an operator can measure before enforcing:
// off (default, no judgement at all), observe (judge and record, still serve)
// and enforce (judge and reject a miss). The guard that reads these columns
// lives in the server package; store only owns storage plus normalisation.
const (
	// GeoModeOff no geo judgement happens. Default for every address.
	GeoModeOff = "off"
	// GeoModeObserve judge, record a geo_would_block trace on a miss, serve anyway.
	GeoModeObserve = "observe"
	// GeoModeEnforce judge, reject a miss with 403 and a geo_blocked trace.
	GeoModeEnforce = "enforce"
)

// IsValidGeoMode reports whether mode is one of the three known modes. The empty
// string is accepted as off so a row written before this feature (or by a client
// that omits the field) keeps the inert default.
func IsValidGeoMode(mode string) bool {
	switch mode {
	case "", GeoModeOff, GeoModeObserve, GeoModeEnforce:
		return true
	default:
		return false
	}
}

// NormalizeGeoMode maps the empty string onto GeoModeOff, leaving the three
// explicit modes untouched. Callers should validate first.
func NormalizeGeoMode(mode string) string {
	if mode == "" {
		return GeoModeOff
	}
	return mode
}

// ParseGeoList splits a stored comma separated allowlist into its entries.
// Blank entries are dropped and surrounding spaces trimmed, so a hand-edited
// "CN, , JP " yields [CN JP]. Case is preserved; matching is case-insensitive.
func ParseGeoList(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// NormalizeGeoCountries canonicalises a country allowlist for storage: trimmed,
// upper-cased ISO 3166-1 alpha-2 codes, duplicates dropped, order preserved.
// Storing one canonical shape keeps the guard's comparison cheap and keeps the
// UI from showing "cn" and "CN" as two different rules.
func NormalizeGeoCountries(raw string) string {
	return joinUnique(ParseGeoList(raw), strings.ToUpper)
}

// NormalizeGeoProvinces canonicalises a province allowlist for storage. Unlike
// countries there is no single code standard in play (a province may be spelled
// as a subdivision code or a name), so entries are only trimmed and deduplicated
// - case folding happens at comparison time instead.
func NormalizeGeoProvinces(raw string) string {
	return joinUnique(ParseGeoList(raw), func(s string) string { return s })
}

// joinUnique applies transform to every entry and joins the first occurrence of
// each distinct (case-insensitive) result with commas.
func joinUnique(entries []string, transform func(string) string) string {
	seen := make(map[string]struct{}, len(entries))
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		v := transform(e)
		key := strings.ToUpper(v)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, v)
	}
	return strings.Join(out, ",")
}

// UpdateEndpointGeoConfig sets the geo allowlist of one address. mode must be a
// known mode; the two lists are normalised before storage and an empty list
// means "do not judge this dimension".
func (s *Store) UpdateEndpointGeoConfig(id int64, mode, countries, provinces string) error {
	return s.updateEndpointGeoConfig(0, id, mode, countries, provinces)
}

// UpdateEndpointGeoConfigForUser sets the geo allowlist of one address owned by
// userID; a row owned by somebody else answers ErrNotFound, same as the other
// per-owner endpoint writers. userID=0 is the global view (tests / super admin
// before switching), which skips the ownership predicate.
func (s *Store) UpdateEndpointGeoConfigForUser(userID, id int64, mode, countries, provinces string) error {
	return s.updateEndpointGeoConfig(userID, id, mode, countries, provinces)
}

// updateEndpointGeoConfig is the single write path behind both exported setters,
// so validation and normalisation cannot drift between the owner-scoped and the
// global variant.
func (s *Store) updateEndpointGeoConfig(userID, id int64, mode, countries, provinces string) error {
	if !IsValidGeoMode(mode) {
		return fmt.Errorf("invalid geo_mode %q", mode)
	}
	query := `UPDATE endpoints SET geo_mode = ?, geo_countries = ?, geo_provinces = ? WHERE id = ?`
	args := []any{NormalizeGeoMode(mode), NormalizeGeoCountries(countries), NormalizeGeoProvinces(provinces), id}
	if userID != 0 {
		query += ` AND user_id = ?`
		args = append(args, userID)
	}
	res, err := s.db.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("update endpoint geo config: %w", err)
	}
	return checkAffected(res)
}
