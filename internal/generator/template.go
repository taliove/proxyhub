package generator

import (
	_ "embed"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/taliove/proxyhub/internal/subscription"
)

// nodesPlaceholder 是模板中用于动态注入当前节点名的占位符。
// 出现在某个 proxy-group 的 proxies 数组里时，会被替换为当前节点池的所有节点名。
const nodesPlaceholder = "{{nodes}}"

// nodesRegionPrefix 分区占位符前缀，支持 {{nodes:region=HK}} 或 {{nodes:region=HK,SG,US}}
const nodesRegionPrefix = "{{nodes:region="

//go:embed default_template.yaml
var defaultTemplate string

// DefaultTemplate 返回内嵌的默认 Clash 配置模板（含 hosts/dns/proxy-groups/rules）。
// 首次启动初始化与「恢复默认」都从这里取值。
func DefaultTemplate() string {
	return defaultTemplate
}

// RenderTemplate 用当前节点池渲染 Clash 配置模板，产出完整的订阅 YAML。
//
// 渲染规则：
//   - 动态注入顶层 proxies 字段：节点池里每个节点的完整代理配置。
//   - 展开占位符：proxy-groups 中值为 "{{nodes}}" 的数组元素，替换为全部节点名。
//   - 其余字段（hosts/dns/rules 等）原样保留。
//
// 节点名会先去重（Clash 要求唯一），组里展开的名称与 proxies 字段里的名称保持一致。
func RenderTemplate(template string, nodes []*subscription.Node) ([]byte, error) {
	if template == "" {
		return nil, fmt.Errorf("template is empty")
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("no nodes to generate")
	}

	var cfg map[string]any
	if err := yaml.Unmarshal([]byte(template), &cfg); err != nil {
		return nil, fmt.Errorf("parse template: %w", err)
	}
	if cfg == nil {
		// 模板仅含空白/注释，解析出的映射为空
		return nil, fmt.Errorf("template has no content")
	}

	// 生成节点的完整代理配置 + 与之一一对应的（去重后）名称列表。
	proxies, names, err := buildProxies(nodes)
	if err != nil {
		return nil, err
	}
	if len(proxies) == 0 {
		return nil, fmt.Errorf("no convertible nodes")
	}

	// 按地区分组节点名（供分区占位符使用）
	nodesByRegion := groupNodesByRegion(nodes, names)

	cfg["proxies"] = proxies
	if err := expandNodesPlaceholder(cfg, names, nodesByRegion); err != nil {
		return nil, err
	}

	// 展开后可能出现空的 proxy-group（如某地区当前无可用节点，{{nodes:region=TW}}
	// 展开为空）。Clash 会拒绝 proxies/use 均为空的策略组,故在此剔除并清理悬空引用。
	pruneEmptyGroups(cfg)

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshal clash config: %w", err)
	}
	return data, nil
}

// buildProxies 把节点池转换为 Clash proxies 列表，并返回去重后的名称序列。
// 无法转换的节点被跳过；名称去重后与返回的 proxies 顺序一致。
func buildProxies(nodes []*subscription.Node) (proxies []map[string]any, names []string, err error) {
	proxies = make([]map[string]any, 0, len(nodes))
	names = make([]string, 0, len(nodes))
	seen := make(map[string]int)

	for _, node := range nodes {
		name := uniqueName(node.EffectiveName(), seen)
		proxy, convErr := ClashProxy(node, name)
		if convErr != nil {
			// 跳过无法转换的节点（类型不支持等），不因单个节点失败而中断整份订阅
			continue
		}
		proxies = append(proxies, proxy)
		names = append(names, name)
	}
	return proxies, names, nil
}

// expandNodesPlaceholder 遍历 proxy-groups，把每个组 proxies 数组里的占位符替换为节点名：
//   - "{{nodes}}" → 全部节点
//   - "{{nodes:region=HK}}" → 仅香港节点
//   - "{{nodes:region=HK,SG,US}}" → 香港+新加坡+美国节点
//
// 同一组内重复占位符只展开第一次。
func expandNodesPlaceholder(cfg map[string]any, allNames []string, nodesByRegion map[string][]string) error {
	groupsRaw, ok := cfg["proxy-groups"]
	if !ok {
		return nil // 没有策略组，无需展开
	}
	groups, ok := groupsRaw.([]any)
	if !ok {
		return fmt.Errorf("proxy-groups is not a list")
	}

	for _, groupRaw := range groups {
		group, ok := groupRaw.(map[string]any)
		if !ok {
			continue
		}
		proxiesRaw, ok := group["proxies"]
		if !ok {
			continue
		}
		items, ok := proxiesRaw.([]any)
		if !ok {
			continue
		}
		group["proxies"] = expandList(items, allNames, nodesByRegion)
	}
	return nil
}

