package distribution

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

// RoutingBuilder builds Xray routing configuration
type RoutingBuilder struct{}

// XrayConfig represents Xray-core configuration structure
type XrayConfig struct {
	Log       *LogConfig       `json:"log,omitempty"`
	Inbounds  []*InboundConfig `json:"inbounds"`
	Outbounds []*OutboundConfig `json:"outbounds"`
	Routing   *RoutingConfig   `json:"routing,omitempty"`
}

// LogConfig represents Xray log configuration
type LogConfig struct {
	Loglevel string `json:"loglevel"`
}

// InboundConfig represents an inbound configuration
type InboundConfig struct {
	Tag      string                 `json:"tag"`
	Listen   string                 `json:"listen,omitempty"`
	Port     int                    `json:"port"`
	Protocol string                 `json:"protocol"`
	Settings map[string]interface{} `json:"settings,omitempty"`
	StreamSettings *StreamSettings   `json:"streamSettings,omitempty"`
}

// OutboundConfig represents an outbound configuration
type OutboundConfig struct {
	Tag      string                 `json:"tag"`
	Protocol string                 `json:"protocol"`
	Settings map[string]interface{} `json:"settings,omitempty"`
	StreamSettings *StreamSettings   `json:"streamSettings,omitempty"`
}

// StreamSettings represents stream settings for transport
type StreamSettings struct {
	Network  string                 `json:"network"`
	Security string                 `json:"security,omitempty"`
	TLSSettings *TLSSettings        `json:"tlsSettings,omitempty"`
	WSSettings  *WSSettings         `json:"wsSettings,omitempty"`
	GrpcSettings *GrpcSettings      `json:"grpcSettings,omitempty"`
}

// TLSSettings represents TLS configuration
type TLSSettings struct {
	ServerName   string   `json:"serverName,omitempty"`
	Certificates []Certificate `json:"certificates,omitempty"`
}

// Certificate represents a TLS certificate
type Certificate struct {
	CertificateFile string `json:"certificateFile"`
	KeyFile         string `json:"keyFile"`
}

// WSSettings represents WebSocket settings
type WSSettings struct {
	Path string `json:"path"`
}

// GrpcSettings represents gRPC settings
type GrpcSettings struct {
	ServiceName string `json:"serviceName"`
}

// RoutingConfig represents routing rules
type RoutingConfig struct {
	Rules []*RoutingRule `json:"rules"`
}

// RoutingRule represents a routing rule
type RoutingRule struct {
	Type        string   `json:"type"`
	InboundTag  []string `json:"inboundTag,omitempty"`
	OutboundTag string   `json:"outboundTag"`
	Network     string   `json:"network,omitempty"`
	// For matching
	Domain []string `json:"domain,omitempty"`
	Path   []string `json:"path,omitempty"`
}

