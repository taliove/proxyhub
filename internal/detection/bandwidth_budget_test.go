package detection

import (
	"testing"
	"time"
)

// TestBandwidthStreamBudget 流式测速墙钟预算:两方向各一次单方向硬超时 + 收尾余量,
// 供 SSE 端点自设写 deadline(issue #158)。推导与 streamBandwidthTest 同一来源
// (streamDirTimeouts):DirTimeout 小于测速时长时硬上限抬到 testDur+10s。
func TestBandwidthStreamBudget(t *testing.T) {
	// 默认配置:TestDurationSec=10、DirTimeoutSec=20 → 2*20s + 15s = 55s
	d := NewDetector(4, time.Second, time.Second)
	if got, want := d.BandwidthStreamBudget(), 55*time.Second; got != want {
		t.Errorf("default budget = %v, want %v", got, want)
	}

	// DirTimeout 小于测速时长:硬上限抬到 testDur+10s → 2*(30+10)s + 15s = 95s
	d.SetBandwidthConfigProvider(func() BandwidthConfig {
		return BandwidthConfig{TestDurationSec: 30, DirTimeoutSec: 5}
	})
	if got, want := d.BandwidthStreamBudget(), 95*time.Second; got != want {
		t.Errorf("custom budget = %v, want %v", got, want)
	}
}
