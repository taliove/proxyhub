package store

import (
	"log/slog"

	"github.com/taliove/proxyhub/internal/detection"
)

// 直连出口配置 settings 键
const (
	settingDirectEgressEnabled   = "direct_egress_enabled"
	settingDirectEgressDoHURL    = "direct_egress_doh_url"
	settingDirectEgressInterface = "direct_egress_interface"
)

// GetDirectEgressConfig 读取直连出口配置,未设置的项用默认值兜底(fail-open:默认开)。
// 开关只有显式写 "false"/"0" 才关;其他非空值(含无法解析的)都视为开,
// 与"TUN 共存下默认保检测可信"的定位一致。
func (s *Store) GetDirectEgressConfig() detection.DirectEgressConfig {
	cfg := detection.DefaultDirectEgressConfig()

	settings, err := s.GetSystemSettings()
	if err != nil {
		slog.Warn("direct egress: read settings failed, fall back to defaults", "error", err)
		return cfg // 读取失败用默认
	}

	if v := settings[settingDirectEgressEnabled]; v != "" {
		cfg.Enabled = v != "false" && v != "0"
	}
	if v := settings[settingDirectEgressDoHURL]; v != "" {
		cfg.DoHURL = v
	}
	// 网卡名允许显式置空(= 自动识别),故不做非空判断
	cfg.Interface = settings[settingDirectEgressInterface]

	return cfg
}
