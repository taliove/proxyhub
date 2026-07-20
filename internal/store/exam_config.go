package store

import "github.com/taliove/proxyhub/internal/detection"

// 深度体检配置 settings 键。
const (
	settingExamStabilityDuration = "exam_stability_duration_sec"
)

// GetExamConfig 读取深度体检配置,未设置或非法的项用默认值兜底(fail-open)。
// 当前仅稳定性采样时长可配(默认 30s);非法值(非正整数/空)回退默认。
func (s *Store) GetExamConfig() detection.ExamConfig {
	cfg := detection.DefaultExamConfig()

	settings, err := s.GetSystemSettings()
	if err != nil {
		return cfg // 读取失败用默认
	}

	applyPositiveInt(settings[settingExamStabilityDuration], &cfg.StabilityDurationSec)
	return cfg
}
