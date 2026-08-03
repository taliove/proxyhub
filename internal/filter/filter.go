package filter

import (
	"math"
	"sort"

	"github.com/taliove/proxyhub/internal/subscription"
)

// Filter 节点过滤器
type Filter struct {
	nodesPerRegion int
	deduplicate    bool
}

// NewFilter 创建过滤器
func NewFilter(nodesPerRegion int, deduplicate bool) *Filter {
	return &Filter{
		nodesPerRegion: nodesPerRegion,
		deduplicate:    deduplicate,
	}
}

// Apply 应用过滤规则
func (f *Filter) Apply(nodes []*subscription.Node) []*subscription.Node {
	if f.deduplicate {
		nodes = f.deduplicateNodes(nodes)
	}

	nodes = f.selectBestByRegion(nodes)
	f.sortByLatency(nodes)

	return nodes
}

// cmpLatency 返回排序比较用的延迟：未检测节点视为 +∞，恒排在已检测节点之后；
// 只改比较，节点的 Latency 字段原样保留。
func cmpLatency(n *subscription.Node) int {
	if n.Unchecked() {
		return math.MaxInt
	}
	return n.Latency
}

// lessNode 报告 a 是否应排在 b 前：先比 cmpLatency，平局按 NodeKey 字典序（确定性
// 次级键）；完全平局返回 false，稳定序下先出现者胜（与去重历史严格 < 语义一致）。
func lessNode(a, b *subscription.Node) bool {
	la, lb := cmpLatency(a), cmpLatency(b)
	if la != lb {
		return la < lb
	}
	return a.NodeKey() < b.NodeKey()
}

// deduplicateNodes 去重（相同 IP+端口只保留一个，保留延迟更低的）。
// 自建节点（SourceSelfHosted）豁免：始终原样保留，避免与机场节点 NodeKey 碰撞时被并掉。
func (f *Filter) deduplicateNodes(nodes []*subscription.Node) []*subscription.Node {
	seen := make(map[string]*subscription.Node)
	var selfHosted []*subscription.Node

	for _, node := range nodes {
		if node.Source == subscription.SourceSelfHosted {
			selfHosted = append(selfHosted, node)
			continue
		}
		key := node.NodeKey()
		existing, exists := seen[key]

		if !exists || lessNode(node, existing) {
			seen[key] = node
		}
	}

	result := make([]*subscription.Node, 0, len(seen)+len(selfHosted))
	for _, node := range seen {
		result = append(result, node)
	}
	result = append(result, selfHosted...)

	return result
}

// selectBestByRegion 每个地区保留延迟最低的 N 个节点。
// 自建节点（SourceSelfHosted）豁免：不受 nodesPerRegion 上限约束，全部保留（常驻安全网）。
func (f *Filter) selectBestByRegion(nodes []*subscription.Node) []*subscription.Node {
	// nodesPerRegion <= 0 表示不限制,保留全部节点
	if f.nodesPerRegion <= 0 {
		return nodes
	}

	// 按地区分组；自建节点单独收集，始终全部保留
	byRegion := make(map[string][]*subscription.Node)
	var selfHosted []*subscription.Node
	for _, node := range nodes {
		if node.Source == subscription.SourceSelfHosted {
			selfHosted = append(selfHosted, node)
			continue
		}
		region := node.Region
		if region == "" {
			region = "Unknown"
		}
		byRegion[region] = append(byRegion[region], node)
	}

	// 每个地区按延迟排序，取前 N 个（未检测节点视为 +∞ 垫后；SliceStable 保证
	// 同 cmpLatency 时先出现者胜，行为可测）
	var result []*subscription.Node
	for _, regionNodes := range byRegion {
		sort.SliceStable(regionNodes, func(i, j int) bool {
			return lessNode(regionNodes[i], regionNodes[j])
		})

		limit := f.nodesPerRegion
		if len(regionNodes) < limit {
			limit = len(regionNodes)
		}

		result = append(result, regionNodes[:limit]...)
	}

	return append(result, selfHosted...)
}

// sortByLatency 按延迟从低到高排序（未检测节点视为 +∞ 垫后；SliceStable 保证
// 同 cmpLatency 时相对序确定，行为可测）
func (f *Filter) sortByLatency(nodes []*subscription.Node) {
	sort.SliceStable(nodes, func(i, j int) bool {
		return lessNode(nodes[i], nodes[j])
	})
}

// FilterAvailable 只保留可用节点。
// 三态语义:已确认死亡(DetectionKind 非空且 Available=false)被过滤;未检测节点
// (DetectionKind=="")放行,由后续排位逻辑垫后。
// 自建节点（SourceSelfHosted）豁免：即使检测为不可用也保留（常驻安全网）。
func FilterAvailable(nodes []*subscription.Node) []*subscription.Node {
	var result []*subscription.Node
	for _, node := range nodes {
		if node.Source == subscription.SourceSelfHosted || node.Available || node.Unchecked() {
			result = append(result, node)
		}
	}
	return result
}

// FilterByLatencyThreshold 过滤延迟超过阈值的节点。
// 自建节点（SourceSelfHosted）豁免：不受延迟阈值约束（常驻安全网）。
func FilterByLatencyThreshold(nodes []*subscription.Node, maxLatency int) []*subscription.Node {
	var result []*subscription.Node
	for _, node := range nodes {
		if node.Source == subscription.SourceSelfHosted || node.Latency <= maxLatency {
			result = append(result, node)
		}
	}
	return result
}
