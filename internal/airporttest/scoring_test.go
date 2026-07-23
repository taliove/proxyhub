package airporttest

import (
	"math"
	"testing"

	"github.com/taliove/proxyhub/internal/subscription"
)

func TestCalculateScore_EmptyNodes(t *testing.T) {
	score, dims := CalculateScore([]*subscription.Node{}, 200, 0, 0)
	if score != 0 {
		t.Errorf("empty nodes should score 0, got %.2f", score)
	}
	if dims.TotalNodes != 0 || dims.AvailableNodes != 0 {
		t.Errorf("empty dims mismatch")
	}
}

func TestCalculateScore_FullAvailability(t *testing.T) {
	nodes := []*subscription.Node{
		{Available: true, Latency: 50, Region: "HK"},
		{Available: true, Latency: 60, Region: "SG"},
	}
	score, dims := CalculateScore(nodes, 200, 0, 10)
	// 可用率100% -> 50分
	if dims.AvailabilityScore != 50 {
		t.Errorf("availability score got %.2f, want 50", dims.AvailabilityScore)
	}
	// 延迟低(<100ms) -> 接近满分30
	if dims.LatencyScore < 29 {
		t.Errorf("latency score too low: %.2f", dims.LatencyScore)
	}
	// 拉取健康100% -> 10分
	if dims.FetchHealthScore != 10 {
		t.Errorf("fetch health score got %.2f, want 10", dims.FetchHealthScore)
	}
	// 地区覆盖HK+SG优先区 -> 4分
	if dims.RegionScore != 4 {
		t.Errorf("region score got %.2f, want 4", dims.RegionScore)
	}
	// 总分接近94
	if score < 93 || score > 95 {
		t.Errorf("overall score got %.2f, want ~94", score)
	}
}

func TestCalculateScore_PartialAvailability(t *testing.T) {
	nodes := []*subscription.Node{
		{Available: true, Latency: 100, Region: "HK"},
		{Available: false, Latency: 0, Region: "HK"},
	}
	_, dims := CalculateScore(nodes, 200, 0, 10)
	// 可用率50% -> 25分
	if dims.AvailabilityScore != 25 {
		t.Errorf("availability score got %.2f, want 25", dims.AvailabilityScore)
	}
	if dims.AvailableNodes != 1 {
		t.Errorf("available nodes got %d, want 1", dims.AvailableNodes)
	}
}

func TestCalculateScore_LatencyMapping(t *testing.T) {
	tests := []struct {
		latency     int
		expectScore float64 // mean+p95各15分,total 30
	}{
		{100, 30}, // ≤100ms满分
		{550, 15}, // 中位线性映射
		{1000, 0}, // ≥1000ms零分
		{50, 30},  // <100ms满分
		{1500, 0}, // >1000ms零分
	}
	for _, tt := range tests {
		nodes := []*subscription.Node{
			{Available: true, Latency: tt.latency, Region: "HK"},
		}
		_, dims := CalculateScore(nodes, 200, 0, 10)
		// 容忍±1误差(浮点运算)
		if math.Abs(dims.LatencyScore-tt.expectScore) > 1 {
			t.Errorf("latency %d: got score %.2f, want %.2f", tt.latency, dims.LatencyScore, tt.expectScore)
		}
	}
}

func TestCalculateScore_FetchHealth(t *testing.T) {
	nodes := []*subscription.Node{
		{Available: true, Latency: 100, Region: "HK"},
	}
	// HTTP 404 -> fetch health 0分
	_, dims := CalculateScore(nodes, 404, 0, 10)
	if dims.FetchHealthScore != 0 {
		t.Errorf("HTTP 404 fetch health should be 0, got %.2f", dims.FetchHealthScore)
	}
	// HTTP 200, 80%解析成功 -> 8分
	_, dims = CalculateScore(nodes, 200, 2, 10)
	if math.Abs(dims.FetchHealthScore-8) > 0.1 {
		t.Errorf("80%% parse success: got %.2f, want 8", dims.FetchHealthScore)
	}
}

