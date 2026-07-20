package aggregator

import (
	"testing"

	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

// TestNodes_IncludesDistributionNodes 验证 Nodes() 方法正确合并分发节点
func TestNodes_IncludesDistributionNodes(t *testing.T) {
	agg, st := newTestAggregator(t)

	// 设置初始节点池（机场节点）
	agg.mu.Lock()
	agg.nodes = []*subscription.Node{
		{
			Name:   "Airport Node 1",
			Type:   "vmess",
			Server: "airport1.com",
			Port:   443,
			Source: "airport",
			Region: "香港",
		},
		{
			Name:   "Airport Node 2",
			Type:   "vless",
			Server: "airport2.com",
			Port:   443,
			Source: "airport",
			Region: "日本",
		},
	}
	agg.mu.Unlock()

	// 测试：分发未启用时，仅返回机场节点
	t.Run("distribution disabled", func(t *testing.T) {
		nodes := agg.Nodes()
		if len(nodes) != 2 {
			t.Errorf("expected 2 nodes when distribution disabled, got %d", len(nodes))
		}
		for _, n := range nodes {
			if n.IsDistribution {
				t.Errorf("unexpected distribution node when distribution disabled")
			}
		}
	})

	// 启用分发并创建配置
	err := st.SaveDistributionConfig(&store.DistributionConfig{
		Enabled:    true,
		ListenPort: 10808,
		Domain:     "dist.example.com",
		Protocol:   "vless",
		Network:    "tcp",
		UUID:       "test-uuid-123",
		TLS:        true,
	})
	if err != nil {
		t.Fatalf("failed to save distribution config: %v", err)
	}

	// 创建分发路径
	_, err = st.CreateDistributionPath(&store.DistributionPath{
		Name:             "香港分发",
		Path:             "/hk",
		UpstreamNodeKeys: []string{"node1:443", "node2:443"},
		LBStrategy:       "random",
		Enabled:          true,
	})
	if err != nil {
		t.Fatalf("failed to create distribution path: %v", err)
	}

	_, err = st.CreateDistributionPath(&store.DistributionPath{
		Name:             "日本分发",
		Path:             "/jp",
		UpstreamNodeKeys: []string{"node3:443"},
		LBStrategy:       "round-robin",
		Enabled:          true,
	})
	if err != nil {
		t.Fatalf("failed to create distribution path: %v", err)
	}

	// 测试：分发启用时，返回机场节点 + 分发节点
	t.Run("distribution enabled", func(t *testing.T) {
		nodes := agg.Nodes()

		// 应该有 2 个机场节点 + 2 个分发节点 = 4 个
		if len(nodes) != 4 {
			t.Fatalf("expected 4 nodes (2 airport + 2 distribution), got %d", len(nodes))
		}

		// 统计节点类型
		airportCount := 0
		distributionCount := 0
		for _, n := range nodes {
			if n.IsDistribution {
				distributionCount++

				// 验证分发节点属性
				if n.Source != SourceDistribution {
					t.Errorf("distribution node should have Source=%v, got %v", SourceDistribution, n.Source)
				}
				if n.Server != "dist.example.com" {
					t.Errorf("distribution node should use config domain, got %v", n.Server)
				}
				if n.Port != 10808 {
					t.Errorf("distribution node should use config port, got %v", n.Port)
				}
				if n.Type != "vless" {
					t.Errorf("distribution node should use config protocol, got %v", n.Type)
				}
				if !n.TLS {
					t.Errorf("distribution node should have TLS enabled")
				}
				if n.UUID != "test-uuid-123" {
					t.Errorf("distribution node should use config UUID, got %v", n.UUID)
				}
				if len(n.UpstreamNodeKeys) == 0 {
					t.Errorf("distribution node should have upstream node keys")
				}
				if n.DistributionPath == "" {
					t.Errorf("distribution node should have distribution path")
				}
			} else {
				airportCount++
			}
		}

		if airportCount != 2 {
			t.Errorf("expected 2 airport nodes, got %d", airportCount)
		}
		if distributionCount != 2 {
			t.Errorf("expected 2 distribution nodes, got %d", distributionCount)
		}
	})

	// 测试：禁用一个分发路径
	t.Run("disabled distribution path excluded", func(t *testing.T) {
		paths, err := st.ListDistributionPaths()
		if err != nil {
			t.Fatalf("failed to list paths: %v", err)
		}

		// 禁用第一个路径
		paths[0].Enabled = false
		if err := st.UpdateDistributionPath(paths[0]); err != nil {
			t.Fatalf("failed to update path: %v", err)
		}

		nodes := agg.Nodes()

		// 应该有 2 个机场节点 + 1 个分发节点 = 3 个
		if len(nodes) != 3 {
			t.Fatalf("expected 3 nodes (2 airport + 1 distribution), got %d", len(nodes))
		}

		distributionCount := 0
		for _, n := range nodes {
			if n.IsDistribution {
				distributionCount++
			}
		}
		if distributionCount != 1 {
			t.Errorf("expected 1 enabled distribution node, got %d", distributionCount)
		}
	})
}

// TestNodes_DistributionRegionInference 验证地区识别逻辑
func TestNodes_DistributionRegionInference(t *testing.T) {
	agg, st := newTestAggregator(t)

	// 启用分发
	err := st.SaveDistributionConfig(&store.DistributionConfig{
		Enabled:    true,
		ListenPort: 10808,
		Domain:     "dist.example.com",
		Protocol:   "vless",
		Network:    "tcp",
		UUID:       "test-uuid",
		TLS:        true,
	})
	if err != nil {
		t.Fatalf("failed to save distribution config: %v", err)
	}

	// 创建不同地区的分发路径
	testCases := []struct {
		name           string
		pathName       string
		expectedRegion string
	}{
		{"香港路径", "香港高速通道", "香港"},
		{"日本路径", "日本优选节点", "日本"},
		{"美国路径", "美国负载均衡", "美国"},
		{"未知地区", "Global LB", "DISTRIBUTION"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			path, err := st.CreateDistributionPath(&store.DistributionPath{
				Name:             tc.pathName,
				Path:             "/" + tc.name,
				UpstreamNodeKeys: []string{"node:443"},
				LBStrategy:       "random",
				Enabled:          true,
			})
			if err != nil {
				t.Fatalf("failed to create path: %v", err)
			}

			nodes := agg.Nodes()

			// 查找对应的分发节点
			var found *subscription.Node
			for _, n := range nodes {
				if n.IsDistribution && n.DistributionPath == path.Path {
					found = n
					break
				}
			}

			if found == nil {
				t.Fatalf("distribution node not found for path %v", tc.pathName)
			}

			if found.Region != tc.expectedRegion {
				t.Errorf("expected region %v, got %v", tc.expectedRegion, found.Region)
			}

			// 清理：删除路径
			if err := st.DeleteDistributionPath(path.ID); err != nil {
				t.Fatalf("failed to delete path: %v", err)
			}
		})
	}
}