// BuildXrayConfig generates Xray configuration from distribution settings
func (rb *RoutingBuilder) BuildXrayConfig(
	cfg *store.DistributionConfig,
	nodes []*store.DistributionNode,
	nodePool []*subscription.Node,
) (*XrayConfig, error) {
	if cfg == nil {
		return nil, errors.New("distribution config is nil")
	}

	// Build node lookup map from node pool
	nodeMap := buildNodeMap(nodePool)

	// Build inbound
	inbound, err := rb.buildInbound(cfg)
	if err != nil {
		return nil, fmt.Errorf("build inbound: %w", err)
	}

	// Build outbounds for each distribution node
	outbounds := []*OutboundConfig{}
	routingRules := []*RoutingRule{}

	for _, distNode := range nodes {
		if !distNode.Enabled {
			continue
		}

		// Resolve upstream nodes from node pool
		upstreamNodes := make([]*subscription.Node, 0, len(distNode.UpstreamNodeKeys))
		for _, nodeKey := range distNode.UpstreamNodeKeys {
			node, ok := nodeMap[nodeKey]
			if !ok {
				return nil, fmt.Errorf("upstream node not found: %s (distribution node: %s)", nodeKey, distNode.Name)
			}
			upstreamNodes = append(upstreamNodes, node)
		}

		if len(upstreamNodes) == 0 {
			return nil, fmt.Errorf("distribution node %s has no upstream nodes", distNode.Name)
		}

		// Create outbound(s) for this distribution node
		outboundTag := fmt.Sprintf("node_%d", distNode.ID)

		if len(upstreamNodes) == 1 {
			// Single upstream: direct outbound
			outbound, err := rb.buildOutbound(outboundTag, upstreamNodes[0])
			if err != nil {
				return nil, fmt.Errorf("build outbound for distribution node %s: %w", distNode.Name, err)
			}
			outbounds = append(outbounds, outbound)
		} else {
			// Multiple upstreams: create balancer
			balancer, nodeOutbounds, err := rb.buildBalancer(outboundTag, distNode.LBStrategy, upstreamNodes)
			if err != nil {
				return nil, fmt.Errorf("build balancer for distribution node %s: %w", distNode.Name, err)
			}
			outbounds = append(outbounds, balancer)
			outbounds = append(outbounds, nodeOutbounds...)
		}

		// Create routing rule
		rule, err := rb.buildRoutingRule(cfg, distNode, outboundTag)
		if err != nil {
			return nil, fmt.Errorf("build routing rule for distribution node %s: %w", distNode.Name, err)
		}
		routingRules = append(routingRules, rule)
	}

	// Add default direct outbound
	outbounds = append(outbounds, &OutboundConfig{
		Tag:      "direct",
		Protocol: "freedom",
	})

	xrayConfig := &XrayConfig{
		Log: &LogConfig{
			Loglevel: "warning",
		},
		Inbounds:  []*InboundConfig{inbound},
		Outbounds: outbounds,
	}

	if len(routingRules) > 0 {
		xrayConfig.Routing = &RoutingConfig{
			Rules: routingRules,
		}
	}

	return xrayConfig, nil
}

// buildNodeMap creates a lookup map from NodeKey to Node
func buildNodeMap(nodes []*subscription.Node) map[string]*subscription.Node {
	nodeMap := make(map[string]*subscription.Node)
	for _, node := range nodes {
		nodeMap[node.NodeKey()] = node
	}
	return nodeMap
}

// buildInbound creates the inbound configuration. The listener is pinned to
// loopback: the data-plane must never be reachable from the network.
func (rb *RoutingBuilder) buildInbound(cfg *store.DistributionConfig) (*InboundConfig, error) {
	inbound := &InboundConfig{
		Tag:      "in",
		Listen:   "127.0.0.1",
		Port:     cfg.ListenPort,
		Protocol: cfg.Protocol,
		Settings: make(map[string]interface{}),
	}

	// Protocol-specific settings
	switch cfg.Protocol {
	case "vless":
		clients := []map[string]interface{}{
			{
				"id": cfg.UUID,
			},
		}
		inbound.Settings["clients"] = clients
		inbound.Settings["decryption"] = "none"

	case "vmess":
		clients := []map[string]interface{}{
			{
				"id": cfg.UUID,
			},
		}
		inbound.Settings["clients"] = clients

	case "trojan":
		clients := []map[string]interface{}{
			{
				"password": cfg.UUID,
			},
		}
		inbound.Settings["clients"] = clients

	default:
		return nil, fmt.Errorf("unsupported protocol: %s", cfg.Protocol)
	}

	// Stream settings
	streamSettings := &StreamSettings{
		Network: cfg.Network,
	}

	if cfg.TLS {
		streamSettings.Security = "tls"
		streamSettings.TLSSettings = &TLSSettings{
			ServerName: cfg.Domain,
		}
		if cfg.CertPath != "" && cfg.KeyPath != "" {
			streamSettings.TLSSettings.Certificates = []Certificate{
				{
					CertificateFile: cfg.CertPath,
					KeyFile:         cfg.KeyPath,
				},
			}
		}
	}

	inbound.StreamSettings = streamSettings

	return inbound, nil
}

