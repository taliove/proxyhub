package generator

import (
	"fmt"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/taliove/proxyhub/internal/subscription"
)

// clashDoc 解析生成的 Clash 配置用于断言。
type clashDoc struct {
	Hosts   map[string]string `yaml:"hosts"`
	DNS     map[string]any    `yaml:"dns"`
	Proxies []struct {
		Name   string `yaml:"name"`
		Type   string `yaml:"type"`
		Server string `yaml:"server"`
		Port   int    `yaml:"port"`
	} `yaml:"proxies"`
	ProxyGroups []struct {
		Name    string   `yaml:"name"`
		Type    string   `yaml:"type"`
		Proxies []string `yaml:"proxies"`
	} `yaml:"proxy-groups"`
	Rules []string `yaml:"rules"`
}

func parseClash(t *testing.T, data []byte) clashDoc {
	t.Helper()
	var doc clashDoc
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("generated config is not valid YAML: %v\n---\n%s", err, data)
	}
	return doc
}

func TestRenderTemplate_Success(t *testing.T) {
	tmpl := `
mode: rule
proxy-groups:
  - name: 手动切换
    type: select
    proxies:
      - DIRECT
      - '{{nodes}}'
rules:
  - MATCH,手动切换
`
	data, err := RenderTemplate(tmpl, sampleNodes())
	if err != nil {
		t.Fatalf("RenderTemplate() error = %v", err)
	}
	doc := parseClash(t, data)

	// proxies 字段应包含全部 4 个节点的完整配置
	if len(doc.Proxies) != 4 {
		t.Errorf("len(proxies) = %d, want 4", len(doc.Proxies))
	}
	// 手动切换组应为 DIRECT + 4 个节点名
	if len(doc.ProxyGroups) != 1 {
		t.Fatalf("len(proxy-groups) = %d, want 1", len(doc.ProxyGroups))
	}
	g := doc.ProxyGroups[0]
	if len(g.Proxies) != 5 {
		t.Errorf("group proxies = %d, want 5 (DIRECT + 4 nodes)", len(g.Proxies))
	}
	if g.Proxies[0] != "DIRECT" {
		t.Errorf("group proxies[0] = %q, want DIRECT", g.Proxies[0])
	}
}

func TestRenderTemplate_MultiplePlaceholders(t *testing.T) {
	tmpl := `
proxy-groups:
  - name: 手动切换
    type: select
    proxies: [DIRECT, '{{nodes}}']
  - name: 流媒体
    type: select
    proxies: ['手动切换', '{{nodes}}']
rules:
  - MATCH,手动切换
`
	data, err := RenderTemplate(tmpl, sampleNodes())
	if err != nil {
		t.Fatalf("RenderTemplate() error = %v", err)
	}
	doc := parseClash(t, data)

	if len(doc.ProxyGroups) != 2 {
		t.Fatalf("len(proxy-groups) = %d, want 2", len(doc.ProxyGroups))
	}
	// 组 1：DIRECT + 4 节点
	if len(doc.ProxyGroups[0].Proxies) != 5 {
		t.Errorf("group[0] proxies = %d, want 5", len(doc.ProxyGroups[0].Proxies))
	}
	// 组 2：手动切换 + 4 节点
	if len(doc.ProxyGroups[1].Proxies) != 5 {
		t.Errorf("group[1] proxies = %d, want 5", len(doc.ProxyGroups[1].Proxies))
	}
	if doc.ProxyGroups[1].Proxies[0] != "手动切换" {
		t.Errorf("group[1] proxies[0] = %q, want 手动切换", doc.ProxyGroups[1].Proxies[0])
	}
}

func TestRenderTemplate_NoPlaceholder(t *testing.T) {
	// 没有占位符的模板：proxy-groups 保持原样，但仍动态注入 proxies 字段
	tmpl := `
proxy-groups:
  - name: 直连
    type: select
    proxies: [DIRECT]
rules:
  - MATCH,直连
`
	data, err := RenderTemplate(tmpl, sampleNodes())
	if err != nil {
		t.Fatalf("RenderTemplate() error = %v", err)
	}
	doc := parseClash(t, data)

	if len(doc.Proxies) != 4 {
		t.Errorf("len(proxies) = %d, want 4 (nodes always injected)", len(doc.Proxies))
	}
	if len(doc.ProxyGroups[0].Proxies) != 1 || doc.ProxyGroups[0].Proxies[0] != "DIRECT" {
		t.Errorf("group proxies = %v, want [DIRECT]", doc.ProxyGroups[0].Proxies)
	}
}

