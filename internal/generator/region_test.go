package generator

import (
	"strings"
	"testing"

	"github.com/taliove/proxyhub/internal/subscription"
	"gopkg.in/yaml.v3"
)

func TestRegionPlaceholder(t *testing.T) {
	nodes := []*subscription.Node{
		{Name: "🇭🇰 HK-01", Type: "vmess", Server: "1.1.1.1", Port: 443, UUID: "xxx", Region: "HK"},
		{Name: "🇭🇰 HK-02", Type: "vmess", Server: "1.1.1.2", Port: 443, UUID: "yyy", Region: "HK"},
		{Name: "🇸🇬 SG-01", Type: "vmess", Server: "2.2.2.1", Port: 443, UUID: "zzz", Region: "SG"},
		{Name: "🇺🇸 US-01", Type: "vmess", Server: "3.3.3.1", Port: 443, UUID: "aaa", Region: "US"},
		{Name: "未知节点", Type: "vmess", Server: "4.4.4.1", Port: 443, UUID: "bbb", Region: "Unknown"},
	}

	template := `
proxy-groups:
  - name: 🇭🇰 香港节点
    type: select
    proxies:
      - '{{nodes:region=HK}}'
  - name: 🇸🇬 新加坡节点
    type: select
    proxies:
      - '{{nodes:region=SG}}'
  - name: 🌏 亚洲节点
    type: select
    proxies:
      - '{{nodes:region=HK,SG}}'
  - name: 🚀 全部节点
    type: select
    proxies:
      - '{{nodes}}'
`

	data, err := RenderTemplate(template, nodes)
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}

	var cfg map[string]any
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parse result: %v", err)
	}

	groups := cfg["proxy-groups"].([]any)

	// 检查香港组：只有 HK-01, HK-02
	hkGroup := groups[0].(map[string]any)
	hkProxies := hkGroup["proxies"].([]any)
	if len(hkProxies) != 2 {
		t.Errorf("HK group: want 2 nodes, got %d", len(hkProxies))
	}
	if !contains(hkProxies, "🇭🇰 HK-01") || !contains(hkProxies, "🇭🇰 HK-02") {
		t.Errorf("HK group missing expected nodes: %v", hkProxies)
	}

	// 检查新加坡组：只有 SG-01
	sgGroup := groups[1].(map[string]any)
	sgProxies := sgGroup["proxies"].([]any)
	if len(sgProxies) != 1 {
		t.Errorf("SG group: want 1 node, got %d", len(sgProxies))
	}

	// 检查亚洲组：HK + SG (3个节点)
	asiaGroup := groups[2].(map[string]any)
	asiaProxies := asiaGroup["proxies"].([]any)
	if len(asiaProxies) != 3 {
		t.Errorf("Asia group: want 3 nodes, got %d", len(asiaProxies))
	}

	// 检查全部组：5个节点
	allGroup := groups[3].(map[string]any)
	allProxies := allGroup["proxies"].([]any)
	if len(allProxies) != 5 {
		t.Errorf("All group: want 5 nodes, got %d", len(allProxies))
	}
}

func TestRegionPlaceholder_EmptyRegion(t *testing.T) {
	nodes := []*subscription.Node{
		{Name: "HK-01", Type: "vmess", Server: "1.1.1.1", Port: 443, UUID: "xxx", Region: "HK"},
	}

	template := `
proxy-groups:
  - name: 🇯🇵 日本节点
    type: select
    proxies:
      - '{{nodes:region=JP}}'
`

	data, err := RenderTemplate(template, nodes)
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}

	// 没有日本节点时,日本组展开为空。Clash 拒绝空的策略组,故应被整组剔除,
	// 而不是产出 `proxies: []`(那正是订阅校验失败的根因)。
	out := string(data)
	if strings.Contains(out, "proxies: []") {
		t.Errorf("empty JP group should be pruned, but got empty proxies array:\n%s", out)
	}
	if strings.Contains(out, "日本节点") {
		t.Errorf("empty JP group should be pruned from output:\n%s", out)
	}
}

func contains(items []any, target string) bool {
	for _, item := range items {
		if s, ok := item.(string); ok && s == target {
			return true
		}
	}
	return false
}
