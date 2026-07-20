package xray

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

// Config represents Xray v1.8.0 JSON configuration
type Config struct {
	Log       *LogConfig     `json:"log,omitempty"`
	Inbounds  []*Inbound     `json:"inbounds"`
	Outbounds []*Outbound    `json:"outbounds"`
	Routing   *RoutingConfig `json:"routing"`
	Stats     *StatsConfig   `json:"stats,omitempty"`
	API       *APIConfig     `json:"api,omitempty"`
	Policy    *PolicyConfig  `json:"policy,omitempty"`
}

// LogConfig configures Xray logging
type LogConfig struct {
	Loglevel string `json:"loglevel"`
}

// Inbound defines an inbound proxy
type Inbound struct {
	Tag            string                 `json:"tag"`
	Listen         string                 `json:"listen,omitempty"`
	Port           int                    `json:"port"`
	Protocol       string                 `json:"protocol"`
	Settings       map[string]interface{} `json:"settings"`
	StreamSettings *StreamSettings        `json:"streamSettings,omitempty"`
}

// Outbound defines an outbound proxy
type Outbound struct {
	Tag            string                 `json:"tag"`
	Protocol       string                 `json:"protocol"`
	Settings       map[string]interface{} `json:"settings"`
	StreamSettings *StreamSettings        `json:"streamSettings,omitempty"`
}

// StreamSettings configures network transport
type StreamSettings struct {
	Network      string        `json:"network"`
	Security     string        `json:"security,omitempty"`
	TLSSettings  *TLSSettings  `json:"tlsSettings,omitempty"`
	WSSettings   *WSSettings   `json:"wsSettings,omitempty"`
	GRPCSettings *GRPCSettings `json:"grpcSettings,omitempty"`
	TCPSettings  *TCPSettings  `json:"tcpSettings,omitempty"`
}

// TLSSettings configures TLS
type TLSSettings struct {
	ServerName   string         `json:"serverName,omitempty"`
	Certificates []*Certificate `json:"certificates,omitempty"`
	Insecure     bool           `json:"allowInsecure,omitempty"`
}

// Certificate represents TLS certificate configuration
type Certificate struct {
	CertificateFile string `json:"certificateFile"`
	KeyFile         string `json:"keyFile"`
}

// WSSettings configures WebSocket transport
type WSSettings struct {
	Path    string            `json:"path"`
	Headers map[string]string `json:"headers,omitempty"`
}

// GRPCSettings configures gRPC transport
type GRPCSettings struct {
	ServiceName string `json:"serviceName"`
	MultiMode   bool   `json:"multiMode,omitempty"`
}

// TCPSettings configures TCP transport
type TCPSettings struct {
	Header map[string]interface{} `json:"header,omitempty"`
}

// RoutingConfig defines routing rules
type RoutingConfig struct {
	DomainStrategy string  `json:"domainStrategy,omitempty"`
	Rules          []*Rule `json:"rules"`
}

// Rule defines a routing rule
type Rule struct {
	Type        string   `json:"type"`
	InboundTag  []string `json:"inboundTag,omitempty"`
	OutboundTag string   `json:"outboundTag"`
	Network     string   `json:"network,omitempty"`
	Protocol    []string `json:"protocol,omitempty"`
	// For gRPC routing by serviceName
	Attrs string `json:"attrs,omitempty"`
}

// StatsConfig enables statistics
type StatsConfig struct{}

// APIConfig configures Xray API
type APIConfig struct {
	Tag      string   `json:"tag"`
	Services []string `json:"services"`
}

// PolicyConfig defines connection policies
type PolicyConfig struct {
	Levels map[string]*LevelPolicy `json:"levels,omitempty"`
	System *SystemPolicy           `json:"system,omitempty"`
}

// LevelPolicy defines per-level policy
type LevelPolicy struct {
	StatsUserUplink   bool `json:"statsUserUplink,omitempty"`
	StatsUserDownlink bool `json:"statsUserDownlink,omitempty"`
}

// SystemPolicy defines system-wide policy
type SystemPolicy struct {
	StatsInboundUplink    bool `json:"statsInboundUplink,omitempty"`
	StatsInboundDownlink  bool `json:"statsInboundDownlink,omitempty"`
	StatsOutboundUplink   bool `json:"statsOutboundUplink,omitempty"`
	StatsOutboundDownlink bool `json:"statsOutboundDownlink,omitempty"`
}

