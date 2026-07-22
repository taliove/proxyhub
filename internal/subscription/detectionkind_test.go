package subscription

import (
	"testing"
	"time"
)

// TestNode_AvailabilitySource 可用性判定来源推导口径(全池统一,见 ticket 0016):
// DetectionKind 为空 -> never(从未检测);health -> 仅健康检查(TCP 快检);real -> 真实代理检测。
func TestNode_AvailabilitySource(t *testing.T) {
	cases := []struct {
		name string
		kind string
		want string
	}{
		{"从未检测", "", AvailabilitySourceNever},
		{"仅健康检查", DetectionKindHealth, AvailabilitySourceHealth},
		{"真实检测", DetectionKindReal, AvailabilitySourceReal},
		{"未知值兜底为 never", "bogus", AvailabilitySourceNever},
	}
	for _, c := range cases {
		n := &Node{DetectionKind: c.kind}
		if got := n.AvailabilitySource(); got != c.want {
			t.Errorf("%s: AvailabilitySource() = %q, want %q", c.name, got, c.want)
		}
	}
}

// TestMergePool_CarryForwardDetectionKind 刷新 carry-forward 必须保留判定来源,
// 否则每轮刷新后"真实检测"标记被抹掉,来源口径失真(与 DetectionLastCheck 同生命周期)。
func TestMergePool_CarryForwardDetectionKind(t *testing.T) {
	oldPool := []*Node{
		{
			Name: "香港01", Type: "ss", Server: "hk01.example.com", Port: 8388,
			Region: "HK", Source: "机场A",
			Available: true, Latency: 120,
			DetectionLastCheck: time.Now().Add(-1 * time.Hour),
			DetectionKind:      DetectionKindReal,
		},
	}
	newPool := []*Node{
		{Name: "香港01", Type: "ss", Server: "hk01.example.com", Port: 8388, Region: "HK", Source: "机场A"},
	}

	result := MergePool(oldPool, newPool)
	if len(result) != 1 {
		t.Fatalf("len = %d, want 1", len(result))
	}
	if result[0].DetectionKind != DetectionKindReal {
		t.Errorf("DetectionKind = %q, want %q(carry-forward 不得丢失)", result[0].DetectionKind, DetectionKindReal)
	}
}