func TestRenderTemplate_EmptyNodes(t *testing.T) {
	tmpl := `
proxy-groups:
  - name: 手动切换
    type: select
    proxies: [DIRECT, '{{nodes}}']
rules:
  - MATCH,手动切换
`
	_, err := RenderTemplate(tmpl, nil)
	if err == nil {
		t.Fatal("RenderTemplate(nil nodes) expected error, got nil")
	}
}

func TestRenderTemplate_InvalidYAML(t *testing.T) {
	tmpl := "proxy-groups: [ this is : not valid : yaml"
	_, err := RenderTemplate(tmpl, sampleNodes())
	if err == nil {
		t.Fatal("RenderTemplate(invalid yaml) expected error, got nil")
	}
}

func TestRenderTemplate_PreservesOtherFields(t *testing.T) {
	tmpl := `
mode: rule
hosts:
  example.com: 1.2.3.4
dns:
  enable: true
  enhanced-mode: fake-ip
proxy-groups:
  - name: 手动切换
    type: select
    proxies: [DIRECT, '{{nodes}}']
rules:
  - DOMAIN-SUFFIX,google.com,手动切换
  - MATCH,DIRECT
`
	data, err := RenderTemplate(tmpl, sampleNodes())
	if err != nil {
		t.Fatalf("RenderTemplate() error = %v", err)
	}
	doc := parseClash(t, data)

	if doc.Hosts["example.com"] != "1.2.3.4" {
		t.Errorf("hosts not preserved: %v", doc.Hosts)
	}
	if doc.DNS["enhanced-mode"] != "fake-ip" {
		t.Errorf("dns not preserved: %v", doc.DNS)
	}
	if len(doc.Rules) != 2 {
		t.Errorf("len(rules) = %d, want 2", len(doc.Rules))
	}
}

func TestRenderTemplate_NodeNamesInProxies(t *testing.T) {
	// 注入的 proxies 字段里的节点名，必须与展开到组里的节点名一致
	tmpl := `
proxy-groups:
  - name: 手动切换
    type: select
    proxies: [DIRECT, '{{nodes}}']
rules:
  - MATCH,手动切换
`
	nodes := []*subscription.Node{
		{Name: "重复名", Type: "ss", Server: "a.com", Port: 1, Cipher: "aes-256-gcm", Password: "p"},
		{Name: "重复名", Type: "ss", Server: "b.com", Port: 2, Cipher: "aes-256-gcm", Password: "p"},
	}
	data, err := RenderTemplate(tmpl, nodes)
	if err != nil {
		t.Fatalf("RenderTemplate() error = %v", err)
	}
	doc := parseClash(t, data)

	// 去重后名称应唯一，且组里的名称与 proxies 里的名称一一对应
	proxyNames := map[string]bool{}
	for _, p := range doc.Proxies {
		if proxyNames[p.Name] {
			t.Errorf("duplicate proxy name: %s", p.Name)
		}
		proxyNames[p.Name] = true
	}
	// 组里除 DIRECT 外的名称都应在 proxies 中存在
	for _, name := range doc.ProxyGroups[0].Proxies {
		if name == "DIRECT" {
			continue
		}
		if !proxyNames[name] {
			t.Errorf("group references unknown proxy %q", name)
		}
	}
}

func TestRenderTemplate_PrunesEmptyRegionGroup(t *testing.T) {
	// 节点池里没有 TW 节点,台湾组展开为空,应被剔除;同时清理引用它的组与规则。
	tmpl := `
proxy-groups:
  - name: 🚀 节点选择
    type: select
    proxies: ['🇭🇰 香港节点', '🇹🇼 台湾节点', '{{nodes}}']
  - name: 🇭🇰 香港节点
    type: url-test
    proxies: ['{{nodes:region=HK}}']
  - name: 🇹🇼 台湾节点
    type: url-test
    proxies: ['{{nodes:region=TW}}']
rules:
  - DOMAIN-SUFFIX,tw.example.com,🇹🇼 台湾节点
  - MATCH,🚀 节点选择
`
	data, err := RenderTemplate(tmpl, sampleNodes())
	if err != nil {
		t.Fatalf("RenderTemplate() error = %v", err)
	}
	doc := parseClash(t, data)

	// 台湾组应被删除
	for _, g := range doc.ProxyGroups {
		if strings.Contains(g.Name, "台湾") {
			t.Errorf("empty 台湾 group should be pruned, got %+v", g)
		}
		// 存活的组不得再引用已删组
		for _, p := range g.Proxies {
			if strings.Contains(p, "台湾") {
				t.Errorf("group %q still references pruned 台湾 group", g.Name)
			}
		}
		// 存活的组不得为空(否则 Clash 校验失败)
		if len(g.Proxies) == 0 {
			t.Errorf("group %q has empty proxies after prune", g.Name)
		}
	}
	// 指向已删组的规则应改写为 DIRECT
	for _, r := range doc.Rules {
		if strings.Contains(r, "台湾") {
			t.Errorf("rule still references pruned group: %q", r)
		}
	}
	// 香港组有节点,应保留
	var hkKept bool
	for _, g := range doc.ProxyGroups {
		if strings.Contains(g.Name, "香港") {
			hkKept = true
		}
	}
	if !hkKept {
		t.Error("香港 group with nodes should be kept")
	}
}

