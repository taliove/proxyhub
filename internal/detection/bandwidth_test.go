package detection

import (
	"context"
	"testing"
	"time"

	"github.com/taliove/proxyhub/internal/subscription"
)

// TestBandwidth_ConfigProvider 验证带宽配置提供函数被采用（注入 vs 默认）
func TestBandwidth_ConfigProvider(t *testing.T) {
	d := NewDetector(4, time.Second, time.Second)

	// 未注入时用默认(与 DefaultBandwidthConfig 一致)
	want := DefaultBandwidthConfig().TimeoutSec
	if got := d.resolveBandwidthConfig(); got.TimeoutSec != want {
		t.Errorf("default TimeoutSec = %d, want %d", got.TimeoutSec, want)
	}

	// 注入自定义配置
	custom := BandwidthConfig{
		DownURL:     "https://custom/down",
		UpURL:       "https://custom/up",
		UpBytes:     512,
		TimeoutSec:  30,
		MinDownMbps: 10,
		MinUpMbps:   5,
	}
	d.SetBandwidthConfigProvider(func() BandwidthConfig { return custom })

	got := d.resolveBandwidthConfig()
	if got.TimeoutSec != 30 || got.MinDownMbps != 10 || got.DownURL != "https://custom/down" {
		t.Errorf("injected config not used: %+v", got)
	}
}

// TestBandwidth_TCPUnreachable 验证 TCP 不通时带宽测试直接返回不可用
func TestBandwidth_TCPUnreachable(t *testing.T) {
	d := NewDetector(4, 300*time.Millisecond, time.Second)
	node := &subscription.Node{Server: "127.0.0.1", Port: 1, Type: "vless"}

	res := d.TestNode(context.Background(), node, "bandwidth")

	if res.Available {
		t.Errorf("bandwidth test on closed port should be unavailable, got %+v", res)
	}
	if res.Mode != "bandwidth" {
		t.Errorf("Mode = %q, want bandwidth", res.Mode)
	}
	if res.Error == "" {
		t.Error("expected error message on unreachable")
	}
}

// TestTestNode_BandwidthRoutesToStream 即时测试 bandwidth 档走流式实现(issue #159):
// 死节点在 TCP 快筛处 fail-fast,错误文本是流式实现的固定串
// (legacy io.Copy 全量下载路径带 dial 详情后缀,已删除)。
func TestTestNode_BandwidthRoutesToStream(t *testing.T) {
	d := NewDetector(4, 300*time.Millisecond, time.Second)
	node := &subscription.Node{Server: "127.0.0.1", Port: 1, Type: "vless"}

	res := d.TestNode(context.Background(), node, "bandwidth")

	if res.Available {
		t.Errorf("bandwidth test on closed port should be unavailable, got %+v", res)
	}
	if res.Mode != "bandwidth" {
		t.Errorf("Mode = %q, want bandwidth", res.Mode)
	}
	if res.Error != "TCP connection failed" {
		t.Errorf("Error = %q, want stream-flavor %q", res.Error, "TCP connection failed")
	}
}

// TestDefaultBandwidthConfig 验证默认配置合理
func TestDefaultBandwidthConfig(t *testing.T) {
	cfg := DefaultBandwidthConfig()
	if cfg.DownURL == "" || cfg.UpURL == "" {
		t.Error("default URLs should not be empty")
	}
	if cfg.UpBytes <= 0 {
		t.Error("default up byte size should be positive")
	}
	if cfg.TimeoutSec <= 0 {
		t.Error("default timeout should be positive")
	}
}
