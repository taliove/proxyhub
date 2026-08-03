package store

// migrateEndpointPublicName adds the public_name column to endpoints
// (subscription profile title, issue #38). Idempotent: safe on every startup.
//
// The column defaults to the empty string, which every reader treats as
// "no public name set" — the /sub handler then falls back to the bare brand
// title. This is the same convention as geo_countries: a NOT NULL DEFAULT ''
// column keeps legacy rows meaningful without any backfill.
//
// New databases get the column straight from the CREATE TABLE in migrate();
// this function is what upgrades an existing file (same dual-path shape as
// migrateEndpointGeo).
func (s *Store) migrateEndpointPublicName() error {
	return s.addColumnIfMissing("endpoints", "public_name", "TEXT NOT NULL DEFAULT ''")
}
