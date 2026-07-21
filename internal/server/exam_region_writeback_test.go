package server

import (
	"testing"

	"github.com/taliove/proxyhub/internal/detection"
	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

// TestOnExamComplete_WritebackRegionAirportNode tests that exam completion
// writes back egress country code to airport nodes with empty/Unknown region.
func TestOnExamComplete_WritebackRegionAirportNode(t *testing.T) {
	t.Run("empty region gets egress country", func(t *testing.T) {
		testNode := &subscription.Node{
			Name:   "test-node",
			Server: "example.com",
			Port:   443,
			Region: "", // empty region
		}
		s, _ := newTestServer(t, []*subscription.Node{testNode})

		// Trigger exam complete with egress info
		report := detection.ExamReport{
			Egress: &detection.EgressMetrics{
				IPv4: &detection.EgressIPv4{
					CountryCode: "JP",
				},
			},
		}

		s.onExamComplete("example.com:443", report)

		// Verify region was updated in NodeSource
		nodes := s.nodes.Nodes()
		if len(nodes) == 0 {
			t.Fatal("expected node in NodeSource")
		}
		if nodes[0].Region != "JP" {
			t.Errorf("expected region JP, got %s", nodes[0].Region)
		}
	})

	t.Run("Unknown region gets replaced", func(t *testing.T) {
		testNode := &subscription.Node{
			Name:   "test-node",
			Server: "example.com",
			Port:   443,
			Region: "Unknown",
		}
		s, _ := newTestServer(t, []*subscription.Node{testNode})

		report := detection.ExamReport{
			Egress: &detection.EgressMetrics{
				IPv4: &detection.EgressIPv4{
					CountryCode: "US",
				},
			},
		}

		s.onExamComplete("example.com:443", report)

		nodes := s.nodes.Nodes()
		if nodes[0].Region != "US" {
			t.Errorf("expected region US, got %s", nodes[0].Region)
		}
	})

	t.Run("existing region not overwritten", func(t *testing.T) {
		testNode := &subscription.Node{
			Name:   "test-node",
			Server: "example.com",
			Port:   443,
			Region: "HK",
		}
		s, _ := newTestServer(t, []*subscription.Node{testNode})

		report := detection.ExamReport{
			Egress: &detection.EgressMetrics{
				IPv4: &detection.EgressIPv4{
					CountryCode: "JP",
				},
			},
		}

		s.onExamComplete("example.com:443", report)

		// Region should remain unchanged
		nodes := s.nodes.Nodes()
		if nodes[0].Region != "HK" {
			t.Errorf("expected region HK preserved, got %s", nodes[0].Region)
		}
	})

	t.Run("no egress info does not update", func(t *testing.T) {
		testNode := &subscription.Node{
			Name:   "test-node",
			Server: "example.com",
			Port:   443,
			Region: "",
		}
		s, _ := newTestServer(t, []*subscription.Node{testNode})

		report := detection.ExamReport{} // No egress

		s.onExamComplete("example.com:443", report)

		// Region should remain empty
		nodes := s.nodes.Nodes()
		if nodes[0].Region != "" {
			t.Errorf("expected empty region, got %s", nodes[0].Region)
		}
	})
}

// TestOnExamComplete_WritebackRegionSelfHostedNode tests that exam completion
// writes back egress country code to self-hosted nodes via store update.
func TestOnExamComplete_WritebackRegionSelfHostedNode(t *testing.T) {
	t.Run("self-hosted node empty region gets updated", func(t *testing.T) {
		// Create self-hosted node with empty region
		node := &store.SelfHostedNode{
			Name:       "self-node",
			Protocol:   "vmess",
			Server:     "00000000-0000-0000-0000-000000000000.example.com",
			Port:       443,
			UUID:       "00000000-0000-0000-0000-000000000000",
			RegionCode: "",
			Enabled:    true,
		}

		s, st := newTestServer(t, []*subscription.Node{node.ToNode()})

		if err := st.CreateSelfHostedNode(node); err != nil {
			t.Fatal(err)
		}

		report := detection.ExamReport{
			Egress: &detection.EgressMetrics{
				IPv4: &detection.EgressIPv4{
					CountryCode: "SG",
				},
			},
		}

		s.onExamComplete("00000000-0000-0000-0000-000000000000.example.com:443", report)

		// Verify database was updated
		nodes, err := st.ListAllSelfHostedNodes()
		if err != nil {
			t.Fatal(err)
		}
		if len(nodes) == 0 {
			t.Fatal("expected node in database")
		}
		if nodes[0].RegionCode != "SG" {
			t.Errorf("expected region SG in database, got %s", nodes[0].RegionCode)
		}
	})

	t.Run("self-hosted Unknown region gets replaced", func(t *testing.T) {
		node := &store.SelfHostedNode{
			Name:       "self-node",
			Protocol:   "vmess",
			Server:     "00000000-0000-0000-0000-000000000000.example.com",
			Port:       443,
			UUID:       "00000000-0000-0000-0000-000000000000",
			RegionCode: "Unknown",
			Enabled:    true,
		}

		s, st := newTestServer(t, []*subscription.Node{node.ToNode()})

		if err := st.CreateSelfHostedNode(node); err != nil {
			t.Fatal(err)
		}

		report := detection.ExamReport{
			Egress: &detection.EgressMetrics{
				IPv4: &detection.EgressIPv4{
					CountryCode: "DE",
				},
			},
		}

		s.onExamComplete("00000000-0000-0000-0000-000000000000.example.com:443", report)

		nodes, err := st.ListAllSelfHostedNodes()
		if err != nil {
			t.Fatal(err)
		}
		if nodes[0].RegionCode != "DE" {
			t.Errorf("expected region DE, got %s", nodes[0].RegionCode)
		}
	})

	t.Run("self-hosted existing region not overwritten", func(t *testing.T) {
		node := &store.SelfHostedNode{
			Name:       "self-node",
			Protocol:   "vmess",
			Server:     "00000000-0000-0000-0000-000000000000.example.com",
			Port:       443,
			UUID:       "00000000-0000-0000-0000-000000000000",
			RegionCode: "FR",
			Enabled:    true,
		}

		s, st := newTestServer(t, []*subscription.Node{node.ToNode()})

		if err := st.CreateSelfHostedNode(node); err != nil {
			t.Fatal(err)
		}

		report := detection.ExamReport{
			Egress: &detection.EgressMetrics{
				IPv4: &detection.EgressIPv4{
					CountryCode: "US",
				},
			},
		}

		s.onExamComplete("00000000-0000-0000-0000-000000000000.example.com:443", report)

		nodes, err := st.ListAllSelfHostedNodes()
		if err != nil {
			t.Fatal(err)
		}
		if nodes[0].RegionCode != "FR" {
			t.Errorf("expected region FR preserved, got %s", nodes[0].RegionCode)
		}
	})
}



