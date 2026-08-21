package store

import (
	"testing"
)

// v0.11.0 时代模板 fixture:错序行(no-resolve 在第三位)、正确行、
// 无 no-resolve 行混杂。fixture 纪律:只用 example.com 合成值。
const noResolveLegacyContent = `rules:
  - IP-CIDR,127.0.0.0/8,no-resolve,🎯 绕过代理
  - IP-CIDR,10.0.0.0/8,🎯 绕过代理,no-resolve
  - DOMAIN-SUFFIX,example.com,DIRECT
  - IP-CIDR6,::1/128,no-resolve,DIRECT
  - MATCH,🐟 漏网之鱼
`

const noResolveFixedContent = `rules:
  - IP-CIDR,127.0.0.0/8,🎯 绕过代理,no-resolve
  - IP-CIDR,10.0.0.0/8,🎯 绕过代理,no-resolve
  - DOMAIN-SUFFIX,example.com,DIRECT
  - IP-CIDR6,::1/128,DIRECT,no-resolve
  - MATCH,🐟 漏网之鱼
`

func TestNormalizeNoResolveOrder(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "错序行归位到行尾",
			in:   "    - IP-CIDR,127.0.0.0/8,no-resolve,🎯 绕过代理",
			want: "    - IP-CIDR,127.0.0.0/8,🎯 绕过代理,no-resolve",
		},
		{
			name: "正确行一字不动",
			in:   "    - IP-CIDR,10.0.0.0/8,🎯 绕过代理,no-resolve",
			want: "    - IP-CIDR,10.0.0.0/8,🎯 绕过代理,no-resolve",
		},
		{
			name: "无 no-resolve 的规则行不动",
			in:   "    - DOMAIN-SUFFIX,example.com,DIRECT",
			want: "    - DOMAIN-SUFFIX,example.com,DIRECT",
		},
		{
			name: "IP-CIDR6 错序同样归位",
			in:   "- IP-CIDR6,::1/128,no-resolve,DIRECT",
			want: "- IP-CIDR6,::1/128,DIRECT,no-resolve",
		},
		{
			name: "含逗号但无裸 no-resolve 字段的非规则行不动",
			in:   "  nameserver: 223.5.5.5,223.6.6.6",
			want: "  nameserver: 223.5.5.5,223.6.6.6",
		},
		{
			name: "单行无逗号不动",
			in:   "rules:",
			want: "rules:",
		},
		{
			name: "空内容不动",
			in:   "",
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeNoResolveOrder(tc.in); got != tc.want {
				t.Errorf("normalizeNoResolveOrder(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}

	// 幂等:归位后的内容再次归位必须逐字节不变
	if got := normalizeNoResolveOrder(normalizeNoResolveOrder(noResolveLegacyContent)); got != noResolveFixedContent {
		t.Errorf("double normalize not idempotent:\n%s", got)
	}
}

// 迁移钉死验收条件(issue #124):三处存储的错序行被修正、正确行一字不动、
// 重复执行不重复改写。
func TestMigrateTemplateNoResolveOrder(t *testing.T) {
	s := newTestStore(t)

	// template:一行错序待修,一行全正确对照
	res, err := s.db.Exec(`INSERT INTO template (user_id, name, content, is_default) VALUES (1, 'clash', ?, 1)`, noResolveLegacyContent)
	if err != nil {
		t.Fatalf("seed template: %v", err)
	}
	templateID, _ := res.LastInsertId()
	pristine := "rules:\n  - IP-CIDR,192.168.0.0/16,🎯 绕过代理,no-resolve\n  - MATCH,🐟 漏网之鱼\n"
	if _, err := s.db.Exec(`INSERT INTO template (user_id, name, content, is_default) VALUES (1, 'pristine', ?, 0)`, pristine); err != nil {
		t.Fatalf("seed pristine template: %v", err)
	}

	// template_versions:历史版本同样带错序
	if _, err := s.db.Exec(`INSERT INTO template_versions (template_id, version, content) VALUES (?, 1, ?)`, templateID, noResolveLegacyContent); err != nil {
		t.Fatalf("seed template_versions: %v", err)
	}

	// system_settings.clash_template:超管全局默认
	if err := s.SetSetting(clashTemplateKey, noResolveLegacyContent); err != nil {
		t.Fatalf("seed clash_template setting: %v", err)
	}

	if err := s.migrateTemplateNoResolveOrder(); err != nil {
		t.Fatalf("migrateTemplateNoResolveOrder: %v", err)
	}

	assertContent := func(where string, query string, args ...any) {
		t.Helper()
		var got string
		if err := s.db.QueryRow(query, args...).Scan(&got); err != nil {
			t.Fatalf("read back %s: %v", where, err)
		}
		if got != noResolveFixedContent {
			t.Errorf("%s content after migration:\n%s\nwant:\n%s", where, got, noResolveFixedContent)
		}
	}
	assertContent("template", `SELECT content FROM template WHERE id = ?`, templateID)
	assertContent("template_versions", `SELECT content FROM template_versions WHERE template_id = ? AND version = 1`, templateID)
	assertContent("system_settings.clash_template", `SELECT value FROM system_settings WHERE key = ?`, clashTemplateKey)

	// 全正确模板迁移前后必须逐字节一致(diff 仅限错序行)
	var gotPristine string
	if err := s.db.QueryRow(`SELECT content FROM template WHERE name = 'pristine'`).Scan(&gotPristine); err != nil {
		t.Fatalf("read back pristine template: %v", err)
	}
	if gotPristine != pristine {
		t.Errorf("pristine template was rewritten:\n%s\nwant:\n%s", gotPristine, pristine)
	}

	// 重复执行(模拟再次启动):不产生二次改写
	if err := s.migrateTemplateNoResolveOrder(); err != nil {
		t.Fatalf("second migrateTemplateNoResolveOrder: %v", err)
	}
	assertContent("template after rerun", `SELECT content FROM template WHERE id = ?`, templateID)
	assertContent("template_versions after rerun", `SELECT content FROM template_versions WHERE template_id = ? AND version = 1`, templateID)
	assertContent("system_settings.clash_template after rerun", `SELECT value FROM system_settings WHERE key = ?`, clashTemplateKey)
}

// clash_template 键不存在(从未保存全局默认)时迁移必须正常通过。
func TestMigrateTemplateNoResolveOrder_NoSettingRow(t *testing.T) {
	s := newTestStore(t)
	if err := s.migrateTemplateNoResolveOrder(); err != nil {
		t.Fatalf("migrate without clash_template row: %v", err)
	}
}
