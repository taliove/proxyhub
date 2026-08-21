package store

import (
	"database/sql"
	"fmt"
	"strings"
)

// migrateTemplateNoResolveOrder 修正 v0.11.0 时代存量模板里的 no-resolve 错序规则
// (issue #124)。ce64e7d(v0.11.1)只修了内嵌默认模板;template(模板库)、
// template_versions(模板版本)、system_settings.clash_template(超管全局默认)
// 三处存储的副本从未迁移——no-resolve 是 IP 类规则的末尾可选参数,写在第三位
// 会被 mihomo 当成策略组名,Android 客户端报 `error proxy [no-resolve] not found`
// 启动即失败(2026-08-17 测试线实证)。幂等:只改写错序行,其余内容逐字节保留,
// 重复启动不产生二次改写。
func (s *Store) migrateTemplateNoResolveOrder() error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin no-resolve migration: %w", err)
	}
	defer tx.Rollback()

	for _, table := range []string{"template", "template_versions"} {
		if err := rewriteNoResolveColumn(tx, table); err != nil {
			return err
		}
	}
	if err := rewriteNoResolveSetting(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// rewriteNoResolveColumn 逐行扫描 <table>.content,只回写发生变化的行。
// table 来自调用方的固定白名单,非外部输入。
func rewriteNoResolveColumn(tx *sql.Tx, table string) error {
	rows, err := tx.Query(`SELECT id, content FROM ` + table)
	if err != nil {
		return fmt.Errorf("scan %s for no-resolve migration: %w", table, err)
	}
	type fix struct {
		id      int64
		content string
	}
	var fixes []fix
	for rows.Next() {
		var id int64
		var content string
		if err := rows.Scan(&id, &content); err != nil {
			rows.Close()
			return fmt.Errorf("scan %s row: %w", table, err)
		}
		if normalized := normalizeNoResolveOrder(content); normalized != content {
			fixes = append(fixes, fix{id: id, content: normalized})
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate %s: %w", table, err)
	}
	rows.Close()

	for _, f := range fixes {
		if _, err := tx.Exec(`UPDATE `+table+` SET content = ? WHERE id = ?`, f.content, f.id); err != nil {
			return fmt.Errorf("rewrite %s row %d: %w", table, f.id, err)
		}
	}
	return nil
}

// rewriteNoResolveSetting 修正 system_settings.clash_template(超管全局默认模板)。
// 键不存在(从未保存过全局默认)是正常情形,直接跳过。
func rewriteNoResolveSetting(tx *sql.Tx) error {
	var content string
	err := tx.QueryRow(`SELECT value FROM system_settings WHERE key = ?`, clashTemplateKey).Scan(&content)
	if err != nil {
		if isNoRows(err) {
			return nil
		}
		return fmt.Errorf("read %s for no-resolve migration: %w", clashTemplateKey, err)
	}
	normalized := normalizeNoResolveOrder(content)
	if normalized == content {
		return nil
	}
	if _, err := tx.Exec(`UPDATE system_settings SET value = ? WHERE key = ?`, normalized, clashTemplateKey); err != nil {
		return fmt.Errorf("rewrite %s: %w", clashTemplateKey, err)
	}
	return nil
}

// normalizeNoResolveOrder 把内容中错序的 no-resolve 规则归位到行尾(纯函数)。
// 判定口径与 internal/generator/template_test.go(issue #114 回归)一致:
// 逗号分段后跳过首字段(规则类型),任何 trim 后等于 "no-resolve" 的字段
// 不在末位即错序。无错序时原样返回,保证幂等。
func normalizeNoResolveOrder(content string) string {
	lines := strings.Split(content, "\n")
	changed := false
	for i, line := range lines {
		if fixed := normalizeNoResolveLine(line); fixed != line {
			lines[i] = fixed
			changed = true
		}
	}
	if !changed {
		return content
	}
	return strings.Join(lines, "\n")
}

// normalizeNoResolveLine 归位单行的错序 no-resolve;行无错序原样返回。
// 首个逗号字段含缩进与 "- " 前缀并原样保留,故按逗号重建不丢 YAML 格式。
func normalizeNoResolveLine(line string) string {
	fields := strings.Split(line, ",")
	misplaced := false
	for i, f := range fields[1:] { // 跳过规则类型字段
		if strings.TrimSpace(f) == "no-resolve" && i+1 != len(fields)-1 {
			misplaced = true
			break
		}
	}
	if !misplaced {
		return line
	}
	out := make([]string, 0, len(fields))
	out = append(out, fields[0])
	for _, f := range fields[1:] {
		if strings.TrimSpace(f) == "no-resolve" {
			continue
		}
		out = append(out, f)
	}
	return strings.Join(append(out, "no-resolve"), ",")
}
