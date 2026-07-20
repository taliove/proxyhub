package server

import "net"

// regionResolverDeps holds the save-time region resolution dependencies
// (injected for unit tests).
type regionResolverDeps struct {
	lookupHost    func(host string) ([]string, error) // DNS resolution, usually net.LookupHost
	countryLookup func(ip string) (string, error)     // offline IP -> ISO 3166-1 alpha-2 code
	recognize     func(name string) string            // node name -> region code, "Unknown" if none
}

// lookupCountryForServer resolves a server string (IP or hostname) to its country code.
// If server is an IP use it directly, else DNS-resolve to the first IP, then perform
// offline country lookup. Any failing step (empty server, DNS failure, no country record)
// yields an empty string. The offline lookup performs no network I/O.
func lookupCountryForServer(server string, d regionResolverDeps) string {
	ip := ""
	if net.ParseIP(server) != nil {
		ip = server
	} else if addrs, err := d.lookupHost(server); err == nil && len(addrs) > 0 {
		ip = addrs[0]
	}
	if ip == "" {
		return ""
	}
	if code, err := d.countryLookup(ip); err == nil && code != "" {
		return code
	}
	return ""
}

// resolveRegionCode resolves a self-hosted node's region code.
// Order: offline GeoIP chain yields an ISO 3166-1 alpha-2 code used as the region
// code directly (no round-trip through the Chinese-name rule table). If the
// GeoIP chain yields nothing, fall back to recognizing the node name; still
// nothing yields "Unknown" (left for the naming template to handle).
//
// The offline lookup performs no network I/O, so no context/timeout is needed.
func resolveRegionCode(server, nodeName string, d regionResolverDeps) string {
	code := lookupCountryForServer(server, d)
	if code != "" {
		return code
	}

	if code := d.recognize(nodeName); code != "Unknown" {
		return code
	}
	return "Unknown"
}

// resolveRegionGeoOnly resolves a server (IP or hostname) to an ISO 3166-1
// alpha-2 country code using only the offline GeoIP chain.
//
// Unlike resolveRegionCode it never falls back to name recognition: the suggest
// endpoint only offers a name when the geo chain succeeds. Any failing step
// (empty server, DNS failure, no country record) yields an empty string.
//
// The offline lookup performs no network I/O, so no context/timeout is needed;
// d.recognize is unused here.
func resolveRegionGeoOnly(server string, d regionResolverDeps) string {
	return lookupCountryForServer(server, d)
}
