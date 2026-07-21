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
		{100, 30},   // ≤100ms满分
		{550, 15},   // 中位线性映射
		{1000, 0},   // ≥1000ms零分
		{50, 30},    // <100ms满分
		{1500, 0},   // >1000ms零分
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
