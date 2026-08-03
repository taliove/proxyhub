package filter

import (
	"testing"

	"github.com/taliove/proxyhub/internal/subscription"
)

// Three-state availability semantics (issue #32): DetectionKind=="" means the
// node was never detected. Unchecked nodes are shippable (FilterAvailable admits
// them) but rank after all detected nodes in latency comparisons (+infinity).
// Fixtures use example.com servers and the all-zero UUID only.

const zeroUUID = "00000000-0000-0000-0000-000000000000"

func detectedNode(name, server string, latency int) *subscription.Node {
	return &subscription.Node{
		Name:          name,
		Server:        server,
		Port:          443,
		UUID:          zeroUUID,
		Available:     true,
		Latency:       latency,
		DetectionKind: subscription.DetectionKindHealth,
	}
}

func uncheckedNode(name, server string) *subscription.Node {
	return &subscription.Node{
		Name:   name,
		Server: server,
		Port:   443,
		UUID:   zeroUUID,
		// Available=false, Latency=0, DetectionKind="" — never detected.
	}
}

func TestNodeUnchecked(t *testing.T) {
	cases := []struct {
		name string
		node *subscription.Node
		want bool
	}{
		{"empty DetectionKind", &subscription.Node{}, true},
		{"health-detected", &subscription.Node{DetectionKind: subscription.DetectionKindHealth}, false},
		{"real-detected", &subscription.Node{DetectionKind: subscription.DetectionKindReal}, false},
	}
	for _, tc := range cases {
		if got := tc.node.Unchecked(); got != tc.want {
			t.Errorf("%s: Unchecked() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestFilterAvailable_ThreeState(t *testing.T) {
	healthDead := detectedNode("health-dead", "dead-h.example.com", 0)
	healthDead.Available = false
	realDead := detectedNode("real-dead", "dead-r.example.com", 0)
	realDead.Available = false
	realDead.DetectionKind = subscription.DetectionKindReal
	selfDead := &subscription.Node{
		Name:          "self-dead",
		Server:        "self.example.com",
		Port:          443,
		Source:        subscription.SourceSelfHosted,
		Available:     false,
		DetectionKind: subscription.DetectionKindHealth,
	}

	nodes := []*subscription.Node{
		uncheckedNode("unchecked", "new.example.com"),
		healthDead,
		realDead,
		detectedNode("available", "up.example.com", 120),
		selfDead,
	}

	result := FilterAvailable(nodes)

	want := map[string]bool{
		"unchecked":   true,  // never detected: admitted
		"health-dead": false, // confirmed dead by health check: filtered
		"real-dead":   false, // confirmed dead by real probe: filtered
		"available":   true,  // detected available: admitted
		"self-dead":   true,  // self-hosted exemption: retained even when dead
	}
	if len(result) != 3 {
		t.Fatalf("len(result) = %d, want 3 (unchecked + available + self-dead)", len(result))
	}
	for name, admitted := range want {
		if got := containsNode(result, name); got != admitted {
			t.Errorf("node %s admitted = %v, want %v", name, got, admitted)
		}
	}
}

func TestSortByLatency_UncheckedLast(t *testing.T) {
	unchecked := uncheckedNode("unchecked", "new.example.com")
	nodes := []*subscription.Node{
		unchecked, // Latency=0 must NOT float to the front
		detectedNode("slow", "slow.example.com", 300),
		detectedNode("fast", "fast.example.com", 100),
	}

	f := NewFilter(10, false)
	f.sortByLatency(nodes)

	if nodes[0].Name != "fast" || nodes[1].Name != "slow" || nodes[2].Name != "unchecked" {
		t.Errorf("order = %s, %s, %s; want fast, slow, unchecked",
			nodes[0].Name, nodes[1].Name, nodes[2].Name)
	}
	if unchecked.Latency != 0 {
		t.Errorf("unchecked node Latency rewritten to %d, want 0 (compare-only, no data mutation)", unchecked.Latency)
	}
}

func TestSortByLatency_NodeKeyTieBreak(t *testing.T) {
	// Detected nodes with identical latency: NodeKey (server:port) is the
	// deterministic tie-breaker, so equal-latency runs sort lexicographically.
	nodes := []*subscription.Node{
		detectedNode("c", "c.example.com", 200),
		detectedNode("a", "a.example.com", 200),
		detectedNode("b", "b.example.com", 200),
	}

	f := NewFilter(10, false)
	f.sortByLatency(nodes)

	if nodes[0].Name != "a" || nodes[1].Name != "b" || nodes[2].Name != "c" {
		t.Errorf("order = %s, %s, %s; want a, b, c (NodeKey tie-break on equal latency)",
			nodes[0].Name, nodes[1].Name, nodes[2].Name)
	}
}

func TestSelectBestByRegion_UncheckedLast(t *testing.T) {
	// nodesPerRegion=2: two detected nodes must keep the slots; the unchecked
	// node (Latency=0) must not displace them.
	f := NewFilter(2, false)
	nodes := []*subscription.Node{
		uncheckedNode("unchecked", "new.example.com"),
		detectedNode("hk-1", "a.example.com", 200),
		detectedNode("hk-2", "b.example.com", 100),
	}
	for _, n := range nodes {
		n.Region = "HK"
	}

	result := f.selectBestByRegion(nodes)

	if len(result) != 2 {
		t.Fatalf("len(result) = %d, want 2", len(result))
	}
	if !containsNode(result, "hk-1") || !containsNode(result, "hk-2") {
		t.Error("detected nodes must keep the per-region slots")
	}
	if containsNode(result, "unchecked") {
		t.Error("unchecked node must not displace detected nodes within the cap")
	}
}

func TestSelectBestByRegion_UncheckedBackfills(t *testing.T) {
	// Region has fewer detected nodes than the cap: the unchecked node fills
	// the remaining slot (it is shippable, just ranked last).
	f := NewFilter(2, false)
	nodes := []*subscription.Node{
		uncheckedNode("unchecked", "new.example.com"),
		detectedNode("hk-1", "a.example.com", 100),
	}
	for _, n := range nodes {
		n.Region = "HK"
	}

	result := f.selectBestByRegion(nodes)

	if len(result) != 2 {
		t.Fatalf("len(result) = %d, want 2 (detected + unchecked backfill)", len(result))
	}
	if !containsNode(result, "unchecked") {
		t.Error("unchecked node should backfill when the region is short of the cap")
	}
}

func TestDeduplicateNodes_DetectedBeatsUnchecked(t *testing.T) {
	// Same NodeKey: the detected node wins even with a worse Latency, because
	// the unchecked node compares as +infinity.
	f := NewFilter(0, true)
	nodes := []*subscription.Node{
		uncheckedNode("unchecked", "dup.example.com"),      // Latency=0
		detectedNode("detected", "dup.example.com", 9000),  // far worse Latency
	}

	result := f.deduplicateNodes(nodes)

	if len(result) != 1 {
		t.Fatalf("len(result) = %d, want 1", len(result))
	}
	if result[0].Name != "detected" {
		t.Errorf("kept %s, want detected (unchecked compares as +infinity)", result[0].Name)
	}
}

func TestDeduplicateNodes_BothUnchecked_FirstSeenWins(t *testing.T) {
	// Both unchecked (both +infinity, same NodeKey): the first-seen node wins,
	// matching the historical strict-< semantics; the NodeKey tie-breaker keeps
	// the comparison a deterministic total order.
	f := NewFilter(0, true)
	nodes := []*subscription.Node{
		uncheckedNode("first", "dup.example.com"),
		uncheckedNode("second", "dup.example.com"),
	}

	result := f.deduplicateNodes(nodes)

	if len(result) != 1 {
		t.Fatalf("len(result) = %d, want 1", len(result))
	}
	if result[0].Name != "first" {
		t.Errorf("kept %s, want first (first-seen wins on a full tie)", result[0].Name)
	}
}
