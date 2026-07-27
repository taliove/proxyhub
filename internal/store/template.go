package store

import (
	"database/sql"
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
// 用户覆盖(template 表 name='clash' 行) ?? 全局默认(system_settings.clash_template) ?? 内嵌默认模板。
// userID<=0 = 全局视角(等价 GetClashTemplate)。
func (s *Store) GetClashTemplateForUser(userID int64) (string, error) {
	if userID > 0 {
		var content string
		err := s.db.QueryRow(
			`SELECT content FROM template WHERE user_id = ? AND name = 'clash'`, userID,
		).Scan(&content)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return "", err
		}
		if content != "" {
			return content, nil
		}
	}
	return s.GetClashTemplate()
}

// SetClashTemplateForUser 保存指定用户的模板覆盖(template 表 name='clash' 行,
// DELETE+INSERT 单事务:表无 UNIQUE(user_id,name) 约束,不能用 ON CONFLICT)。
func (s *Store) SetClashTemplateForUser(userID int64, tmpl string) error {
	if tmpl == "" {
		return errors.New("template is empty")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin set clash template: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM template WHERE user_id = ? AND name = 'clash'`, userID); err != nil {
		return fmt.Errorf("clear clash template: %w", err)
	}
	if _, err := tx.Exec(`
		INSERT INTO template (user_id, name, content, updated_at)
		VALUES (?, 'clash', ?, CURRENT_TIMESTAMP)
	`, userID, tmpl); err != nil {
		return fmt.Errorf("insert clash template: %w", err)
	}
	return tx.Commit()
}

// DeleteClashTemplateForUser 删除指定用户的模板覆盖("重置":回到跟随全局默认)。
// 无覆盖行是 no-op(幂等)。
func (s *Store) DeleteClashTemplateForUser(userID int64) error {
	_, err := s.db.Exec(`DELETE FROM template WHERE user_id = ? AND name = 'clash'`, userID)
	return err
}
