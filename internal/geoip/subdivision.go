package geoip

import (
	"errors"
	"fmt"
	"net"
)

// Province (subdivision) resolution against the embedded database.
//
// MEASURED FACT (pull-guard ticket 07): the embedded database is DB-IP
// Country-Lite. Its records carry exactly two objects, `continent` and
// `country`; there is no `subdivisions` key at all. Probing known CN ranges
// (1.2.4.8, 114.114.114.114, 180.76.76.76, 223.5.5.5, 202.96.209.133,
// 61.139.2.69) returns the country CN and nothing province-level, i.e. province
// coverage is 0%, not "partial". Upgrading to a City-level database is the only
// way to change that, and that is a data-licensing decision, not a code one.
//
// The lookup below is therefore written as a real reader against the real
// record shape - so that dropping in a City database makes it start answering
// with no code change - while ErrSubdivisionUnavailable is what it returns
// today for every address. Callers must treat that error as "cannot judge the
// province", never as "province matches".

// ErrSubdivisionUnavailable is returned when the embedded database holds no
// subdivision (province / state) record for an address. With the shipped
// Country-Lite database this is the answer for every address.
var ErrSubdivisionUnavailable = errors.New("geoip: no subdivision for ip")

// LookupSubdivision resolves an IP to its first-level subdivision, returning
// both the ISO 3166-2 code (e.g. "GD") and the English name (e.g. "Guangdong").
// It never performs network I/O.
//
// Errors: an invalid IP string yields an error; an address with no subdivision
// record yields ErrSubdivisionUnavailable.
func LookupSubdivision(ip string) (code, name string, err error) {
	reader, err := loadCountryReader()
	if err != nil {
		return "", "", fmt.Errorf("geoip: open database: %w", err)
	}
	return lookupSubdivisionIn(reader, ip)
}

// lookupSubdivisionIn holds the resolution logic against an explicit reader so
// tests can drive it with an in-process database that does carry subdivisions.
func lookupSubdivisionIn(reader subdivisionReader, ip string) (code, name string, err error) {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return "", "", fmt.Errorf("geoip: invalid ip %q", ip)
	}
	var record struct {
		Subdivisions []struct {
			ISOCode string            `maxminddb:"iso_code"`
			Names   map[string]string `maxminddb:"names"`
		} `maxminddb:"subdivisions"`
	}
	if err := reader.Lookup(parsed, &record); err != nil {
		return "", "", fmt.Errorf("geoip: lookup %s: %w", ip, err)
	}
	if len(record.Subdivisions) == 0 {
		return "", "", ErrSubdivisionUnavailable
	}
	first := record.Subdivisions[0]
	if first.ISOCode == "" && first.Names["en"] == "" {
		return "", "", ErrSubdivisionUnavailable
	}
	return first.ISOCode, first.Names["en"], nil
}

// subdivisionReader is the slice of maxminddb.Reader this file needs, kept
// narrow so tests can substitute a database of their own.
type subdivisionReader interface {
	Lookup(ip net.IP, result any) error
}