// expandList 展开单个 proxies 列表中的占位符
func expandList(items []any, allNames []string, nodesByRegion map[string][]string) []any {
	out := make([]any, 0, len(items)+len(allNames))
	expanded := false // 防止同组内重复注入全量节点

	for _, item := range items {
		s, ok := item.(string)
		if !ok {
			out = append(out, item)
			continue
		}

		// {{nodes}} → 全部节点
		if s == nodesPlaceholder {
			if !expanded {
				for _, name := range allNames {
					out = append(out, name)
				}
				expanded = true
			}
			continue
		}

		// {{nodes:region=HK}} 或 {{nodes:region=HK,SG,US}}
		if strings.HasPrefix(s, nodesRegionPrefix) && strings.HasSuffix(s, "}}") {
			regionPart := strings.TrimPrefix(s, nodesRegionPrefix)
			regionPart = strings.TrimSuffix(regionPart, "}}")
			regions := strings.Split(regionPart, ",")

			for _, region := range regions {
				region = strings.TrimSpace(region)
				if names, ok := nodesByRegion[region]; ok {
					for _, name := range names {
						out = append(out, name)
					}
				}
			}
			continue
		}

		// 非占位符，原样保留
		out = append(out, item)
	}
	return out
}

// pruneEmptyGroups 剔除 proxies 为空且无 use(provider) 的策略组,并清理其他组、
// rules 中对已删除组的悬空引用。删除一个组可能使仅引用它的组也变空,故迭代到不动点。
//
// rules 中指向已删除组的策略会被改写为 DIRECT,避免 Clash 因引用不存在的策略而报错;
// 组名带 emoji/地区前缀,与规则的 type/value/options 字段几乎不可能冲突,按整字段精确匹配。
func pruneEmptyGroups(cfg map[string]any) {
	groupsRaw, ok := cfg["proxy-groups"].([]any)
	if !ok {
		return
	}

	dead := make(map[string]bool)
	groups := groupsRaw
	for {
		var kept []any
		removedThisRound := false
		for _, groupRaw := range groups {
			group, ok := groupRaw.(map[string]any)
			if !ok {
				kept = append(kept, groupRaw)
				continue
			}
			// 先剔除本组 proxies 里指向已删组的引用
			if items, ok := group["proxies"].([]any); ok {
				group["proxies"] = filterDeadRefs(items, dead)
			}
			if groupIsEmpty(group) {
				if name, ok := group["name"].(string); ok {
					dead[name] = true
				}
				removedThisRound = true
				continue
			}
			kept = append(kept, group)
		}
		groups = kept
		if !removedThisRound {
			break
		}
	}

	cfg["proxy-groups"] = groups
	if len(dead) > 0 {
		scrubRules(cfg, dead)
	}
}

// groupIsEmpty 判断策略组是否为 Clash 不接受的空组:proxies 为空且没有非空的 use。
func groupIsEmpty(group map[string]any) bool {
	if uses, ok := group["use"].([]any); ok && len(uses) > 0 {
		return false
	}
	items, ok := group["proxies"].([]any)
	return !ok || len(items) == 0
}

// filterDeadRefs 返回去掉指向已删组名后的新列表(不修改入参)。
func filterDeadRefs(items []any, dead map[string]bool) []any {
	out := make([]any, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok && dead[s] {
			continue
		}
		out = append(out, item)
	}
	return out
}

// scrubRules 将 rules 中指向已删除组的策略字段改写为 DIRECT。
func scrubRules(cfg map[string]any, dead map[string]bool) {
	rulesRaw, ok := cfg["rules"].([]any)
	if !ok {
		return
	}
	out := make([]any, 0, len(rulesRaw))
	for _, ruleRaw := range rulesRaw {
		s, ok := ruleRaw.(string)
		if !ok {
			out = append(out, ruleRaw)
			continue
		}
		fields := strings.Split(s, ",")
		for i, f := range fields {
			if dead[strings.TrimSpace(f)] {
				fields[i] = "DIRECT"
			}
		}
		out = append(out, strings.Join(fields, ","))
	}
	cfg["rules"] = out
}

// groupNodesByRegion 按地区分组节点名（已去重）
func groupNodesByRegion(nodes []*subscription.Node, uniqueNames []string) map[string][]string {
	// 建立名称→节点的映射（基于去重后的名称）
	nameToNode := make(map[string]*subscription.Node)
	seen := make(map[string]int)
	for _, node := range nodes {
		name := uniqueName(node.EffectiveName(), seen)
		nameToNode[name] = node
	}

	// 按地区分组
	result := make(map[string][]string)
	for _, name := range uniqueNames {
		node := nameToNode[name]
		if node == nil {
			continue
		}
		region := node.Region
		result[region] = append(result[region], name)
	}
	return result
}
