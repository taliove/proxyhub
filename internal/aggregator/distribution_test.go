package aggregator

import (
	"testing"

	"github.com/taliove/proxyhub/internal/store"
)

func TestGenerateDistributionNodes(t *testing.T) {
	tests := []struct {
		name      string
		cfg       *store.DistributionConfig
		paths     []*store.DistributionPath
		wantCount int
		wantErr   bool
	}{
		{
			name:      "nil config returns empty",
			cfg:       nil,
			paths:     []*store.DistributionPath{},
			wantCount: 0,
		},
		{
			name: "disabled config returns empty",
			cfg: &store.DistributionConfig{
				Enabled: false,
			},
			paths:     []*store.DistributionPath{},
			wantCount: 0,
		},
		{
			name: "empty paths returns empty",
			cfg: &store.DistributionConfig{
				Enabled:    true,
				ListenPort: 10808,
				Domain:     "example.com",
				Protocol:   "vless",
				Network:    "tcp",
				UUID:       "test-uuid",
				TLS:        true,
			},
			paths:     []*store.DistributionPath{},
			wantCount: 0,
		},
		{
			name: "generates nodes for enabled paths",
			cfg: &store.DistributionConfig{
				Enabled:    true,
				ListenPort: 10808,
				Domain:     "example.com",
				Protocol:   "vless",
				Network:    "tcp",
				UUID:       "test-uuid",
				TLS:        true,
			},
			paths: []*store.DistributionPath{
				{
					ID:               1,
					Name:             "香港优选",
					Path:             "/hk-premium",
					UpstreamNodeKeys: []string{"node1:443", "node2:443"},
					LBStrategy:       "random",
					Enabled:          true,
				},
				{
					ID:               2,
					Name:             "日本负载均衡",
					Path:             "/jp-lb",
					UpstreamNodeKeys: []string{"node3:443"},
					LBStrategy:       "round-robin",
					Enabled:          true,
				},
			},
			wantCount: 2,
		},
		{
			name: "skips disabled paths",
			cfg: &store.DistributionConfig{
				Enabled:    true,
				ListenPort: 10808,
				Domain:     "example.com",
				Protocol:   "vless",
				Network:    "tcp",
				UUID:       "test-uuid",
				TLS:        false,
			},
			paths: []*store.DistributionPath{
				{
					ID:               1,
					Name:             "Enabled Path",
					Path:             "/enabled",
					UpstreamNodeKeys: []string{"node1:443"},
					LBStrategy:       "random",
					Enabled:          true,
				},
				{
					ID:               2,
					Name:             "Disabled Path",
					Path:             "/disabled",
					UpstreamNodeKeys: []string{"node2:443"},
					LBStrategy:       "random",
					Enabled:          false,
				},
			},
			wantCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nodes := generateDistributionNodes(tt.cfg, tt.paths)

			if len(nodes) != tt.wantCount {
				t.Errorf("generateDistributionNodes() got %d nodes, want %d", len(nodes), tt.wantCount)
				return
			}

			// 验证生成的节点属性
			if tt.cfg != nil && tt.cfg.Enabled && len(tt.paths) > 0 {
				for i, node := range nodes {
					if !tt.paths[i].Enabled {
						continue
					}

					// 基本字段
					if node.Name != tt.paths[i].Name {
						t.Errorf("node.Name = %v, want %v", node.Name, tt.paths[i].Name)
					}
					if node.Type != tt.cfg.Protocol {
						t.Errorf("node.Type = %v, want %v", node.Type, tt.cfg.Protocol)
					}
					if node.Server != tt.cfg.Domain {
						t.Errorf("node.Server = %v, want %v", node.Server, tt.cfg.Domain)
					}
					if node.Port != tt.cfg.ListenPort {
						t.Errorf("node.Port = %v, want %v", node.Port, tt.cfg.ListenPort)
					}

					// 协议字段
					if node.UUID != tt.cfg.UUID {
						t.Errorf("node.UUID = %v, want %v", node.UUID, tt.cfg.UUID)
					}
					if node.TLS != tt.cfg.TLS {
						t.Errorf("node.TLS = %v, want %v", node.TLS, tt.cfg.TLS)
					}
					if node.Network != tt.cfg.Network {
						t.Errorf("node.Network = %v, want %v", node.Network, tt.cfg.Network)
					}

					// 元数据
					if node.Source != SourceDistribution {
						t.Errorf("node.Source = %v, want %v", node.Source, SourceDistribution)
					}

					// 分发特有字段
					if !node.IsDistribution {
						t.Errorf("node.IsDistribution = false, want true")
					}
					if node.DistributionPath != tt.paths[i].Path {
						t.Errorf("node.DistributionPath = %v, want %v", node.DistributionPath, tt.paths[i].Path)
					}
					if node.LBStrategy != tt.paths[i].LBStrategy {
						t.Errorf("node.LBStrategy = %v, want %v", node.LBStrategy, tt.paths[i].LBStrategy)
					}
					if len(node.UpstreamNodeKeys) != len(tt.paths[i].UpstreamNodeKeys) {
						t.Errorf("node.UpstreamNodeKeys length = %v, want %v",
							len(node.UpstreamNodeKeys), len(tt.paths[i].UpstreamNodeKeys))
					}

					// 初始状态
					if !node.Available {
						t.Errorf("node.Available = false, want true")
					}
					if node.Latency != 0 {
						t.Errorf("node.Latency = %v, want 0", node.Latency)
					}
				}
			}
		})
	}
}

