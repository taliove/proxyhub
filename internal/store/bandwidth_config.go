package store

import (
	"strconv"

	"github.com/taliove/proxyhub/internal/detection"
)

// 带宽测试配置 settings 键
const (
	settingBandwidthDownURL        = "bandwidth_down_url"
	settingBandwidthUpURL          = "bandwidth_up_url"
	settingBandwidthUpBytes        = "bandwidth_up_bytes"
	settingBandwidthTestDuration   = "bandwidth_test_duration_sec"
	settingBandwidthTimeoutSec     = "bandwidth_timeout_sec"
	settingBandwidthDirTimeoutSec  = "bandwidth_dir_timeout_sec"
	settingBandwidthMinDownMbps    = "bandwidth_min_down_mbps"
	settingBandwidthMinUpMbps      = "bandwidth_min_up_mbps"
)

// GetBandwidthConfig 读取带宽测试配置，未设置的项用默认值兜底（fail-open）。
// 下行大小由 DownURL 的 bytes= 参数控制，故无独立 down_bytes 配置项。
func (s *Store) GetBandwidthConfig() detection.BandwidthConfig {
	cfg := detection.DefaultBandwidthConfig()

	settings, err := s.GetSystemSettings()
	if err != nil {
		return cfg // 读取失败用默认
	}

	if v := settings[settingBandwidthDownURL]; v != "" {
		cfg.DownURL = v
	}
	if v := settings[settingBandwidthUpURL]; v != "" {
		cfg.UpURL = v
	}
	applyPositiveInt(settings[settingBandwidthUpBytes], &cfg.UpBytes)
	applyPositiveInt(settings[settingBandwidthTestDuration], &cfg.TestDurationSec)
	applyPositiveInt(settings[settingBandwidthTimeoutSec], &cfg.TimeoutSec)
	applyPositiveInt(settings[settingBandwidthDirTimeoutSec], &cfg.DirTimeoutSec)
	applyNonNegFloat(settings[settingBandwidthMinDownMbps], &cfg.MinDownMbps)
	applyNonNegFloat(settings[settingBandwidthMinUpMbps], &cfg.MinUpMbps)

	return cfg
}

// applyPositiveInt 若 raw 是合法正整数则写入 dst（非法/非正忽略，保留默认）
func applyPositiveInt(raw string, dst *int) {
	if raw == "" {
		return
	}
	if n, err := strconv.Atoi(raw); err == nil && n > 0 {
		*dst = n
	}
}

// applyNonNegFloat 若 raw 是合法非负浮点则写入 dst（非法/负数忽略，保留默认）
func applyNonNegFloat(raw string, dst *float64) {
	if raw == "" {
		return
	}
	if f, err := strconv.ParseFloat(raw, 64); err == nil && f >= 0 {
		*dst = f
	}
}
