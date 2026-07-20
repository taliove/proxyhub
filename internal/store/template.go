package store

import (
	"errors"

	"github.com/taliove/proxyhub/internal/generator"
)

// clashTemplateKey 是 settings 表里存储 Clash 配置模板的键。
const clashTemplateKey = "clash_template"

// GetClashTemplate 返回当前生效的 Clash 配置模板。
// 若用户从未保存过（DB 中不存在），回退到内嵌的默认模板。
func (s *Store) GetClashTemplate() (string, error) {
	tmpl, err := s.GetSetting(clashTemplateKey)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return generator.DefaultTemplate(), nil
		}
		return "", err
	}
	if tmpl == "" {
		return generator.DefaultTemplate(), nil
	}
	return tmpl, nil
}

// SetClashTemplate 保存用户编辑的 Clash 配置模板。
func (s *Store) SetClashTemplate(tmpl string) error {
	if tmpl == "" {
		return errors.New("template is empty")
	}
	return s.SetSetting(clashTemplateKey, tmpl)
}

// ResetClashTemplate 把模板恢复为内嵌的默认模板。
func (s *Store) ResetClashTemplate() error {
	return s.SetSetting(clashTemplateKey, generator.DefaultTemplate())
}
