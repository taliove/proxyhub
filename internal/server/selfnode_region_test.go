package server

import (
	"errors"
	"testing"
)

// GeoIP hit: the ISO code from the offline lookup is used directly as the region
// code, without consulting the name recognizer.
func TestResolveRegionCode_GeoIPHit(t *testing.T) {
	recognizeCalled := false
	d := regionResolverDeps{
		lookupHost:    func(string) ([]string, error) { return []string{"1.2.3.4"}, nil },
		countryLookup: func(string) (string, error) { return "JP", nil },
		recognize:     func(string) string { recognizeCalled = true; return "Unknown" },
	}
	if got := resolveRegionCode("tokyo.example.com", "我的机", d); got != "JP" {
		t.Errorf("got %q, want JP", got)
	}
	if recognizeCalled {
		t.Error("recognize should NOT be called when GeoIP resolves the IP")
	}
}

// GeoIP chain fails (DNS + lookup) -> fall back to recognizing the node name.
func TestResolveRegionCode_FallbackToName(t *testing.T) {
	d := regionResolverDeps{
		lookupHost:    func(string) ([]string, error) { return nil, errors.New("dns fail") },
		countryLookup: func(string) (string, error) { return "", errors.New("geo fail") },
		recognize: func(name string) string {
			if name == "香港VIP" {
				return "HK"
			}
			return "Unknown"
		},
	}
	if got := resolveRegionCode("bad.host", "香港VIP", d); got != "HK" {
		t.Errorf("got %q, want HK (name fallback)", got)
	}
}

// Everything fails -> "Unknown".
func TestResolveRegionCode_AllFailUnknown(t *testing.T) {
	d := regionResolverDeps{
		lookupHost:    func(string) ([]string, error) { return nil, errors.New("x") },
		countryLookup: func(string) (string, error) { return "", errors.New("x") },
		recognize:     func(string) string { return "Unknown" },
	}
	if got := resolveRegionCode("h", "n", d); got != "Unknown" {
		t.Errorf("got %q, want Unknown", got)
	}
}

// Server is already an IP: DNS resolution is skipped, GeoIP is used directly.
func TestResolveRegionCode_ServerIsIP(t *testing.T) {
	called := false
	d := regionResolverDeps{
		lookupHost: func(string) ([]string, error) { called = true; return nil, nil },
		countryLookup: func(ip string) (string, error) {
			if ip == "8.8.8.8" {
				return "US", nil
			}
			return "", errors.New("x")
		},
		recognize: func(string) string { return "Unknown" },
	}
	if got := resolveRegionCode("8.8.8.8", "n", d); got != "US" {
		t.Errorf("got %q, want US", got)
	}
	if called {
		t.Error("lookupHost should NOT be called when server is already an IP")
	}
}

// GeoIP lookup returns an empty code (no error) -> treated as a miss, falls back.
func TestResolveRegionCode_EmptyCodeFallsBack(t *testing.T) {
	d := regionResolverDeps{
		lookupHost:    func(string) ([]string, error) { return []string{"1.2.3.4"}, nil },
		countryLookup: func(string) (string, error) { return "", nil },
		recognize: func(name string) string {
			if name == "美国节点" {
				return "US"
			}
			return "Unknown"
		},
	}
	if got := resolveRegionCode("x.example.com", "美国节点", d); got != "US" {
		t.Errorf("got %q, want US (empty code falls back to name)", got)
	}
}
