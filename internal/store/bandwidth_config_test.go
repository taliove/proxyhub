package store

import (
	"testing"

	"github.com/taliove/proxyhub/internal/detection"
)

func TestGetBandwidthConfig_Defaults(t *testing.T) {
	st := newTestStore(t)

	cfg := st.GetBandwidthConfig()
	def := detection.DefaultBandwidthConfig()

	// 未设置任何项时应返回默认配置(与 DefaultBandwidthConfig 一致)
	if cfg.UpBytes != def.UpBytes {
		t.Errorf("UpBytes = %d, want %d", cfg.UpBytes, def.UpBytes)
	}
	if cfg.TestDurationSec != def.TestDurationSec {
		t.Errorf("TestDurationSec = %d, want %d", cfg.TestDurationSec, def.TestDurationSec)
	}
	if cfg.TimeoutSec != def.TimeoutSec {
		t.Errorf("TimeoutSec = %d, want %d", cfg.TimeoutSec, def.TimeoutSec)
	}
	if cfg.DirTimeoutSec != def.DirTimeoutSec {
		t.Errorf("DirTimeoutSec = %d, want %d", cfg.DirTimeoutSec, def.DirTimeoutSec)
	}
	if cfg.MinDownMbps != 1.0 || cfg.MinUpMbps != 1.0 {
		t.Errorf("thresholds = %.1f/%.1f, want 1.0/1.0", cfg.MinDownMbps, cfg.MinUpMbps)
	}
	if cfg.DownURL == "" || cfg.UpURL == "" {
		t.Error("URLs should have default values")
	}
}

func TestGetBandwidthConfig_Overrides(t *testing.T) {
	st := newTestStore(t)

	// 写入自定义配置
	settings := map[string]string{
		settingBandwidthDownURL:     "https://example.com/down",
		settingBandwidthUpURL:       "https://example.com/up",
		settingBandwidthUpBytes:     "10485760", // 10MB
		settingBandwidthTimeoutSec:  "90",
		settingBandwidthMinDownMbps: "5.5",
		settingBandwidthMinUpMbps:   "2.5",
	}
	for k, v := range settings {
		if err := st.SetSetting(k, v); err != nil {
			t.Fatalf("SetSetting(%s) error = %v", k, err)
		}
	}

	cfg := st.GetBandwidthConfig()

	if cfg.DownURL != "https://example.com/down" {
		t.Errorf("DownURL = %q, want https://example.com/down", cfg.DownURL)
	}
	if cfg.UpURL != "https://example.com/up" {
		t.Errorf("UpURL = %q", cfg.UpURL)
	}
	if cfg.UpBytes != 10485760 {
		t.Errorf("UpBytes = %d, want 10485760", cfg.UpBytes)
	}
	if cfg.TimeoutSec != 90 {
		t.Errorf("TimeoutSec = %d, want 90", cfg.TimeoutSec)
	}
	if cfg.MinDownMbps != 5.5 {
		t.Errorf("MinDownMbps = %.1f, want 5.5", cfg.MinDownMbps)
	}
	if cfg.MinUpMbps != 2.5 {
		t.Errorf("MinUpMbps = %.1f, want 2.5", cfg.MinUpMbps)
	}
}

func TestGetBandwidthConfig_InvalidValuesFallBack(t *testing.T) {
	st := newTestStore(t)

	// 非法值（非数字/负数）应被忽略，回退默认
	st.SetSetting(settingBandwidthUpBytes, "not-a-number")
	st.SetSetting(settingBandwidthTimeoutSec, "-5")

	cfg := st.GetBandwidthConfig()
	def := detection.DefaultBandwidthConfig()

	if cfg.UpBytes != def.UpBytes {
		t.Errorf("invalid UpBytes should fall back to default %d, got %d", def.UpBytes, cfg.UpBytes)
	}
	if cfg.TimeoutSec != def.TimeoutSec {
		t.Errorf("negative TimeoutSec should fall back to default %d, got %d", def.TimeoutSec, cfg.TimeoutSec)
	}
}
