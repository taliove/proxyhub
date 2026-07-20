package server

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

// TestToNodeViews_UnlockLevelRegionPassthrough /nodes 视图透出解锁 level/region;
// generic 结果(空)不泄漏两键。
func TestToNodeViews_UnlockLevelRegionPassthrough(t *testing.T) {
	node := &subscription.Node{Name: "hk01", Server: "example.com", Port: 443, Type: "vmess", Source: "airport"}
	key := node.NodeKey()
	unlock := map[string][]store.DetectionResultView{
		key: {
			{TargetName: "Netflix", Available: true, Latency: 88, Level: "full", Region: "US"},
			{TargetName: "connectivity", Available: true, Latency: 30},
		},
	}

	views := toNodeViews([]*subscription.Node{node}, nil, unlock, nil)
	if len(views) != 1 {
		t.Fatalf("views len = %d, want 1", len(views))
	}

	nf, ok := views[0].UnlockResults["Netflix"]
	if !ok {
		t.Fatalf("Netflix unlock result missing")
	}
	if nf.Level != "full" || nf.Region != "US" {
		t.Errorf("netflix view level/region = %q/%q, want full/US", nf.Level, nf.Region)
	}

	gen := views[0].UnlockResults["connectivity"]
	if gen.Level != "" || gen.Region != "" {
		t.Errorf("generic view level/region = %q/%q, want empty", gen.Level, gen.Region)
	}

	// JSON omitempty:generic 目标不出现 level/region。
	b, err := json.Marshal(gen)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if s := string(b); strings.Contains(s, `"level"`) || strings.Contains(s, `"region"`) {
		t.Errorf("generic view JSON leaks level/region: %s", s)
	}
}
