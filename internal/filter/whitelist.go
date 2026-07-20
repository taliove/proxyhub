package filter

import "github.com/taliove/proxyhub/internal/subscription"

// FilterByWhitelist 只保留名称命中任一关键词的机场节点（子串匹配、不区分大小写）。
// 与 FilterByKeywords 方向相反：白名单是"要哪些"，黑名单是"排除哪些"。
// 自建节点（Source == subscription.SourceSelfHosted）始终豁免，作为 FailBack 安全网保留。
// keywords 为空（或全为空白）时不启用白名单，原样返回。返回新切片，不修改入参（见 ADR 0009）。
func FilterByWhitelist(nodes []*subscription.Node, keywords []string) []*subscription.Node {
	// keepOnMatch=true：只保留命中的（白名单语义）
	return filterByKeywordMatch(nodes, keywords, true)
}
