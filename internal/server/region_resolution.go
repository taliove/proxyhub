package server

import "github.com/taliove/proxyhub/internal/store"

// resolveNodeRegion resolves a node's region code with the priority chain:
// 1. Latest exam egress (ground truth from real exit point)
// 2. Offline GeoIP lookup (guess based on server IP/hostname)
// 3. Preserve existing region if both fail
//
// This is the single source of truth for region resolution, used by both
// exam writeback and refresh names operations.
func (s *Server) resolveNodeRegion(nodeKey string, currentRegion string, node *store.SelfHostedNode) string {
	// Priority 1: Latest exam egress country code (ground truth)
	if latestExam, err := s.st.LatestExamHistory(nodeKey); err == nil && latestExam != nil {
		if latestExam.Report.Egress != nil &&
			latestExam.Report.Egress.IPv4 != nil &&
			latestExam.Report.Egress.IPv4.CountryCode != "" {
			return latestExam.Report.Egress.IPv4.CountryCode
		}
	}

	// Priority 2: GeoIP lookup (fallback guess)
	if node != nil {
		deps := regionResolverDeps{
			lookupHost:    s.lookupHost,
			countryLookup: s.countryLookup,
		}
		if geoCode := resolveRegionGeoOnly(node.Server, deps); geoCode != "" {
			return geoCode
		}
	}

	// Priority 3: Preserve existing region if both fail
	return currentRegion
}
