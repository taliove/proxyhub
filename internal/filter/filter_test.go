package filter

import (
	"testing"

	"github.com/taliove/proxyhub/internal/subscription"
)

func TestDeduplicateNodes(t *testing.T) {
	nodes := []*subscription.Node{
		{Name: "HK-1", Server: "1.2.3.4", Port: 443, Latency: 100, Available: true},
		{Name: "HK-2", Server: "1.2.3.4", Port: 443, Latency: 150, Available: true}, // 重复，延迟更高
		{Name: "HK-3", Server: "5.6.7.8", Port: 443, Latency: 120, Available: true},
	}

	f := NewFilter(10, true)
	result := f.deduplicateNodes(nodes)

	if len(result) != 2 {
		t.Fatalf("len(result) = %d, want 2", len(result))
	}

	// 检查保留的是延迟更低的
	for _, node := range result {
		if node.Server == "1.2.3.4" && node.Latency != 100 {
			t.Errorf("duplicate kept wrong node: latency %d, want 100", node.Latency)
		}
	}
}

func TestSelectBestByRegion(t *testing.T) {
	nodes := []*subscription.Node{
		{Name: "HK-1", Region: "HK", Latency: 50, Available: true},
		{Name: "HK-2", Region: "HK", Latency: 80, Available: true},
		{Name: "HK-3", Region: "HK", Latency: 100, Available: true},
		{Name: "HK-4", Region: "HK", Latency: 120, Available: true},
		{Name: "JP-1", Region: "JP", Latency: 60, Available: true},
		{Name: "JP-2", Region: "JP", Latency: 90, Available: true},
	}

	// 每个地区保留 2 个
	f := NewFilter(2, false)
	result := f.selectBestByRegion(nodes)

	if len(result) != 4 {
		t.Fatalf("len(result) = %d, want 4 (2 HK + 2 JP)", len(result))
	}

	// 统计各地区节点数
	counts := make(map[string]int)
	for _, node := range result {
		counts[node.Region]++
	}

	if counts["HK"] != 2 {
		t.Errorf("HK count = %d, want 2", counts["HK"])
	}
	if counts["JP"] != 2 {
		t.Errorf("JP count = %d, want 2", counts["JP"])
	}

	// 检查是否选了延迟最低的
	for _, node := range result {
		if node.Region == "HK" && node.Latency > 80 {
			t.Errorf("HK node latency %d > 80, should keep only fastest 2", node.Latency)
		}
		if node.Region == "JP" && node.Latency > 90 {
			t.Errorf("JP node latency %d > 90, should keep only fastest 2", node.Latency)
		}
	}
}

func TestSortByLatency(t *testing.T) {
	nodes := []*subscription.Node{
		{Name: "Node-3", Latency: 300, DetectionKind: subscription.DetectionKindHealth},
		{Name: "Node-1", Latency: 100, DetectionKind: subscription.DetectionKindHealth},
		{Name: "Node-2", Latency: 200, DetectionKind: subscription.DetectionKindHealth},
	}

	f := NewFilter(10, false)
	f.sortByLatency(nodes)

	if nodes[0].Latency != 100 || nodes[1].Latency != 200 || nodes[2].Latency != 300 {
		t.Errorf("sort failed: got latencies %d, %d, %d", nodes[0].Latency, nodes[1].Latency, nodes[2].Latency)
	}
}

func TestApply_FullPipeline(t *testing.T) {
	nodes := []*subscription.Node{
		// 香港：5 个节点，其中 2 个重复
		{Name: "HK-1", Server: "1.1.1.1", Port: 443, Region: "HK", Latency: 50, Available: true, DetectionKind: subscription.DetectionKindHealth},
		{Name: "HK-1-dup", Server: "1.1.1.1", Port: 443, Region: "HK", Latency: 60, Available: true, DetectionKind: subscription.DetectionKindHealth},
		{Name: "HK-2", Server: "1.1.1.2", Port: 443, Region: "HK", Latency: 80, Available: true, DetectionKind: subscription.DetectionKindHealth},
		{Name: "HK-3", Server: "1.1.1.3", Port: 443, Region: "HK", Latency: 100, Available: true, DetectionKind: subscription.DetectionKindHealth},
		{Name: "HK-4", Server: "1.1.1.4", Port: 443, Region: "HK", Latency: 120, Available: true, DetectionKind: subscription.DetectionKindHealth},
		// 日本：2 个节点
		{Name: "JP-1", Server: "2.2.2.1", Port: 443, Region: "JP", Latency: 70, Available: true, DetectionKind: subscription.DetectionKindHealth},
		{Name: "JP-2", Server: "2.2.2.2", Port: 443, Region: "JP", Latency: 90, Available: true, DetectionKind: subscription.DetectionKindHealth},
	}

	f := NewFilter(3, true) // 去重 + 每个地区保留 3 个
	result := f.Apply(nodes)

	// 去重后 6 个，香港保留 3 个 + 日本保留 2 个（只有 2 个）= 5 个
	if len(result) != 5 {
		t.Fatalf("len(result) = %d, want 5", len(result))
	}

	// 检查排序（按延迟）
	for i := 1; i < len(result); i++ {
		if result[i].Latency < result[i-1].Latency {
			t.Error("result not sorted by latency")
			break
		}
	}

	// 第一个节点应该是延迟最低的
	if result[0].Latency != 50 {
		t.Errorf("first node latency = %d, want 50", result[0].Latency)
	}
}

func TestFilterAvailable(t *testing.T) {
	nodes := []*subscription.Node{
		{Name: "Node-1", Available: true},
		{Name: "Node-2", Available: false, DetectionKind: subscription.DetectionKindHealth}, // 已确认死亡
		{Name: "Node-3", Available: true},
	}

	result := FilterAvailable(nodes)

	if len(result) != 2 {
		t.Fatalf("len(result) = %d, want 2", len(result))
	}

	for _, node := range result {
		if !node.Available {
			t.Errorf("node %s is not available", node.Name)
		}
	}
}

func TestFilterByLatencyThreshold(t *testing.T) {
	nodes := []*subscription.Node{
		{Name: "Node-1", Latency: 50},
		{Name: "Node-2", Latency: 300},
		{Name: "Node-3", Latency: 500},
		{Name: "Node-4", Latency: 600},
	}

	result := FilterByLatencyThreshold(nodes, 500)

	if len(result) != 3 {
		t.Fatalf("len(result) = %d, want 3", len(result))
	}

	for _, node := range result {
		if node.Latency > 500 {
			t.Errorf("node %s latency %d > threshold 500", node.Name, node.Latency)
		}
	}
}

func TestSelectBestByRegion_UnknownRegion(t *testing.T) {
	nodes := []*subscription.Node{
		{Name: "Node-1", Region: "", Latency: 50, Available: true},
		{Name: "Node-2", Region: "", Latency: 60, Available: true},
	}

	f := NewFilter(1, false)
	result := f.selectBestByRegion(nodes)

	// 空地区被归类为 "Unknown"，保留 1 个
	if len(result) != 1 {
		t.Fatalf("len(result) = %d, want 1", len(result))
	}

	if result[0].Latency != 50 {
		t.Errorf("latency = %d, want 50 (fastest)", result[0].Latency)
	}
}
