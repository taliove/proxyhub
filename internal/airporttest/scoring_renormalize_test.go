package airporttest

import (
	"math"
	"testing"

	"github.com/taliove/proxyhub/internal/subscription"
)

// TestCalculateScore_RenormalizedWeights tests scoring when fetch health is N/A.
// When httpStatus is non-2xx, fetch health dimension is excluded and weights
// are renormalized: availability 5/9 (55.56%), latency 3/9 (33.33%), region 1/9 (11.11%).
func TestCalculateScore_RenormalizedWeights_URLUnreachable(t *testing.T) {
	// Setup: 100% availability, perfect latency, 2 priority regions
	nodes := []*subscription.Node{
		{Available: true, Latency: 50, Region: "HK"},
		{Available: true, Latency: 60, Region: "SG"},
	}

	// HTTP 404: fetch health N/A, weights renormalized
	score, dims := CalculateScore(nodes, 404, 0, 0)

	// Fetch health should be 0 (N/A)
	if dims.FetchHealthScore != 0 {
		t.Errorf("fetch health score should be 0 for HTTP 404, got %.2f", dims.FetchHealthScore)
	}

	// Availability: 100% * (5/9 * 100) = 55.56
	expectedAvail := 55.56
	if math.Abs(dims.AvailabilityScore-expectedAvail) > 0.1 {
		t.Errorf("availability score got %.2f, want %.2f (renormalized 5/9)", dims.AvailabilityScore, expectedAvail)
	}

	// Latency: ~30 * (3/9 / 0.3) = ~33.33 for perfect latency
	// Original latency was 30%, renormalized to 33.33%
	if dims.LatencyScore < 32 || dims.LatencyScore > 34 {
		t.Errorf("latency score got %.2f, want ~33.33 (renormalized 3/9)", dims.LatencyScore)
	}

	// Region: 4 (HK+SG) * (1/9 / 0.1) = ~4.44
	expectedRegion := 4.44
	if math.Abs(dims.RegionScore-expectedRegion) > 0.1 {
		t.Errorf("region score got %.2f, want %.2f (renormalized 1/9)", dims.RegionScore, expectedRegion)
	}

	// Overall: should be close to 55.56 + 33.33 + 4.44 = 93.33
	expectedOverall := 93.33
	if math.Abs(score-expectedOverall) > 1 {
		t.Errorf("overall score got %.2f, want %.2f (renormalized sum)", score, expectedOverall)
	}

	// Total should still be 0-100 range
	if score < 0 || score > 100 {
		t.Errorf("score %.2f out of valid 0-100 range", score)
	}
}

// TestCalculateScore_RenormalizedWeights_PartialAvailability tests renormalized scoring
// with partial availability to ensure weights scale correctly.
func TestCalculateScore_RenormalizedWeights_PartialAvailability(t *testing.T) {
	nodes := []*subscription.Node{
		{Available: true, Latency: 100, Region: "HK"},
		{Available: false, Latency: 0, Region: "HK"},
		{Available: true, Latency: 150, Region: "SG"},
		{Available: false, Latency: 0, Region: "SG"},
	}

	// HTTP 500: fetch health N/A
	score, dims := CalculateScore(nodes, 500, 0, 0)

	// 50% availability * (5/9 * 100) = 27.78
	expectedAvail := 27.78
	if math.Abs(dims.AvailabilityScore-expectedAvail) > 0.1 {
		t.Errorf("availability score got %.2f, want %.2f", dims.AvailabilityScore, expectedAvail)
	}

	// Latency score should be scaled to 3/9 weight (33.33% instead of 30%)
	// With moderate latency (100-150ms), expect ~20-30 range after renormalization
	if dims.LatencyScore < 20 || dims.LatencyScore > 33 {
		t.Errorf("latency score got %.2f, expected 20-33 range", dims.LatencyScore)
	}

	// Region: 2 priority regions (HK+SG) = 4 points, renormalized to 1/9 weight
	expectedRegion := 4.44
	if math.Abs(dims.RegionScore-expectedRegion) > 0.1 {
		t.Errorf("region score got %.2f, want %.2f", dims.RegionScore, expectedRegion)
	}

	// Overall should be sum of renormalized components
	expectedOverall := dims.AvailabilityScore + dims.LatencyScore + dims.RegionScore
	if math.Abs(score-expectedOverall) > 0.1 {
		t.Errorf("overall score %.2f != sum of components %.2f", score, expectedOverall)
	}

	// Verify still in valid range
	if score < 0 || score > 100 {
		t.Errorf("score %.2f out of valid range", score)
	}
}