func TestInferDistributionRegion(t *testing.T) {
	tests := []struct {
		name       string
		path       *store.DistributionPath
		wantRegion string
	}{
		{
			name: "recognizes 香港",
			path: &store.DistributionPath{
				Name: "香港优选节点",
			},
			wantRegion: "香港",
		},
		{
			name: "recognizes 日本",
			path: &store.DistributionPath{
				Name: "日本高速通道",
			},
			wantRegion: "日本",
		},
		{
			name: "recognizes 美国",
			path: &store.DistributionPath{
				Name: "美国节点池",
			},
			wantRegion: "美国",
		},
		{
			name: "recognizes 新加坡",
			path: &store.DistributionPath{
				Name: "新加坡LB",
			},
			wantRegion: "新加坡",
		},
		{
			name: "recognizes 台湾",
			path: &store.DistributionPath{
				Name: "台湾专线",
			},
			wantRegion: "台湾",
		},
		{
			name: "recognizes 韩国",
			path: &store.DistributionPath{
				Name: "韩国节点",
			},
			wantRegion: "韩国",
		},
		{
			name: "defaults to DISTRIBUTION for unknown",
			path: &store.DistributionPath{
				Name: "Global Load Balancer",
			},
			wantRegion: "DISTRIBUTION",
		},
		{
			name: "defaults to DISTRIBUTION for empty name",
			path: &store.DistributionPath{
				Name: "",
			},
			wantRegion: "DISTRIBUTION",
		},
		{
			name: "prefers first matched region",
			path: &store.DistributionPath{
				Name: "香港-日本跨境",
			},
			wantRegion: "香港",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := inferDistributionRegion(tt.path)
			if got != tt.wantRegion {
				t.Errorf("inferDistributionRegion() = %v, want %v", got, tt.wantRegion)
			}
		})
	}
}

func TestGenerateDistributionNodes_NodeKeyUniqueness(t *testing.T) {
	cfg := &store.DistributionConfig{
		Enabled:    true,
		ListenPort: 10808,
		Domain:     "example.com",
		Protocol:   "vless",
		Network:    "tcp",
		UUID:       "test-uuid",
		TLS:        true,
	}

	paths := []*store.DistributionPath{
		{
			ID:               1,
			Name:             "Path A",
			Path:             "/path-a",
			UpstreamNodeKeys: []string{"node1:443"},
			LBStrategy:       "random",
			Enabled:          true,
		},
		{
			ID:               2,
			Name:             "Path B",
			Path:             "/path-b",
			UpstreamNodeKeys: []string{"node2:443"},
			LBStrategy:       "random",
			Enabled:          true,
		},
	}

	nodes := generateDistributionNodes(cfg, paths)

	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(nodes))
	}

	// 验证所有节点使用相同的 server:port，但名称不同
	// 这意味着它们的 NodeKey() 应该相同（server:port:sni）
	key1 := nodes[0].NodeKey()
	key2 := nodes[1].NodeKey()

	if key1 != key2 {
		t.Errorf("distribution nodes should have same NodeKey when sharing domain, got %v and %v", key1, key2)
	}

	// 验证它们有不同的名称和路径
	if nodes[0].Name == nodes[1].Name {
		t.Errorf("nodes should have different names")
	}
	if nodes[0].DistributionPath == nodes[1].DistributionPath {
		t.Errorf("nodes should have different distribution paths")
	}
}

func TestGenerateDistributionNodes_ProtocolFields(t *testing.T) {
	t.Run("vless protocol", func(t *testing.T) {
		cfg := &store.DistributionConfig{
			Enabled:    true,
			ListenPort: 443,
			Domain:     "vless.example.com",
			Protocol:   "vless",
			Network:    "ws",
			UUID:       "uuid-123",
			TLS:        true,
		}

		paths := []*store.DistributionPath{
			{
				ID:               1,
				Name:             "Test",
				Path:             "/test",
				UpstreamNodeKeys: []string{"node:443"},
				LBStrategy:       "random",
				Enabled:          true,
			},
		}

		nodes := generateDistributionNodes(cfg, paths)

		if len(nodes) != 1 {
			t.Fatalf("expected 1 node, got %d", len(nodes))
		}

		node := nodes[0]
		if node.Type != "vless" {
			t.Errorf("node.Type = %v, want vless", node.Type)
		}
		if node.UUID != "uuid-123" {
			t.Errorf("node.UUID = %v, want uuid-123", node.UUID)
		}
		if !node.TLS {
			t.Errorf("node.TLS = false, want true")
		}
		if node.Network != "ws" {
			t.Errorf("node.Network = %v, want ws", node.Network)
		}
	})

	t.Run("vmess protocol", func(t *testing.T) {
		cfg := &store.DistributionConfig{
			Enabled:    true,
			ListenPort: 8080,
			Domain:     "vmess.example.com",
			Protocol:   "vmess",
			Network:    "tcp",
			UUID:       "uuid-456",
			TLS:        false,
		}

		paths := []*store.DistributionPath{
			{
				ID:               1,
				Name:             "Test",
				Path:             "/test",
				UpstreamNodeKeys: []string{"node:443"},
				LBStrategy:       "random",
				Enabled:          true,
			},
		}

		nodes := generateDistributionNodes(cfg, paths)

		if len(nodes) != 1 {
			t.Fatalf("expected 1 node, got %d", len(nodes))
		}

		node := nodes[0]
		if node.Type != "vmess" {
			t.Errorf("node.Type = %v, want vmess", node.Type)
		}
		if node.UUID != "uuid-456" {
			t.Errorf("node.UUID = %v, want uuid-456", node.UUID)
		}
		if node.TLS {
			t.Errorf("node.TLS = true, want false")
		}
	})
}
