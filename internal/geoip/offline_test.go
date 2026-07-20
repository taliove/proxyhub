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

// buildMiniDB constructs a tiny in-process country database with a couple of
// mapped ranges, so the resolver logic is exercised without depending on the
// real embedded mmdb contents.
func buildMiniDB(t *testing.T) *maxminddb.Reader {
	t.Helper()
	tree, err := mmdbwriter.New(mmdbwriter.Options{
		DatabaseType: "DBIP-Country-Lite-Test",
		RecordSize:   24,
		// Allow inserting into documentation/private ranges used by these tests
		// (e.g. 2001:db8::/32). Unmapped reserved ranges still miss.
		IncludeReservedNetworks: true,
	})
	if err != nil {
		t.Fatalf("mmdbwriter.New: %v", err)
	}
	insert := func(cidr, code string) {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			t.Fatalf("ParseCIDR(%q): %v", cidr, err)
		}
		rec := mmdbtype.Map{"country": mmdbtype.Map{"iso_code": mmdbtype.String(code)}}
		if err := tree.Insert(network, rec); err != nil {
			t.Fatalf("Insert(%q): %v", cidr, err)
		}
	}
	insert("1.0.0.0/24", "US")    // IPv4 range
	insert("2001:db8::/32", "JP") // IPv6 range
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

func TestLookupCountryIn_HitIPv4(t *testing.T) {
	reader := buildMiniDB(t)
	code, err := lookupCountryIn(reader, "1.0.0.1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != "US" {
		t.Errorf("code = %q, want US", code)
	}
}

func TestLookupCountryIn_HitIPv6(t *testing.T) {
	reader := buildMiniDB(t)
	code, err := lookupCountryIn(reader, "2001:db8::1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != "JP" {
		t.Errorf("code = %q, want JP", code)
	}
}

func TestLookupCountryIn_PrivateMiss(t *testing.T) {
	reader := buildMiniDB(t)
	_, err := lookupCountryIn(reader, "10.1.2.3")
	if !errors.Is(err, ErrCountryNotFound) {
		t.Errorf("err = %v, want ErrCountryNotFound", err)
	}
}

func TestLookupCountryIn_InvalidInput(t *testing.T) {
	reader := buildMiniDB(t)
	if _, err := lookupCountryIn(reader, "not-an-ip"); err == nil {
		t.Error("expected error for invalid ip, got nil")
	}
}

// TestLookupCountry_EmbeddedDB smoke-tests the real embedded database through
// the exported entry point: a well-known public IP resolves, a private IP does
// not. It asserts resolvability, not a specific country (data may shift monthly).
func TestLookupCountry_EmbeddedDB(t *testing.T) {
	if _, err := LookupCountry("8.8.8.8"); err != nil {
		t.Errorf("LookupCountry(8.8.8.8) unexpected error: %v", err)
	}
	if _, err := LookupCountry("192.168.1.1"); err == nil {
		t.Error("LookupCountry(192.168.1.1) expected miss, got nil error")
	}
}