// TestCalculateScore_RenormalizedWeights_ZeroAvailability tests edge case:
// 0% availability with renormalized weights should give 0 score.
func TestCalculateScore_RenormalizedWeights_ZeroAvailability(t *testing.T) {
	nodes := []*subscription.Node{
		{Available: false, Latency: 0, Region: "HK"},
		{Available: false, Latency: 0, Region: "SG"},
		{Available: false, Latency: 0, Region: "US"},
	}

	score, dims := CalculateScore(nodes, 404, 0, 0)

	// 0% availability
	if dims.AvailabilityScore != 0 {
		t.Errorf("availability score should be 0, got %.2f", dims.AvailabilityScore)
	}

	// No latency (no available nodes)
	if dims.LatencyScore != 0 {
		t.Errorf("latency score should be 0, got %.2f", dims.LatencyScore)
	}

	// Region coverage should still score (nodes exist, just unavailable)
	// HK+SG+US = 6 points raw, renormalized to 1/9 weight = 6.67
	if dims.RegionScore < 6 || dims.RegionScore > 7 {
		t.Errorf("region score got %.2f, want ~6.67", dims.RegionScore)
	}

	// Overall should be just region score (others are 0)
	if math.Abs(score-dims.RegionScore) > 0.1 {
		t.Errorf("overall score %.2f should equal region score %.2f", score, dims.RegionScore)
	}
}

// TestCalculateScore_NormalWeights_URLReachable verifies that when URL is reachable
// (HTTP 2xx), original 4-dimension weights are used (not renormalized).
func TestCalculateScore_NormalWeights_URLReachable(t *testing.T) {
	nodes := []*subscription.Node{
		{Available: true, Latency: 50, Region: "HK"},
		{Available: true, Latency: 60, Region: "SG"},
	}

	// HTTP 200: all 4 dimensions active
	score, dims := CalculateScore(nodes, 200, 0, 10)

	// Availability: 100% * 50% = 50
	if dims.AvailabilityScore != 50 {
		t.Errorf("availability score got %.2f, want 50", dims.AvailabilityScore)
	}

	// Latency: ~30 for perfect latency (original weight)
	if dims.LatencyScore < 29 || dims.LatencyScore > 31 {
		t.Errorf("latency score got %.2f, want ~30", dims.LatencyScore)
	}

	// Fetch health: 100% * 10% = 10
	if dims.FetchHealthScore != 10 {
		t.Errorf("fetch health score got %.2f, want 10", dims.FetchHealthScore)
	}

	// Region: HK+SG = 4 (original weight, no renormalization)
	if dims.RegionScore != 4 {
		t.Errorf("region score got %.2f, want 4", dims.RegionScore)
	}

	// Overall: 50 + 30 + 10 + 4 = 94
	expectedOverall := 94.0
	if math.Abs(score-expectedOverall) > 1 {
		t.Errorf("overall score got %.2f, want %.2f", score, expectedOverall)
	}
}

// TestCalculateScore_RenormalizedWeights_EmptyPool verifies that empty pool
// with URL unreachable still returns 0 (not division by zero).
func TestCalculateScore_RenormalizedWeights_EmptyPool(t *testing.T) {
	score, dims := CalculateScore([]*subscription.Node{}, 404, 0, 0)

	if score != 0 {
		t.Errorf("empty pool score should be 0, got %.2f", score)
	}

	if dims.AvailabilityScore != 0 || dims.LatencyScore != 0 || dims.RegionScore != 0 || dims.FetchHealthScore != 0 {
		t.Error("all dimension scores should be 0 for empty pool")
	}
}
