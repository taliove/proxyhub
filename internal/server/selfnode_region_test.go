package server

import (
	"context"
	"errors"
	"testing"
)

func TestResolveRegionCode_GeoIPHit(t *testing.T) {
	d := regionResolverDeps{
		lookupHost: func(string) ([]string, error) { return []string{"1.2.3.4"}, nil },
		geoLookup:  func(context.Context, string) (string, error) { return "日本", nil },
		recognize:  func(name string) string { if name == "日本" { return "JP" }; return "Unknown" },
	}
	if got := resolveRegionCode(context.Background(), "tokyo.example.com", "我的机", d); got != "JP" {
		t.Errorf("got %q, want JP", got)
	}
}

func TestResolveRegionCode_FallbackToName(t *testing.T) {
	d := regionResolverDeps{
		lookupHost: func(string) ([]string, error) { return nil, errors.New("dns fail") },
		geoLookup:  func(context.Context, string) (string, error) { return "", errors.New("geo fail") },
		recognize:  func(name string) string { if name == "香港VIP" { return "HK" }; return "Unknown" },
	}
	if got := resolveRegionCode(context.Background(), "bad.host", "香港VIP", d); got != "HK" {
		t.Errorf("got %q, want HK (name fallback)", got)
	}
}

func TestResolveRegionCode_AllFailUnknown(t *testing.T) {
	d := regionResolverDeps{
		lookupHost: func(string) ([]string, error) { return nil, errors.New("x") },
		geoLookup:  func(context.Context, string) (string, error) { return "", errors.New("x") },
		recognize:  func(string) string { return "Unknown" },
	}
	if got := resolveRegionCode(context.Background(), "h", "n", d); got != "Unknown" {
		t.Errorf("got %q, want Unknown", got)
	}
}

func TestResolveRegionCode_ServerIsIP(t *testing.T) {
	called := false
	d := regionResolverDeps{
		lookupHost: func(string) ([]string, error) { called = true; return nil, nil },
		geoLookup:  func(context.Context, string) (string, error) { return "美国", nil },
		recognize:  func(name string) string { if name == "美国" { return "US" }; return "Unknown" },
	}
	if got := resolveRegionCode(context.Background(), "8.8.8.8", "n", d); got != "US" {
		t.Errorf("got %q, want US", got)
	}
	if called {
		t.Error("lookupHost should NOT be called when server is already an IP")
	}
}