// ConfigBuilder builds Xray configuration from store types
type ConfigBuilder struct{}

// NewConfigBuilder creates a new ConfigBuilder
func NewConfigBuilder() *ConfigBuilder {
	return &ConfigBuilder{}
}

// BuildConfig generates Xray configuration from distribution config and paths
func (cb *ConfigBuilder) BuildConfig(
	cfg *store.DistributionConfig,
	paths []*store.DistributionPath,
	nodes []*subscription.Node,
) (*Config, error) {
	if cfg == nil {
		return nil, errors.New("distribution config is required")
	}
	if len(paths) == 0 {
		return nil, errors.New("at least one distribution path is required")
	}

	// Build node map for quick lookup
	nodeMap := make(map[string]*subscription.Node)
	for _, node := range nodes {
		nodeMap[node.NodeKey()] = node
	}

	// Build inbound
	inbound, err := cb.buildInbound(cfg)
	if err != nil {
		return nil, fmt.Errorf("build inbound: %w", err)
	}

	// Build outbounds for each path
	outbounds := []*Outbound{}
	for _, path := range paths {
		if !path.Enabled {
			continue
		}
		outbound, err := cb.buildOutbound(path, nodeMap)
		if err != nil {
			return nil, fmt.Errorf("build outbound for path %s: %w", path.Name, err)
		}
		outbounds = append(outbounds, outbound)
	}

	if len(outbounds) == 0 {
		return nil, errors.New("no enabled paths found")
	}

	// Build routing rules
	routing := cb.buildRouting(cfg, paths)

	// Build stats and API config
	apiInbound := &Inbound{
		Tag:      "api",
		Listen:   loopbackAddr,
		Port:     10085,
		Protocol: "dokodemo-door",
		Settings: map[string]interface{}{
			"address": "127.0.0.1",
		},
	}

	config := &Config{
		Log: &LogConfig{
			Loglevel: "warning",
		},
		Inbounds:  append([]*Inbound{inbound}, apiInbound),
		Outbounds: outbounds,
		Routing:   routing,
		Stats:     &StatsConfig{},
		API: &APIConfig{
			Tag:      "api",
			Services: []string{"StatsService"},
		},
		Policy: &PolicyConfig{
			System: &SystemPolicy{
				StatsInboundUplink:    true,
				StatsInboundDownlink:  true,
				StatsOutboundUplink:   true,
				StatsOutboundDownlink: true,
			},
		},
	}

	return config, nil
}

// loopbackAddr is the only address Xray inbounds may bind: the data-plane
// and the stats API must never be reachable from the network.
const loopbackAddr = "127.0.0.1"

// buildInbound creates the main inbound configuration
func (cb *ConfigBuilder) buildInbound(cfg *store.DistributionConfig) (*Inbound, error) {
	inbound := &Inbound{
		Tag:      "main-in",
		Listen:   loopbackAddr,
		Port:     cfg.ListenPort,
		Protocol: cfg.Protocol,
	}

	// Protocol-specific settings
	switch cfg.Protocol {
	case "vless":
		inbound.Settings = map[string]interface{}{
			"clients": []map[string]interface{}{
				{
					"id": cfg.UUID,
				},
			},
			"decryption": "none",
		}
	case "vmess":
		inbound.Settings = map[string]interface{}{
			"clients": []map[string]interface{}{
				{
					"id": cfg.UUID,
				},
			},
		}
	default:
		return nil, fmt.Errorf("unsupported protocol: %s", cfg.Protocol)
	}

	// Stream settings
	streamSettings := &StreamSettings{
		Network: cfg.Network,
	}

	switch cfg.Network {
	case "grpc":
		streamSettings.GRPCSettings = &GRPCSettings{
			ServiceName: "GunService",
		}
	case "ws":
		streamSettings.WSSettings = &WSSettings{
			Path: "/",
		}
	case "tcp":
		streamSettings.TCPSettings = &TCPSettings{}
	default:
		return nil, fmt.Errorf("unsupported network: %s", cfg.Network)
	}

	// TLS settings
	if cfg.TLS {
		if cfg.CertPath == "" || cfg.KeyPath == "" {
			return nil, errors.New("cert_path and key_path required when TLS enabled")
		}
		streamSettings.Security = "tls"
		streamSettings.TLSSettings = &TLSSettings{
			ServerName: cfg.Domain,
			Certificates: []*Certificate{
				{
					CertificateFile: cfg.CertPath,
					KeyFile:         cfg.KeyPath,
				},
			},
		}
	}

	inbound.StreamSettings = streamSettings
	return inbound, nil
}

