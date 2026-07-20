package geoip

import (
	_ "embed"
	"errors"
	"fmt"
	"net"
	"sync"

	maxminddb "github.com/oschwald/maxminddb-golang"
)

// countryDB is the DB-IP Lite country database (CC BY 4.0), embedded at build
// time. It is staged by `make geoip-update`; the binary data file is never
// committed. `make build` hard-fails when it is missing.
//
//go:embed dbip-country.mmdb
var countryDB []byte

// ErrCountryNotFound is returned when an IP resolves to no country (e.g. private
// or reserved ranges that carry no country record).
var ErrCountryNotFound = errors.New("geoip: no country for ip")

// countryReader lazily opens the embedded database once, shared across lookups.
var (
	countryReaderOnce sync.Once
	countryReader     *maxminddb.Reader
	countryReaderErr  error
)

func loadCountryReader() (*maxminddb.Reader, error) {
	countryReaderOnce.Do(func() {
		countryReader, countryReaderErr = maxminddb.FromBytes(countryDB)
	})
	return countryReader, countryReaderErr
}

// LookupCountry resolves an IP (IPv4 or IPv6) to its ISO 3166-1 alpha-2 country
// code using the embedded offline database. It never performs network I/O.
//
// Errors: an invalid IP string yields an error; an IP with no country record
// (private/reserved ranges) yields ErrCountryNotFound.
//
// This is the stable seam other packages reuse for offline geolocation; pair it
// with CountryName to obtain a Chinese country name.
func LookupCountry(ip string) (string, error) {
	reader, err := loadCountryReader()
	if err != nil {
		return "", fmt.Errorf("geoip: open database: %w", err)
	}
	return lookupCountryIn(reader, ip)
}

// lookupCountryIn holds the resolution logic against an explicit reader so tests
// can drive it with an in-process mini database (no dependence on the real
// mmdb contents).
func lookupCountryIn(reader *maxminddb.Reader, ip string) (string, error) {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return "", fmt.Errorf("geoip: invalid ip %q", ip)
	}
	var record struct {
		Country struct {
			ISOCode string `maxminddb:"iso_code"`
		} `maxminddb:"country"`
	}
	if err := reader.Lookup(parsed, &record); err != nil {
		return "", fmt.Errorf("geoip: lookup %s: %w", ip, err)
	}
	if record.Country.ISOCode == "" {
		return "", ErrCountryNotFound
	}
	return record.Country.ISOCode, nil
}
