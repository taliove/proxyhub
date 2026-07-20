package aggregator

import (
	"strings"

	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

// SourceDistribution 分发节点的来源标记
const SourceDistribution = "distribution"

// generateDistributionNodes 将分发路径转换为订阅节点
// 每个 DistributionPath 生成一个虚拟节点，代表一个分发入口
func generateDistributionNodes(cfg *store.DistributionConfig, paths []*store.DistributionPath) []*subscription.Node {
	if cfg == nil || !cfg.Enabled {
		return nil
	}

	nodes := make([]*subscription.Node, 0, len(paths))
	for _, path := range paths {
		if !path.Enabled {
			continue
		}

		node := &subscription.Node{
			Name:   path.Name,
			Type:   cfg.Protocol,
			Server: cfg.Domain,
			Port:   cfg.ListenPort,

			// 协议字段
			UUID:   cfg.UUID,
			TLS:    cfg.TLS,
			SNI:    cfg.Domain,
			Network: cfg.Network,

			// 元数据
			Source: SourceDistribution,
			Region: inferDistributionRegion(path),

			// 分发特有字段
			IsDistribution:   true,
			UpstreamNodeKeys: path.UpstreamNodeKeys,
			LBStrategy:       path.LBStrategy,
			DistributionPath: path.Path,

			// 初始状态（健康检查会更新）
			Available: true,
			Latency:   0,
		}

		nodes = append(nodes, node)
	}

	return nodes
}

// inferDistributionRegion 从分发路径推断地区
// 如果路径名包含明确地区标识，提取之；否则标记为 "DISTRIBUTION"
func inferDistributionRegion(path *store.DistributionPath) string {
	// 简单实现：根据路径名判断
	// 未来可以根据上游节点的 Region 聚合推断
	name := path.Name

	// 如果名称包含常见地区关键词，提取之
	regions := []string{"香港", "日本", "美国", "新加坡", "台湾", "韩国", "德国", "英国", "加拿大"}
	for _, region := range regions {
		if strings.Contains(name, region) {
			return region
		}
	}

	// 默认标记为分发节点
	return "DISTRIBUTION"
}