func TestCalculateScore_RegionCoverage(t *testing.T) {
	// HK+SG+US优先区各2分 -> 6分
	nodes := []*subscription.Node{
		{Available: true, Latency: 100, Region: "HK"},
		{Available: true, Latency: 100, Region: "SG"},
		{Available: true, Latency: 100, Region: "US"},
	}
	_, dims := CalculateScore(nodes, 200, 0, 10)
	if dims.RegionScore != 6 {
		t.Errorf("HK+SG+US region score got %.2f, want 6", dims.RegionScore)
	}
	// HK+JP+KR(优先2分+非优先各1分) -> 4分
	nodes = []*subscription.Node{
		{Available: true, Latency: 100, Region: "HK"},
		{Available: true, Latency: 100, Region: "JP"},
		{Available: true, Latency: 100, Region: "KR"},
	}
	_, dims = CalculateScore(nodes, 200, 0, 10)
	if dims.RegionScore != 4 {
		t.Errorf("HK+JP+KR region score got %.2f, want 4", dims.RegionScore)
	}
	// 超过10分上限
	nodes = []*subscription.Node{
		{Available: true, Latency: 100, Region: "HK"},
		{Available: true, Latency: 100, Region: "SG"},
		{Available: true, Latency: 100, Region: "US"},
		{Available: true, Latency: 100, Region: "JP"},
		{Available: true, Latency: 100, Region: "KR"},
		{Available: true, Latency: 100, Region: "TW"},
		{Available: true, Latency: 100, Region: "UK"},
	}
	_, dims = CalculateScore(nodes, 200, 0, 10)
	if dims.RegionScore != 10 {
		t.Errorf("region score should cap at 10, got %.2f", dims.RegionScore)
	}
}

// 权重随 dimensions_json 同源落库(ticket 0037 审查遗留):URL 可达 50/30/10/10;
// 不可达按 5:3:1 重归一且拉取健康为 nil(N/A)。前端报告直接读,旧 run 回退硬编码。
func TestCalculateScore_PersistsDimensionWeights(t *testing.T) {
	nodes := []*subscription.Node{
		{Available: true, Latency: 100, Region: "HK"},
	}

	t.Run("URL reachable: 50/30/10/10", func(t *testing.T) {
		_, dims := CalculateScore(nodes, 200, 0, 10)
		if dims.AvailabilityWeight != 50 || dims.LatencyWeight != 30 || dims.RegionWeight != 10 {
			t.Errorf("weights got %.2f/%.2f/%.2f, want 50/30/10",
				dims.AvailabilityWeight, dims.LatencyWeight, dims.RegionWeight)
		}
		if dims.FetchHealthWeight == nil || *dims.FetchHealthWeight != 10 {
			t.Errorf("fetch health weight got %v, want 10", dims.FetchHealthWeight)
		}
	})

	t.Run("URL unreachable: renormalized 5:3:1, fetch health nil", func(t *testing.T) {
		_, dims := CalculateScore(nodes, 0, 0, 0)
		const ninth = 100.0 / 9.0
		if math.Abs(dims.AvailabilityWeight-5*ninth) > 1e-9 ||
			math.Abs(dims.LatencyWeight-3*ninth) > 1e-9 ||
			math.Abs(dims.RegionWeight-1*ninth) > 1e-9 {
			t.Errorf("weights got %.4f/%.4f/%.4f, want 55.5556/33.3333/11.1111",
				dims.AvailabilityWeight, dims.LatencyWeight, dims.RegionWeight)
		}
		if dims.FetchHealthWeight != nil {
			t.Errorf("fetch health weight got %v, want nil (N/A)", *dims.FetchHealthWeight)
		}
	})

	t.Run("empty nodes still persist weights", func(t *testing.T) {
		_, dims := CalculateScore([]*subscription.Node{}, 200, 0, 0)
		if dims.AvailabilityWeight != 50 || dims.FetchHealthWeight == nil {
			t.Errorf("empty nodes weights got %.2f/%v, want 50/10",
				dims.AvailabilityWeight, dims.FetchHealthWeight)
		}
	})
}
