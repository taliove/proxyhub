package store

import (
	"errors"
	"fmt"

	"github.com/taliove/proxyhub/internal/sitepath"
)

// SitePathKey 是 Site Path 在 settings 表中的键。
// 值为空(或未写入)表示未配置,服务端按开发模式原样服务,不做路径边界限制。
const SitePathKey = "site_path"

// GetSitePath 读取已配置的 Site Path(规范形式);未配置返回 "" 且 err 为 nil。
func (s *Store) GetSitePath() (string, error) {
	v, err := s.GetSetting(SitePathKey)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return "", nil
		}
		return "", fmt.Errorf("get site path: %w", err)
	}
	return sitepath.Normalize(v), nil
}

// SetSitePath 校验并持久化 Site Path(规范化后存储)。传空串清除配置(回到开发模式)。
func (s *Store) SetSitePath(path string) error {
	p := sitepath.Normalize(path)
	if p == "" {
		return s.SetSetting(SitePathKey, "")
	}
	if err := sitepath.Validate(p); err != nil {
		return err
	}
	return s.SetSetting(SitePathKey, p)
}
