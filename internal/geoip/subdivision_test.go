package geoip

import (
	"bytes"
	"errors"
	"net"
	"testing"

	"github.com/maxmind/mmdbwriter"
	"github.com/maxmind/mmdbwriter/mmdbtype"
	maxminddb "github.com/oschwald/maxminddb-golang"
)

// buildSubdivisionDB constructs a tiny City-shaped database that does carry
// subdivisions, so the parsing logic is exercised even though the shipped
// Country-Lite database has none.
func buildSubdivisionDB(t *testing.T) *maxminddb.Reader {
	t.Helper()
	tree, err := mmdbwriter.New(mmdbwriter.Options{
		DatabaseType:            "Test-City",
		RecordSize:              24,
		IncludeReservedNetworks: true,
	})
	if err != nil {
		t.Fatalf("mmdbwriter.New: %v", err)
	}
	insert := func(cidr, code, name string) {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			t.Fatalf("ParseCIDR(%q): %v", cidr, err)
		}
		rec := mmdbtype.Map{
			"country": mmdbtype.Map{"iso_code": mmdbtype.String("CN")},
			"subdivisions": mmdbtype.Slice{
				mmdbtype.Map{
					"iso_code": mmdbtype.String(code),
					"names":    mmdbtype.Map{"en": mmdbtype.String(name)},
				},
			},
		}
		if err := tree.Insert(network, rec); err != nil {
			t.Fatalf("Insert(%q): %v", cidr, err)
		}
	}
	insert("1.0.0.0/24", "GD", "Guangdong")
	// 10.0.0.0/8 intentionally left unmapped -> lookups miss.

	var buf bytes.Buffer
	if _, err := tree.WriteTo(&buf); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	reader, err := maxminddb.FromBytes(buf.Bytes())
	if err != nil {
		t.Fatalf("FromBytes: %v", err)
	}
	t.Cleanup(func() { reader.Close() })
	return reader
}

func TestLookupSubdivisionIn_Hit(t *testing.T) {
	code, name, err := lookupSubdivisionIn(buildSubdivisionDB(t), "1.0.0.1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != "GD" || name != "Guangdong" {
		t.Errorf("got (%q, %q), want (GD, Guangdong)", code, name)
	}
}

func TestLookupSubdivisionIn_Miss(t *testing.T) {
	_, _, err := lookupSubdivisionIn(buildSubdivisionDB(t), "10.1.2.3")
	if !errors.Is(err, ErrSubdivisionUnavailable) {
		t.Errorf("err = %v, want ErrSubdivisionUnavailable", err)
	}
}

func TestLookupSubdivisionIn_InvalidInput(t *testing.T) {
	if _, _, err := lookupSubdivisionIn(buildSubdivisionDB(t), "not-an-ip"); err == nil {
		t.Error("expected error for invalid ip, got nil")
	}
}

// TestLookupSubdivision_EmbeddedDBHasNoProvinces pins down the measured fact
// behind the guard's documented province semantics: the shipped Country-Lite
// database resolves CN addresses to a country and to no subdivision at all.
// If this ever starts failing, a City-level database has been adopted and the
// province allowlist has become usable - update the docs, not the assertion.
func TestLookupSubdivision_EmbeddedDBHasNoProvinces(t *testing.T) {
	cnIPs := []string{"1.2.4.8", "114.114.114.114", "180.76.76.76", "223.5.5.5", "202.96.209.133"}
	for _, ip := range cnIPs {
		if country, err := LookupCountry(ip); err != nil || country != "CN" {
			t.Fatalf("LookupCountry(%s) = (%q, %v), want (CN, nil) - probe IP is stale", ip, country, err)
		}
		if _, _, err := LookupSubdivision(ip); !errors.Is(err, ErrSubdivisionUnavailable) {
			t.Errorf("LookupSubdivision(%s) err = %v, want ErrSubdivisionUnavailable", ip, err)
		}
	}
}
