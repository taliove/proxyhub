package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

func TestHandleRefreshNames_AirportNodes(t *testing.T) {
	// Mock node pool with test nodes
	nodes := []*subscription.Node{
		{
			Name:   "HK-01",
			Server: "hk1.example.com",
			Port:   443,
			Type:   "vmess",
			UUID:   "00000000-0000-0000-0000-000000000001",
			Source: "TestAirport",
			Region: "HK",
		},
		{
			Name:   "SG-01",
			Server: "sg1.example.com",
			Port:   443,
			Type:   "vmess",
			UUID:   "00000000-0000-0000-0000-000000000002",
			Source: "TestAirport",
			Region: "SG",
		},
	}

	srv, _ := newTestServer(t, nodes)

	// Request refresh for all nodes
	body := bytes.NewReader([]byte(`{}`))
	req := httptest.NewRequest(http.MethodPost, "/api/nodes/refresh-names", body)
	rec := httptest.NewRecorder()

	srv.handleRefreshNames(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Updated int `json:"updated"`
		Total   int `json:"total"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.Total != 2 {
		t.Errorf("expected total=2, got %d", resp.Total)
	}
	if resp.Updated != 2 {
		t.Errorf("expected updated=2, got %d", resp.Updated)
	}

	// Verify nodes have DisplayName set
	updatedNodes := srv.nodes.Nodes()
	for _, n := range updatedNodes {
		if n.DisplayName == "" {
			t.Errorf("node %s should have DisplayName set", n.NodeKey())
		}
	}
}

func TestHandleRefreshNames_SelectedNodes(t *testing.T) {
	nodes := []*subscription.Node{
		{
			Name:   "HK-01",
			Server: "hk1.example.com",
			Port:   443,
			Type:   "vmess",
			UUID:   "00000000-0000-0000-0000-000000000001",
			Source: "TestAirport",
			Region: "HK",
		},
		{
			Name:   "SG-01",
			Server: "sg1.example.com",
			Port:   443,
			Type:   "vmess",
			UUID:   "00000000-0000-0000-0000-000000000002",
			Source: "TestAirport",
			Region: "SG",
		},
	}

	srv, _ := newTestServer(t, nodes)

	// Request refresh for only one node
	body := bytes.NewReader([]byte(`{"node_keys":["hk1.example.com:443"]}`))
	req := httptest.NewRequest(http.MethodPost, "/api/nodes/refresh-names", body)
	rec := httptest.NewRecorder()

	srv.handleRefreshNames(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Updated int `json:"updated"`
		Total   int `json:"total"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.Total != 1 {
		t.Errorf("expected total=1, got %d", resp.Total)
	}
	if resp.Updated != 1 {
		t.Errorf("expected updated=1, got %d", resp.Updated)
	}
}

func TestHandleRefreshNames_SelfHostedNodes(t *testing.T) {
	srv, st := newTestServer(t, nil)

	// Create self-hosted node with empty name and Unknown region
	selfNode := &store.SelfHostedNode{
		Name:       "",
		Server:     "1.2.3.4",
		Port:       443,
		Protocol:   "vmess",
		UUID:       "00000000-0000-0000-0000-000000000001",
		RegionCode: "Unknown",
		Enabled:    true,
	}

	// Mock countryLookup to return a test region
	srv.countryLookup = func(ip string) (string, error) {
		if ip == "1.2.3.4" {
			return "HK", nil
		}
		return "", nil
	}

	// Save self-hosted node
	if err := st.CreateSelfHostedNode(selfNode); err != nil {
		t.Fatalf("create self node: %v", err)
	}

	// Request refresh
	body := bytes.NewReader([]byte(`{"node_keys":["1.2.3.4:443"]}`))
	req := httptest.NewRequest(http.MethodPost, "/api/nodes/refresh-names", body)
	rec := httptest.NewRecorder()

	srv.handleRefreshNames(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Updated int `json:"updated"`
		Total   int `json:"total"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.Total != 1 {
		t.Errorf("expected total=1, got %d", resp.Total)
	}

	// Verify node region and name were updated
	nodes, err := st.ListAllSelfHostedNodes()
	if err != nil {
		t.Fatalf("list self nodes: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}

	updated := nodes[0]
	if updated.RegionCode != "HK" {
		t.Errorf("expected region HK, got %s", updated.RegionCode)
	}
	if updated.Name == "" {
		t.Errorf("expected name to be set")
	}
	if !strings.Contains(updated.Name, "香港") {
		t.Errorf("expected name to contain 香港, got %s", updated.Name)
	}
}

func TestHandleRefreshNames_AfterRegionWriteback(t *testing.T) {
	// Test that standardization uses updated region after write-back
	nodes := []*subscription.Node{
		{
			Name:   "Unknown-Node",
			Server: "test.example.com",
			Port:   443,
			Type:   "vmess",
			UUID:   "00000000-0000-0000-0000-000000000001",
			Source: "TestAirport",
			Region: "Unknown",
		},
	}

	srv, _ := newTestServer(t, nodes)

	// Simulate region write-back (as if from health check)
	for _, n := range srv.nodes.Nodes() {
		if n.NodeKey() == "test.example.com:443" {
			n.Region = "HK" // Write-back sets real region
			break
		}
	}

	// Now refresh names - should use the updated HK region
	body := bytes.NewReader([]byte(`{}`))
	req := httptest.NewRequest(http.MethodPost, "/api/nodes/refresh-names", body)
	rec := httptest.NewRecorder()

	srv.handleRefreshNames(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify DisplayName reflects HK region
	updatedNodes := srv.nodes.Nodes()
	for _, n := range updatedNodes {
		if n.NodeKey() == "test.example.com:443" {
			if n.DisplayName == "" {
				t.Error("DisplayName should be set after refresh")
			}
			// DisplayName should contain HK-related info (depends on template)
			if !strings.Contains(n.DisplayName, "HK") && !strings.Contains(n.DisplayName, "香港") {
				t.Errorf("DisplayName should reflect HK region, got: %s", n.DisplayName)
			}
		}
	}
}

func TestHandleRefreshNames_SelfHosted_OverwritesCustomName(t *testing.T) {
	// Test that refresh always overwrites custom names for self-hosted nodes when region is known
	srv, st := newTestServer(t, nil)

	// Create self-hosted node with custom name and known region
	selfNode := &store.SelfHostedNode{
		Name:       "My Custom HK Node",
		Server:     "2.3.4.5",
		Port:       443,
		Protocol:   "vmess",
		UUID:       "00000000-0000-0000-0000-000000000002",
		RegionCode: "HK",
		Enabled:    true,
	}

	// Mock countryLookup to return same region
	srv.countryLookup = func(ip string) (string, error) {
		if ip == "2.3.4.5" {
			return "HK", nil
		}
		return "", nil
	}

	if err := st.CreateSelfHostedNode(selfNode); err != nil {
		t.Fatalf("create self node: %v", err)
	}

	// Request refresh - should overwrite custom name
	body := bytes.NewReader([]byte(`{"node_keys":["2.3.4.5:443"]}`))
	req := httptest.NewRequest(http.MethodPost, "/api/nodes/refresh-names", body)
	rec := httptest.NewRecorder()

	srv.handleRefreshNames(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify node name was overwritten to standard format
	nodes, err := st.ListAllSelfHostedNodes()
	if err != nil {
		t.Fatalf("list self nodes: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}

	updated := nodes[0]
	if updated.Name != "自建香港" {
		t.Errorf("expected name='自建香港', got %s (custom name should be overwritten)", updated.Name)
	}
}

func TestHandleRefreshNames_SelfHosted_PreservesUnknownRegion(t *testing.T) {
	// Test that refresh preserves name when region remains Unknown after resolution
	srv, st := newTestServer(t, nil)

	// Create self-hosted node with custom name and Unknown region
	// Use a name with no letters to avoid any region pattern matching
	selfNode := &store.SelfHostedNode{
		Name:       "Proxy-001",
		Server:     "192.0.2.1", // TEST-NET-1, reserved for documentation, not in GeoIP
		Port:       8443,
		Protocol:   "vmess",
		UUID:       "00000000-0000-0000-0000-000000000003",
		RegionCode: "Unknown",
		Enabled:    true,
	}

	// Mock countryLookup to explicitly return empty (ensuring GeoIP lookup fails)
	srv.countryLookup = func(ip string) (string, error) {
		return "", nil
	}

	if err := st.CreateSelfHostedNode(selfNode); err != nil {
		t.Fatalf("create self node: %v", err)
	}

	// Request refresh - should preserve name when region remains Unknown
	body := bytes.NewReader([]byte(`{"node_keys":["192.0.2.1:8443"]}`))
	req := httptest.NewRequest(http.MethodPost, "/api/nodes/refresh-names", body)
	rec := httptest.NewRecorder()

	srv.handleRefreshNames(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify node name was preserved
	nodes, err := st.ListAllSelfHostedNodes()
	if err != nil {
		t.Fatalf("list self nodes: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}

	updated := nodes[0]
	if updated.Name != "Proxy-001" {
		t.Errorf("expected name preserved when region unknown, got %s", updated.Name)
	}
	if updated.RegionCode != "Unknown" {
		t.Errorf("expected region to remain Unknown, got %s", updated.RegionCode)
	}
}
