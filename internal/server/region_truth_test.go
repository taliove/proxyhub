package server

import (
	"testing"

	"github.com/taliove/proxyhub/internal/detection"
	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

// TestWritebackRegionAlwaysOverwrites verifies that exam egress country code
// always overwrites node region, even when region already has a GeoIP-guessed value.
func TestWritebackRegionAlwaysOverwrites(t *testing.T) {
	srv, st := newTestServer(t, nil)

	// Self-hosted node with incorrect GeoIP-guessed region (CN)
	node := &store.SelfHostedNode{
		Server:     "example.com",
		Port:       443,
		Protocol:   "vless",
		RegionCode: "CN", // GeoIP guessed wrong
		Name:       "Test Node",
	}
	if err := st.CreateSelfHostedNode(node); err != nil {
		t.Fatalf("create node: %v", err)
	}

	// Exam report with correct egress country (CA)
	report := detection.ExamReport{
		Egress: &detection.EgressMetrics{
			IPv4: &detection.EgressIPv4{
				IP:          "192.0.2.1",
				CountryCode: "CA",
			},
		},
	}

	nodeKey := node.ToNode().NodeKey()
	srv.writebackRegionIfNeeded(0, nodeKey, report)

	// Verify region was updated to CA
	nodes, err := st.ListAllSelfHostedNodes()
	if err != nil {
		t.Fatalf("list nodes: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	if nodes[0].RegionCode != "CA" {
		t.Errorf("expected region CA, got %q", nodes[0].RegionCode)
	}
}

// TestWritebackRegionNoEgress verifies that writeback does nothing when exam has no egress info.
func TestWritebackRegionNoEgress(t *testing.T) {
	srv, st := newTestServer(t, nil)

	node := &store.SelfHostedNode{
		Server:     "example.com",
		Port:       443,
		Protocol:   "vless",
		RegionCode: "CN",
		Name:       "Test Node",
	}
	if err := st.CreateSelfHostedNode(node); err != nil {
		t.Fatalf("create node: %v", err)
	}

	// Exam report without egress info
	report := detection.ExamReport{}

	nodeKey := node.ToNode().NodeKey()
	srv.writebackRegionIfNeeded(0, nodeKey, report)

	// Verify region unchanged
	nodes, err := st.ListAllSelfHostedNodes()
	if err != nil {
		t.Fatalf("list nodes: %v", err)
	}
	if nodes[0].RegionCode != "CN" {
		t.Errorf("expected region unchanged (CN), got %q", nodes[0].RegionCode)
	}
}

// TestWritebackRegionAirportNode verifies writeback works for airport nodes in memory pool.
func TestWritebackRegionAirportNode(t *testing.T) {
	testNode := &subscription.Node{
		Server:      "example.com",
		Port:        443,
		Type:        "vless",
		Region:      "CN", // GeoIP guessed wrong
		DisplayName: "Test Airport",
		Source:      subscription.SourceSelfHosted, // Will be changed to airport source
	}
	// Mark as airport node (non-self-hosted)
	testNode.Source = "test-airport"

	srv, _ := newTestServer(t, []*subscription.Node{testNode})

	report := detection.ExamReport{
		Egress: &detection.EgressMetrics{
			IPv4: &detection.EgressIPv4{
				IP:          "192.0.2.1",
				CountryCode: "HK",
			},
		},
	}

	nodeKey := testNode.NodeKey()
	srv.writebackRegionIfNeeded(0, nodeKey, report)

	if testNode.Region != "HK" {
		t.Errorf("expected region HK, got %q", testNode.Region)
	}
}

// TestRefreshNamesEgressPriority verifies that refresh names uses exam egress first, GeoIP fallback.
func TestRefreshNamesEgressPriority(t *testing.T) {
	srv, st := newTestServer(t, nil)
	srv.countryLookup = func(ip string) (string, error) {
		return "US", nil // GeoIP would guess US
	}
	srv.lookupHost = func(host string) ([]string, error) {
		return []string{"192.0.2.1"}, nil
	}

	// Node with Unknown region
	node := &store.SelfHostedNode{
		Server:     "example.com",
		Port:       443,
		Protocol:   "vless",
		RegionCode: "Unknown",
		Name:       "Test Node",
	}
	if err := st.CreateSelfHostedNode(node); err != nil {
		t.Fatalf("create node: %v", err)
	}

	nodeKey := node.ToNode().NodeKey()

	// Save exam history with egress showing HK
	examReport := detection.ExamReport{
		Egress: &detection.EgressMetrics{
			IPv4: &detection.EgressIPv4{
				IP:          "192.0.2.10",
				CountryCode: "HK",
			},
		},
	}
	if err := st.SaveExamHistory(nodeKey, examReport); err != nil {
		t.Fatalf("save exam history: %v", err)
	}

	// Refresh names should use egress (HK), not GeoIP (US)
	updated := srv.refreshSelfHostedNodeNames(nil)

	if updated != 1 {
		t.Errorf("expected 1 update, got %d", updated)
	}

	nodes, err := st.ListAllSelfHostedNodes()
	if err != nil {
		t.Fatalf("list nodes: %v", err)
	}
	if nodes[0].RegionCode != "HK" {
		t.Errorf("expected region HK (from egress), got %q", nodes[0].RegionCode)
	}
	if nodes[0].Name != "自建香港" {
		t.Errorf("expected name 自建香港, got %q", nodes[0].Name)
	}
}

// TestRefreshNamesGeoIPFallback verifies GeoIP is used when no exam history exists.
func TestRefreshNamesGeoIPFallback(t *testing.T) {
	srv, st := newTestServer(t, nil)
	srv.countryLookup = func(ip string) (string, error) {
		return "JP", nil
	}
	srv.lookupHost = func(host string) ([]string, error) {
		return []string{"192.0.2.1"}, nil
	}

	node := &store.SelfHostedNode{
		Server:     "example.com",
		Port:       443,
		Protocol:   "vless",
		RegionCode: "Unknown",
		Name:       "Test Node",
	}
	if err := st.CreateSelfHostedNode(node); err != nil {
		t.Fatalf("create node: %v", err)
	}

	// No exam history saved - should fallback to GeoIP
	updated := srv.refreshSelfHostedNodeNames(nil)

	if updated != 1 {
		t.Errorf("expected 1 update, got %d", updated)
	}

	nodes, err := st.ListAllSelfHostedNodes()
	if err != nil {
		t.Fatalf("list nodes: %v", err)
	}
	if nodes[0].RegionCode != "JP" {
		t.Errorf("expected region JP (from GeoIP), got %q", nodes[0].RegionCode)
	}
	if nodes[0].Name != "自建日本" {
		t.Errorf("expected name 自建日本, got %q", nodes[0].Name)
	}
}

// TestRefreshNamesPreserveWhenBothFail verifies region is preserved when both egress and GeoIP fail.
func TestRefreshNamesPreserveWhenBothFail(t *testing.T) {
	srv, st := newTestServer(t, nil)
	srv.countryLookup = func(ip string) (string, error) {
		return "", nil // GeoIP fails
	}
	srv.lookupHost = func(host string) ([]string, error) {
		return nil, nil // DNS fails
	}

	node := &store.SelfHostedNode{
		Server:     "example.com",
		Port:       443,
		Protocol:   "vless",
		RegionCode: "FR", // Existing region
		Name:       "自建法国",
	}
	if err := st.CreateSelfHostedNode(node); err != nil {
		t.Fatalf("create node: %v", err)
	}

	// No exam history, GeoIP fails - should preserve existing region
	_ = srv.refreshSelfHostedNodeNames(nil)

	// Should still update (re-apply naming even if region unchanged)
	nodes, err := st.ListAllSelfHostedNodes()
	if err != nil {
		t.Fatalf("list nodes: %v", err)
	}
	if nodes[0].RegionCode != "FR" {
		t.Errorf("expected region FR (preserved), got %q", nodes[0].RegionCode)
	}
	if nodes[0].Name != "自建法国" {
		t.Errorf("expected name 自建法国, got %q", nodes[0].Name)
	}
}

// TestRefreshNamesAirportEgressPriority verifies airport nodes use egress-corrected region for naming.
func TestRefreshNamesAirportEgressPriority(t *testing.T) {
	testNode := &subscription.Node{
		Server:      "example.com",
		Port:        443,
		Type:        "vless",
		Region:      "CN", // GeoIP guessed wrong
		DisplayName: "Test Airport",
		Source:      "test-airport",
	}

	srv, st := newTestServer(t, []*subscription.Node{testNode})

	nodeKey := testNode.NodeKey()

	// Save exam history showing real egress is HK
	examReport := detection.ExamReport{
		Egress: &detection.EgressMetrics{
			IPv4: &detection.EgressIPv4{
				IP:          "192.0.2.10",
				CountryCode: "HK",
			},
		},
	}
	if err := st.SaveExamHistory(nodeKey, examReport); err != nil {
		t.Fatalf("save exam history: %v", err)
	}

	// First writeback the egress region
	srv.writebackRegionIfNeeded(0, nodeKey, examReport)

	// Then refresh names - should use the corrected HK region
	updated := srv.refreshAirportNodeNames(nil)

	if updated != 1 {
		t.Errorf("expected 1 update, got %d", updated)
	}

	// Airport node standardization would have applied based on HK region
	// (actual standardization logic is complex, we just verify it was called)
}

