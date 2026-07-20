package geoip

import "testing"

func TestCountryName(t *testing.T) {
	cases := []struct {
		code string
		want string
	}{
		{"HK", "香港"},
		{"JP", "日本"},
		{"US", "美国"},
		{"IS", "冰岛"},   // long-tail code still resolves
		{"hk", "香港"},   // case-insensitive
		{" us ", "美国"}, // trimmed
		{"ZZ", ""},     // unassigned -> empty
		{"", ""},       // empty -> empty
		{"XYZ", ""},    // malformed -> empty
	}
	for _, c := range cases {
		if got := CountryName(c.code); got != c.want {
			t.Errorf("CountryName(%q) = %q, want %q", c.code, got, c.want)
		}
	}
}
