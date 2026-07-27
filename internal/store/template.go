package store

import (
	"errors"
	"fmt"

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

// GetClashTemplateForUser 按回退链读取 Clash 模板(租户级设置,多租户):
// 用户默认模板(template 表 is_default=1) ?? 全局默认(system_settings.clash_template) ?? 内嵌默认模板。
// userID<=0 = 全局视角(等价 GetClashTemplate)。
//
// Ticket endpoint-template-01 后语义变更:读用户默认模板而非 name='clash' 单行,
// 迁移保证现有 name='clash' 行已标为默认,行为等价。
func (s *Store) GetClashTemplateForUser(userID int64) (string, error) {
	if userID > 0 {
		def, err := s.GetDefaultTemplate(userID)
		if err != nil {
			return "", err
		}
		if def != nil && def.Content != "" {
			return def.Content, nil
		}
	}
	return s.GetClashTemplate()
}

// SetClashTemplateForUser 保存指定用户的模板覆盖。
// Ticket endpoint-template-01 后:upsert 名为 'clash' 的模板(向后兼容旧行为)。
// 若库为空自动标为默认;若已有其他默认模板则不改变默认标记。
func (s *Store) SetClashTemplateForUser(userID int64, tmpl string) error {
	if tmpl == "" {
		return errors.New("template is empty")
	}

	// Check if template 'clash' exists
	existing, err := s.GetTemplateByName(userID, "clash")
	if err != nil && err.Error() != "not found" {
		return err
	}

	if existing != nil {
		// Update existing 'clash' template
		return s.UpdateTemplate(userID, "clash", tmpl)
	}

	// Create new 'clash' template
	// Check if library is empty (auto-default)
	var count int
	err = s.db.QueryRow(`SELECT COUNT(*) FROM template WHERE user_id = ?`, userID).Scan(&count)
	if err != nil {
		return fmt.Errorf("count templates: %w", err)
	}
	isFirst := count == 0

	_, err = s.db.Exec(`
		INSERT INTO template (user_id, name, content, is_default, updated_at)
		VALUES (?, 'clash', ?, ?, CURRENT_TIMESTAMP)
	`, userID, tmpl, boolToInt(isFirst))
	return err
}

// DeleteClashTemplateForUser 删除指定用户的模板覆盖("重置":回到跟随全局默认)。
// Ticket endpoint-template-01 后:删除名为 'clash' 的模板(向后兼容旧行为)。
// 无覆盖行是 no-op(幂等)。
func (s *Store) DeleteClashTemplateForUser(userID int64) error {
	_, err := s.db.Exec(`DELETE FROM template WHERE user_id = ? AND name = 'clash'`, userID)
	return err
}