func TestRenderTemplate_PruneCascades(t *testing.T) {
	// 中转组只引用台湾组;台湾组空 → 被删 → 中转组也变空 → 也应被删(不动点迭代)。
	tmpl := `
proxy-groups:
  - name: 中转
    type: select
    proxies: ['🇹🇼 台湾节点']
  - name: 🇹🇼 台湾节点
    type: url-test
    proxies: ['{{nodes:region=TW}}']
  - name: 兜底
    type: select
    proxies: [DIRECT, '中转']
rules:
  - MATCH,兜底
`
	data, err := RenderTemplate(tmpl, sampleNodes())
	if err != nil {
		t.Fatalf("RenderTemplate() error = %v", err)
	}
	doc := parseClash(t, data)

	for _, g := range doc.ProxyGroups {
		if g.Name == "中转" || strings.Contains(g.Name, "台湾") {
			t.Errorf("cascaded-empty group %q should be pruned", g.Name)
		}
		if len(g.Proxies) == 0 {
			t.Errorf("group %q empty after prune", g.Name)
		}
	}
	// 兜底组保留(含 DIRECT),对中转的引用被清除
	var fb *struct {
		Name    string   `yaml:"name"`
		Type    string   `yaml:"type"`
		Proxies []string `yaml:"proxies"`
	}
	for i := range doc.ProxyGroups {
		if doc.ProxyGroups[i].Name == "兜底" {
			fb = &doc.ProxyGroups[i]
		}
	}
	if fb == nil {
		t.Fatal("兜底 group should survive")
	}
	for _, p := range fb.Proxies {
		if p == "中转" {
			t.Errorf("兜底 still references pruned 中转")
		}
	}
}

func TestRenderTemplate_EmptyTemplate(t *testing.T) {
	_, err := RenderTemplate("", sampleNodes())
	if err == nil {
		t.Fatal("RenderTemplate(empty template) expected error, got nil")
	}
}

func TestDefaultTemplate_AndroidCompat(t *testing.T) {
	// Android(mihomo/FLClash)客户端原样使用订阅内的 dns 段,桌面/iOS 客户端普遍覆写它,
	// 所以默认模板的 dns 基线必须在 Android 直连环境下自洽(issue #114):
	// - listen 禁 <1024 端口(Android 无 root 绑不了),且只绑回环
	// - 禁 fallback / 境外 DoT(境内直连不可达,fallback-filter 会拖死全部境外域名解析)
	// - 必备 default-nameserver 与 proxy-server-nameserver(节点域名解析独立通道)
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(DefaultTemplate()), &doc); err != nil {
		t.Fatalf("embedded default template is not valid YAML: %v", err)
	}

	dns, ok := doc["dns"].(map[string]any)
	if !ok {
		t.Fatal("default template has no dns section")
	}

	listen, _ := dns["listen"].(string)
	host, port, _ := strings.Cut(listen, ":")
	if host != "127.0.0.1" {
		t.Errorf("dns.listen host = %q, want loopback(与 allow-lan: false 立场一致)", host)
	}
	var portNum int
	if _, err := fmt.Sscanf(port, "%d", &portNum); err != nil || portNum < 1024 {
		t.Errorf("dns.listen port = %q, want >=1024(Android 无 root 绑不了特权端口)", port)
	}

	if _, has := dns["fallback"]; has {
		t.Error("dns.fallback 存在:境外 DoT 境内直连不可达,会拖死全部境外域名解析")
	}

	assertStringList := func(key string) []string {
		t.Helper()
		raw, ok := dns[key].([]any)
		if !ok || len(raw) == 0 {
			t.Fatalf("dns.%s 缺失或为空", key)
		}
		out := make([]string, 0, len(raw))
		for _, v := range raw {
			out = append(out, fmt.Sprint(v))
		}
		return out
	}

	for _, key := range []string{"default-nameserver", "proxy-server-nameserver", "nameserver"} {
		for _, ns := range assertStringList(key) {
			if strings.Contains(ns, "1.1.1.1") || strings.Contains(ns, "8.8.8.8") || strings.HasPrefix(ns, "tls://") {
				t.Errorf("dns.%s 含境外/DoT 地址 %q:境内直连不可达", key, ns)
			}
		}
	}

	for _, key := range []string{"use-hosts", "use-system-hosts"} {
		if dns[key] != true {
			t.Errorf("dns.%s = %v, want true(hosts 保真依赖)", key, dns[key])
		}
	}

	if doc["unified-delay"] != true {
		t.Errorf("unified-delay = %v, want true", doc["unified-delay"])
	}

	if raw, ok := dns["fake-ip-filter"].([]any); !ok || len(raw) < 10 {
		t.Errorf("fake-ip-filter 条目 = %d, want 通行清单规模(>=10)", len(raw))
	}
}