// buildOutbound creates an outbound configuration for a single node
func (rb *RoutingBuilder) buildOutbound(tag string, node *subscription.Node) (*OutboundConfig, error) {
	outbound := &OutboundConfig{
		Tag:      tag,
		Protocol: node.Type,
		Settings: make(map[string]interface{}),
	}

	// Build vnext/servers based on protocol
	switch node.Type {
	case "vless":
		vnext := []map[string]interface{}{
			{
				"address": node.Server,
				"port":    node.Port,
				"users": []map[string]interface{}{
					{
						"id":         node.UUID,
						"encryption": "none",
					},
				},
			},
		}
		outbound.Settings["vnext"] = vnext

	case "vmess":
		vnext := []map[string]interface{}{
			{
				"address": node.Server,
				"port":    node.Port,
				"users": []map[string]interface{}{
					{
						"id":       node.UUID,
						"alterId":  node.AlterID,
						"security": "auto",
					},
				},
			},
		}
		outbound.Settings["vnext"] = vnext

	case "trojan":
		servers := []map[string]interface{}{
			{
				"address":  node.Server,
				"port":     node.Port,
				"password": node.Password,
			},
		}
		outbound.Settings["servers"] = servers

	default:
		return nil, fmt.Errorf("unsupported node type: %s", node.Type)
	}

	// Stream settings
	streamSettings := &StreamSettings{
		Network: node.Network,
	}

	if node.TLS {
		streamSettings.Security = "tls"
		streamSettings.TLSSettings = &TLSSettings{
			ServerName: node.SNI,
		}
	}

	if node.Network == "ws" {
		streamSettings.WSSettings = &WSSettings{
			Path: "/", // Default path, nodes usually don't store this
		}
	} else if node.Network == "grpc" {
		streamSettings.GrpcSettings = &GrpcSettings{
			ServiceName: node.GrpcServiceName,
		}
	}

	outbound.StreamSettings = streamSettings

	return outbound, nil
}

// buildBalancer creates a balancer outbound and individual node outbounds
func (rb *RoutingBuilder) buildBalancer(
	tag string,
	strategy string,
	nodes []*subscription.Node,
) (*OutboundConfig, []*OutboundConfig, error) {
	// Create individual outbounds for each node
	nodeOutbounds := make([]*OutboundConfig, 0, len(nodes))
	selectorTags := make([]string, 0, len(nodes))

	for i, node := range nodes {
		nodeTag := fmt.Sprintf("%s_node_%d", tag, i)
		outbound, err := rb.buildOutbound(nodeTag, node)
		if err != nil {
			return nil, nil, fmt.Errorf("build node outbound: %w", err)
		}
		nodeOutbounds = append(nodeOutbounds, outbound)
		selectorTags = append(selectorTags, nodeTag)
	}

	// Map strategy
	xrayStrategy := "random"
	switch strategy {
	case "random":
		xrayStrategy = "random"
	case "round_robin":
		xrayStrategy = "roundrobin"
	case "leastping":
		xrayStrategy = "leastping"
	}

	// Create balancer outbound
	balancer := &OutboundConfig{
		Tag:      tag,
		Protocol: "balancer",
		Settings: map[string]interface{}{
			"selector": selectorTags,
			"strategy": xrayStrategy,
		},
	}

	return balancer, nodeOutbounds, nil
}

// buildRoutingRule creates a routing rule for a distribution node
func (rb *RoutingBuilder) buildRoutingRule(
	cfg *store.DistributionConfig,
	distNode *store.DistributionNode,
	outboundTag string,
) (*RoutingRule, error) {
	rule := &RoutingRule{
		Type:        "field",
		InboundTag:  []string{"in"},
		OutboundTag: outboundTag,
	}

	// Match based on protocol and network
	switch cfg.Network {
	case "grpc":
		// Match by gRPC service name (use node.DistributionPath for gRPC serviceName)
		if distNode.DistributionPath == "" {
			return nil, fmt.Errorf("distribution node %s has empty distribution_path field for gRPC routing", distNode.Name)
		}
		// Xray gRPC routing uses domain matching
		rule.Domain = []string{distNode.DistributionPath}

	case "ws":
		// Match by WebSocket path
		if distNode.DistributionPath == "" {
			return nil, fmt.Errorf("distribution node %s has empty distribution_path field for WebSocket routing", distNode.Name)
		}
		rule.Path = []string{distNode.DistributionPath}

	case "tcp":
		// TCP doesn't have path-based routing, would need other criteria
		// For now, we'll route all TCP traffic to the first node
		// In a real implementation, this might need SNI or other matching
		return nil, errors.New("TCP protocol does not support path-based routing")

	default:
		return nil, fmt.Errorf("unsupported network for routing: %s", cfg.Network)
	}

	return rule, nil
}

// ToJSON converts XrayConfig to JSON string
func (xc *XrayConfig) ToJSON() (string, error) {
	data, err := json.MarshalIndent(xc, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal xray config: %w", err)
	}
	return string(data), nil
}
