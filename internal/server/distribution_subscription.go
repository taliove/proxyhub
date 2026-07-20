package server

import (
	"strings"

	"github.com/taliove/proxyhub/internal/subscription"
)

// generateDistributionNodes 根据流量分发配置生成虚拟节点列表，用于订阅生成。
// 这些节点指向 ProxyHub 自身（作为代理中继），而非直连上游节点。
//
// 每个分发路径生成一个节点，协议和传输方式继承全局配置，通过 Path 区分路由。
// 返回的节点可与机场节点混合后一起下发到订阅中，客户端可自由选择使用。
func (s *Server) generateDistributionNodes() ([]*subscription.Node, error) {
	// 读取全局配置
	cfg, err := s.st.GetDistributionConfig()
	if err != nil {
		return nil, err
	}

	// 未启用或配置为空，返回空列表
	if cfg == nil || !cfg.Enabled {
		return nil, nil
	}

	// 读取所有路径
	paths, err := s.st.ListDistributionPaths()
	if err != nil {
		return nil, err
	}

	if len(paths) == 0 {
		return nil, nil
	}

	nodes := make([]*subscription.Node, 0, len(paths))
	for _, path := range paths {
		if !path.Enabled {
			continue
		}

		node := &subscription.Node{
			Name:   path.Name, // 分发路径的显示名称
			Type:   cfg.Protocol,
			Server: cfg.Domain,
			Port:   cfg.ListenPort,
			UUID:   cfg.UUID,
			TLS:    cfg.TLS,
			SNI:    cfg.Domain,
			Source: "distribution", // 特殊来源标记，区分于机场节点

			// 健康状态：分发节点默认可用（实际可用性由 Xray 保证）
			Available: true,
			Latency:   0, // 延迟未知，客户端自测

			// 地区信息：从路径名称推断
			Region: inferRegionFromPathName(path.Name),
		}

		// 根据传输方式设置 Network 和 Path/ServiceName
		node.Network = cfg.Network
		switch cfg.Network {
		case "grpc":
			node.GrpcServiceName = path.Path // gRPC 通过 serviceName 路由
		case "ws":
			// WebSocket 通过 path 路由，具体字段名取决于协议
			// 这里暂时不设置，由订阅生成器根据协议处理
			// node.WSPath = path.Path
		}

		nodes = append(nodes, node)
	}

	return nodes, nil
}

// inferRegionFromPathName 从路径名称推断地区信息（用于显示）。
// 简单实现：从名称中查找常见地区关键词，未匹配则返回 "分发"。
func inferRegionFromPathName(name string) string {
	// 常见地区关键词映射
	regions := map[string]string{
		"香港": "香港", "HK": "香港", "hk": "香港",
		"台湾": "台湾", "TW": "台湾", "tw": "台湾",
		"日本": "日本", "JP": "日本", "jp": "日本",
		"新加坡": "新加坡", "SG": "新加坡", "sg": "新加坡",
		"美国": "美国", "US": "美国", "us": "美国",
		"韩国": "韩国", "KR": "韩国", "kr": "韩国",
		"英国": "英国", "UK": "英国", "uk": "英国",
	}

	// 遍历关键词，查找匹配
	for keyword, region := range regions {
		if strings.Contains(name, keyword) {
			return region
		}
	}

	// 未匹配到地区，返回通用标记
	return "分发"
}

// distributionEnabled 检查流量分发功能是否已启用
func (s *Server) distributionEnabled() bool {
	cfg, err := s.st.GetDistributionConfig()
	if err != nil {
		return false
	}
	return cfg != nil && cfg.Enabled
}