func TestDefaultTemplate_Valid(t *testing.T) {
	// 内嵌默认模板必须可渲染,且产出结构与模板自身一致。
	// 断言从模板内容推导(首组名、顶层段),不硬编码规模数字——模板再调整不会炸同类问题。
	raw := DefaultTemplate()
	var tmplDoc clashDoc
	if err := yaml.Unmarshal([]byte(raw), &tmplDoc); err != nil {
		t.Fatalf("embedded default template is not valid YAML: %v", err)
	}
	if len(tmplDoc.ProxyGroups) == 0 {
		t.Fatal("default template declares no proxy-groups")
	}

	data, err := RenderTemplate(raw, sampleNodes())
	if err != nil {
		t.Fatalf("RenderTemplate(DefaultTemplate) error = %v", err)
	}
	doc := parseClash(t, data)

	// 模板声明了的顶层段,渲染后必须保留
	if len(tmplDoc.DNS) > 0 && len(doc.DNS) == 0 {
		t.Error("dns section declared in template but missing after render")
	}
	if len(tmplDoc.Hosts) > 0 && len(doc.Hosts) == 0 {
		t.Error("hosts section declared in template but missing after render")
	}

	// 首组是 select 组、引用全部节点占位符,永远非空,裁剪不会改变它;
	// 渲染结果的首组必须与模板声明的首组同名
	if len(doc.ProxyGroups) == 0 {
		t.Fatal("default template produced no proxy-groups")
	}
	if doc.ProxyGroups[0].Name != tmplDoc.ProxyGroups[0].Name {
		t.Errorf("first group = %q, want template's first group %q",
			doc.ProxyGroups[0].Name, tmplDoc.ProxyGroups[0].Name)
	}

	// 4 个 sample 节点必须全部注入
	if len(doc.Proxies) != 4 {
		t.Errorf("proxies = %d, want 4 (injected nodes)", len(doc.Proxies))
	}

	// 裁剪完整性:存活组必须全部非空,规则引用的组必须存在(或为 DIRECT)
	groupNames := map[string]bool{"DIRECT": true}
	for _, g := range doc.ProxyGroups {
		if len(g.Proxies) == 0 {
			t.Errorf("group %q has empty proxies after prune", g.Name)
		}
		groupNames[g.Name] = true
	}
	if len(doc.Rules) == 0 {
		t.Fatal("default template produced no rules")
	}
	for _, r := range doc.Rules {
		parts := strings.Split(r, ",")
		if len(parts) < 2 {
			t.Errorf("malformed rule %q", r)
			continue
		}
		target := parts[len(parts)-1]
		if target == "no-resolve" { // IP-CIDR,...,GROUP,no-resolve
			target = parts[len(parts)-2]
		}
		if !groupNames[target] {
			t.Errorf("rule %q references unknown group %q", r, target)
		}
	}
	// 必须有 MATCH 兜底规则收尾
	if last := doc.Rules[len(doc.Rules)-1]; !strings.HasPrefix(last, "MATCH,") {
		t.Errorf("last rule = %q, want MATCH catch-all", last)
	}
}