// buildOutbound creates outbound configuration for a distribution path
func (cb *ConfigBuilder) buildOutbound(path *store.DistributionPath, nodeMap map[string]*subscription.Node) (*Outbound, error) {
	if len(path.UpstreamNodeKeys) == 0 {
		return nil, fmt.Errorf("path %s has no upstream nodes", path.Name)
	}

	// Use first node for now (load balancing will be handled by Xray or external logic)
	nodeKey := path.UpstreamNodeKeys[0]
	node, ok := nodeMap[nodeKey]
	if !ok {
		return nil, fmt.Errorf("node %s not found", nodeKey)
	}

	outbound := &Outbound{
		Tag:      fmt.Sprintf("out-%s", path.Name),
		Protocol: node.Type,
	}

	// Protocol-specific settings
	switch node.Type {
	case "vless":
		outbound.Settings = map[string]interface{}{
			"vnext": []map[string]interface{}{
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
			},
		}
	case "vmess":
		outbound.Settings = map[string]interface{}{
			"vnext": []map[string]interface{}{
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
			},
		}
	case "trojan":
		outbound.Settings = map[string]interface{}{
			"servers": []map[string]interface{}{
				{
					"address":  node.Server,
					"port":     node.Port,
					"password": node.Password,
				},
			},
		}
	default:
		return nil, fmt.Errorf("unsupported node type: %s", node.Type)
	}

	// Stream settings
	streamSettings := &StreamSettings{
		Network: node.Network,
	}

	switch node.Network {
	case "grpc":
		streamSettings.GRPCSettings = &GRPCSettings{
			ServiceName: node.GrpcServiceName,
		}
	case "ws":
		streamSettings.WSSettings = &WSSettings{
			Path: "/", // Node struct doesn't have ws path, use default
		}
	case "tcp":
		streamSettings.TCPSettings = &TCPSettings{}
	}

	// TLS settings
	if node.TLS {
		streamSettings.Security = "tls"
		streamSettings.TLSSettings = &TLSSettings{
			ServerName: node.SNI,
			Insecure:   node.Insecure,
		}
	}

	outbound.StreamSettings = streamSettings
	return outbound, nil
}

// buildRouting creates routing rules to match paths to outbounds
func (cb *ConfigBuilder) buildRouting(cfg *store.DistributionConfig, paths []*store.DistributionPath) *RoutingConfig {
	rules := []*Rule{}

	// API rule (highest priority)
	rules = append(rules, &Rule{
		Type:        "field",
		InboundTag:  []string{"api"},
		OutboundTag: "api",
	})

	// Path-specific routing rules
	for _, path := range paths {
		if !path.Enabled {
			continue
		}

		rule := &Rule{
			Type:        "field",
			InboundTag:  []string{"main-in"},
			OutboundTag: fmt.Sprintf("out-%s", path.Name),
		}

		// Route by gRPC serviceName or WebSocket path
		if cfg.Network == "grpc" {
			// For gRPC, use attrs to match serviceName
			// Xray syntax: attrs[':path'].startswith('/ServiceName/')
			rule.Attrs = fmt.Sprintf("attrs[':path'].startswith('/%s')", path.Path)
		} else if cfg.Network == "ws" {
			// For WebSocket, path matching is implicit in the ws settings
			// Multiple ws paths require separate inbounds in real deployment
			// For now, we route based on first enabled path
		}

		rules = append(rules, rule)
	}

	return &RoutingConfig{
		DomainStrategy: "AsIs",
		Rules:          rules,
	}
}

// WriteConfig writes configuration to JSON file
func (cb *ConfigBuilder) WriteConfig(cfg *Config, path string) error {
	if cfg == nil {
		return errors.New("config is nil")
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("write config file: %w", err)
	}

	return nil
}

// ValidateConfig validates Xray configuration file
func (cb *ConfigBuilder) ValidateConfig(path string) error {
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("config file not found: %w", err)
	}

	cmd := exec.Command("xray", "-test", "-c", path)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("xray validation failed: %w\nOutput: %s", err, string(output))
	}

	return nil
}
