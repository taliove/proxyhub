package airporttest

import (
	"testing"

	"github.com/taliove/proxyhub/internal/subscription"
)

func TestSampleNodes_Full(t *testing.T) {
	nodes := []*subscription.Node{
		{Name: "HK1", Region: "HK"},
		{Name: "HK2", Region: "HK"},
	}
	sampled := SampleNodes(nodes, true)
	if len(sampled) != len(nodes) {
		t.Errorf("full=true should return all nodes, got %d want %d", len(sampled), len(nodes))
	}
}

func TestSampleNodes_Empty(t *testing.T) {
	sampled := SampleNodes([]*subscription.Node{}, false)
	if len(sampled) != 0 {
		t.Errorf("empty input should return empty, got %d", len(sampled))
	}
}

func TestSampleNodes_PriorityRegionQuota(t *testing.T) {
	// HK优先区配额5,给8个应该抽5个
	nodes := make([]*subscription.Node, 8)
	for i := range nodes {
		nodes[i] = &subscription.Node{Name: "HK", Region: "HK"}
	}
	sampled := SampleNodes(nodes, false)
	if len(sampled) != 5 {
		t.Errorf("HK priority quota=5, got %d", len(sampled))
	}
}

func TestSampleNodes_DefaultRegionQuota(t *testing.T) {
	// 非优先区配额2,给5个应该抽2个
	nodes := make([]*subscription.Node, 5)
	for i := range nodes {
		nodes[i] = &subscription.Node{Name: "JP", Region: "JP"}
	}
	sampled := SampleNodes(nodes, false)
	if len(sampled) != 2 {
		t.Errorf("non-priority quota=2, got %d", len(sampled))
	}
}

func TestSampleNodes_UnknownRegion(t *testing.T) {
	// 无地区信息(Region="")按其他区配额2
	nodes := make([]*subscription.Node, 5)
	for i := range nodes {
		nodes[i] = &subscription.Node{Name: "unknown", Region: ""}
	}
	sampled := SampleNodes(nodes, false)
	if len(sampled) != 2 {
		t.Errorf("unknown region quota=2, got %d", len(sampled))
	}
}

func TestSampleNodes_RandomDistribution(t *testing.T) {
	// 层内随机:多次抽样,每个节点被选概率非零
	nodes := make([]*subscription.Node, 10)
	for i := range nodes {
		nodes[i] = &subscription.Node{Name: "HK", Region: "HK"}
	}
	selected := make(map[*subscription.Node]int)
	for run := 0; run < 100; run++ {
		sampled := SampleNodes(nodes, false)
		for _, n := range sampled {
			selected[n]++
		}
	}
	// 断言:至少8/10节点被选过(允许极低概率未被选,不要求100%)
	if len(selected) < 8 {
		t.Errorf("randomness failed: only %d/10 nodes selected in 100 runs", len(selected))
	}
}

func TestSampleNodes_MultiRegion(t *testing.T) {
	// 混合地区:HK 8个抽5,SG 3个全保留,JP 5个抽2
	nodes := []*subscription.Node{
		{Name: "HK1", Region: "HK"}, {Name: "HK2", Region: "HK"}, {Name: "HK3", Region: "HK"},
		{Name: "HK4", Region: "HK"}, {Name: "HK5", Region: "HK"}, {Name: "HK6", Region: "HK"},
		{Name: "HK7", Region: "HK"}, {Name: "HK8", Region: "HK"},
		{Name: "SG1", Region: "SG"}, {Name: "SG2", Region: "SG"}, {Name: "SG3", Region: "SG"},
		{Name: "JP1", Region: "JP"}, {Name: "JP2", Region: "JP"}, {Name: "JP3", Region: "JP"},
		{Name: "JP4", Region: "JP"}, {Name: "JP5", Region: "JP"},
	}
	sampled := SampleNodes(nodes, false)
	// 期望:HK 5 + SG 3 + JP 2 = 10
	if len(sampled) != 10 {
		t.Errorf("multi-region sampling got %d, want 10 (HK5+SG3+JP2)", len(sampled))
	}
	// 统计各地区
	regionCount := make(map[string]int)
	for _, n := range sampled {
		regionCount[n.Region]++
	}
	if regionCount["HK"] != 5 {
		t.Errorf("HK sampled %d, want 5", regionCount["HK"])
	}
	if regionCount["SG"] != 3 {
		t.Errorf("SG sampled %d, want 3", regionCount["SG"])
	}
	if regionCount["JP"] != 2 {
		t.Errorf("JP sampled %d, want 2", regionCount["JP"])
	}
}
