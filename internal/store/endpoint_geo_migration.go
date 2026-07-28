package store

import "fmt"

// migrateEndpointGeo adds the per-address geo allowlist columns to endpoints
// (pull-guard ticket 07). Idempotent: safe on every startup.
//
//   - geo_mode defaults to 'off', so every pre-existing address keeps its
//     current behaviour (no geo judgement) until an operator opts in. This is
//     the whole reason the column is NOT NULL DEFAULT 'off' rather than
//     nullable: a NULL would force every reader to decide what it means.
//   - geo_countries / geo_provinces default to the empty string, which the
//     guard reads as "do not judge this dimension". An address in enforce mode
//     with both lists empty therefore still serves everybody; narrowing is an
//     explicit act, never a side effect of switching mode.
//
// No index is added: the guard reads the columns off the endpoint row it has
// already fetched by path, so there is no query to serve.
//
// Must run after the base schema has created endpoints. New databases get the
// columns straight from the CREATE TABLE in migrate(); this function is what
// upgrades an existing file (same dual-path shape as migratePullLogStatus).
func (s *Store) migrateEndpointGeo() error {
	columns := []struct {
		name string
		decl string
	}{
		{"geo_mode", fmt.Sprintf("TEXT NOT NULL DEFAULT '%s'", GeoModeOff)},
		{"geo_countries", "TEXT NOT NULL DEFAULT ''"},
		{"geo_provinces", "TEXT NOT NULL DEFAULT ''"},
	}
	for _, c := range columns {
		if err := s.addColumnIfMissing("endpoints", c.name, c.decl); err != nil {
			return err
		}
	}
	return nil
}
