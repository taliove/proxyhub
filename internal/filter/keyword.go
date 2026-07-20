package filter

import (
	"strings"

	"github.com/taliove/proxyhub/internal/subscription"
)

// SplitKeywords 把系统设置里的原始关键词文本拆成关键词列表。
// 支持逗号（中英文）与换行混合分隔，去除首尾空白，丢弃空片段。
func SplitKeywords(raw string) []string {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '，' || r == '\n' || r == '\r'
	})
	keywords := make([]string, 0, len(fields))
	for _, f := range fields {
		if kw := strings.TrimSpace(f); kw != "" {
			keywords = append(keywords, kw)
		}
	}
	return keywords
}

// FilterByKeywords 剔除名称命中任一关键词的机场节点（子串匹配、不区分大小写）。
// 自建节点（Source == subscription.SourceSelfHosted）始终豁免，作为 FailBack 安全网保留。
// keywords 为空（或全为空白）时原样返回。返回新切片，不修改入参（见 ADR 0005）。
func FilterByKeywords(nodes []*subscription.Node, keywords []string) []*subscription.Node {
	// keepOnMatch=false：命中即剔除（黑名单语义）
	return filterByKeywordMatch(nodes, keywords, false)
}

// filterByKeywordMatch 是白/黑名单共用的过滤核心。keepOnMatch 决定命中后保留还是剔除：
//   - true  → 白名单：只保留命中的
//   - false → 黑名单：剔除命中的
//
// 自建节点始终豁免；关键词全为空白时不启用（原样返回）；返回新切片，不修改入参。
func filterByKeywordMatch(nodes []*subscription.Node, keywords []string, keepOnMatch bool) []*subscription.Node {
	normalized := make([]string, 0, len(keywords))
	for _, kw := range keywords {
		if kw = strings.TrimSpace(strings.ToLower(kw)); kw != "" {
			normalized = append(normalized, kw)
		}
	}
	if len(normalized) == 0 {
		return nodes
	}

	result := make([]*subscription.Node, 0, len(nodes))
	for _, node := range nodes {
		if node.Source == subscription.SourceSelfHosted {
			result = append(result, node)
			continue
		}
		if matchesAny(strings.ToLower(node.Name), normalized) == keepOnMatch {
			result = append(result, node)
		}
	}
	return result
}

// matchesAny 报告 name 是否包含 keywords 中任一子串（调用方已保证均为小写）
func matchesAny(name string, keywords []string) bool {
	for _, kw := range keywords {
		if strings.Contains(name, kw) {
			return true
		}
	}
	return false
}
