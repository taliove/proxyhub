package distribution

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

// Mock XrayStatsClient for testing
type mockXrayStatsClient struct {
	stats map[string]*PathStats
	err   error
}

func (m *mockXrayStatsClient) GetPathStats(pathTag string) (*PathStats, error) {
	if m.err != nil {
		return nil, m.err
	}
	if stats, ok := m.stats[pathTag]; ok {
		return stats, nil
	}
	return &PathStats{}, nil
}

func TestRoutingBuilder_BuildXrayConfig(t *testing.T) {
	tests := []struct {
		name      string
		cfg       *store.DistributionConfig
		distNodes []*store.DistributionNode
		nodePool  []*subscription.Node
		wantErr   bool
		errMsg    string
		checkFunc func(*testing.T, *XrayConfig)
	}{
		{
			name: "single distribution node with single upstream",
			cfg: &store.DistributionConfig{
				Enabled:    true,
				ListenPort: 10808,
				Protocol:   "vless",
				Network:    "grpc",
				UUID:       "test-uuid-123",
				TLS:        true,
				Domain:     "example.com",
			},
			distNodes: []*store.DistributionNode{
				{
					ID:               1,
					Name:             "HK Node",
					Region:           "HK",
					DistributionPath: "/hk",
					UpstreamNodeKeys: []string{"hk.example.com:443:hk.example.com"}, // Include SNI in key
					LBStrategy:       "random",
					Enabled:          true,
				},
			},
			nodePool: []*subscription.Node{
				{
					Name:            "HK Node",
					Type:            "vless",
					Server:          "hk.example.com",
					Port:            443,
					UUID:            "node-uuid-123",
					Network:         "grpc",
					GrpcServiceName: "/hk",
					TLS:             true,
					SNI:             "hk.example.com",
				},
			},
			wantErr: false,
			checkFunc: func(t *testing.T, cfg *XrayConfig) {
				if len(cfg.Inbounds) != 1 {
					t.Errorf("expected 1 inbound, got %d", len(cfg.Inbounds))
				}
				if cfg.Inbounds[0].Port != 10808 {
					t.Errorf("expected port 10808, got %d", cfg.Inbounds[0].Port)
				}
				if cfg.Inbounds[0].Protocol != "vless" {
					t.Errorf("expected protocol vless, got %s", cfg.Inbounds[0].Protocol)
				}
				// Should have 2 outbounds: node_1 + direct
				if len(cfg.Outbounds) != 2 {
					t.Errorf("expected 2 outbounds, got %d", len(cfg.Outbounds))
				}
				if cfg.Routing == nil {
					t.Error("expected routing config, got nil")
				}
				if len(cfg.Routing.Rules) != 1 {
					t.Errorf("expected 1 routing rule, got %d", len(cfg.Routing.Rules))
				}
			},
		},
		{
			name: "multiple upstreams with load balancing",
			cfg: &store.DistributionConfig{
				Enabled:    true,
				ListenPort: 10808,
				Protocol:   "vless",
				Network:    "grpc",
				UUID:       "test-uuid-123",
			},
			distNodes: []*store.DistributionNode{
				{
					ID:               1,
					Name:             "Multi Node",
					Region:           "US",
					DistributionPath: "/multi",
					UpstreamNodeKeys: []string{
						"node1.example.com:443",
						"node2.example.com:443",
					},
					LBStrategy: "round_robin",
					Enabled:    true,
				},
			},
			nodePool: []*subscription.Node{
				{
					Name:            "Node 1",
					Type:            "vless",
					Server:          "node1.example.com",
					Port:            443,
					UUID:            "node1-uuid",
					Network:         "grpc",
					GrpcServiceName: "/multi",
				},
				{
					Name:            "Node 2",
					Type:            "vless",
					Server:          "node2.example.com",
					Port:            443,
					UUID:            "node2-uuid",
					Network:         "grpc",
					GrpcServiceName: "/multi",
				},
			},
			wantErr: false,
			checkFunc: func(t *testing.T, cfg *XrayConfig) {
				// Should have: balancer + 2 node outbounds + direct = 4 outbounds
				if len(cfg.Outbounds) != 4 {
					t.Errorf("expected 4 outbounds, got %d", len(cfg.Outbounds))
				}
				// First outbound should be balancer
				if cfg.Outbounds[0].Protocol != "balancer" {
					t.Errorf("expected first outbound to be balancer, got %s", cfg.Outbounds[0].Protocol)
				}
			},
		},
		{
			name: "upstream node not found",
			cfg: &store.DistributionConfig{
				Enabled:    true,
				ListenPort: 10808,
				Protocol:   "vless",
				Network:    "grpc",
				UUID:       "test-uuid-123",
			},
			distNodes: []*store.DistributionNode{
				{
					ID:               1,
					Name:             "Missing Node",
					Region:           "US",
					DistributionPath: "/missing",
					UpstreamNodeKeys: []string{"missing.node:443"},
					Enabled:          true,
				},
			},
			nodePool: []*subscription.Node{},
			wantErr:  true,
			errMsg:   "upstream node not found",
		},
		{
			name: "empty distribution_path for grpc routing",
			cfg: &store.DistributionConfig{
				Enabled:    true,
				ListenPort: 10808,
				Protocol:   "vless",
				Network:    "grpc",
				UUID:       "test-uuid-123",
			},
			distNodes: []*store.DistributionNode{
				{
					ID:               1,
					Name:             "Empty Path Node",
					Region:           "US",
					DistributionPath: "", // Empty path
					UpstreamNodeKeys: []string{"node.example.com:443"},
					Enabled:          true,
				},
			},
			nodePool: []*subscription.Node{
				{
					Name:    "Node",
					Type:    "vless",
					Server:  "node.example.com",
					Port:    443,
					UUID:    "node-uuid",
					Network: "grpc",
				},
			},
			wantErr: true,
			errMsg:  "empty distribution_path field",
		},
		{
			name: "websocket network",
			cfg: &store.DistributionConfig{
				Enabled:    true,
				ListenPort: 10808,
				Protocol:   "vmess",
				Network:    "ws",
				UUID:       "test-uuid-123",
			},
			distNodes: []*store.DistributionNode{
				{
					ID:               1,
					Name:             "WS Node",
					Region:           "US",
					DistributionPath: "/ws-path",
					UpstreamNodeKeys: []string{"ws.example.com:443"},
					Enabled:          true,
				},
			},
			nodePool: []*subscription.Node{
				{
					Name:    "WS Node",
					Type:    "vmess",
					Server:  "ws.example.com",
					Port:    443,
					UUID:    "node-uuid",
					Network: "ws",
				},
			},
			wantErr: false,
			checkFunc: func(t *testing.T, cfg *XrayConfig) {
				if cfg.Routing == nil || len(cfg.Routing.Rules) == 0 {
					t.Fatal("expected routing rules")
				}
				rule := cfg.Routing.Rules[0]
				if len(rule.Path) != 1 || rule.Path[0] != "/ws-path" {
					t.Errorf("expected path /ws-path, got %v", rule.Path)
				}
			},
		},
		{
			name: "disabled node should be skipped",
			cfg: &store.DistributionConfig{
				Enabled:    true,
				ListenPort: 10808,
				Protocol:   "vless",
				Network:    "grpc",
				UUID:       "test-uuid-123",
			},
			distNodes: []*store.DistributionNode{
				{
					ID:               1,
					Name:             "Disabled Node",
					Region:           "US",
					DistributionPath: "/disabled",
					UpstreamNodeKeys: []string{"node.example.com:443"},
					Enabled:          false, // Disabled
				},
			},
			nodePool: []*subscription.Node{
				{
					Name:    "Node",
					Type:    "vless",
					Server:  "node.example.com",
					Port:    443,
					UUID:    "node-uuid",
					Network: "grpc",
				},
			},
			wantErr: false,
			checkFunc: func(t *testing.T, cfg *XrayConfig) {
				// Should only have direct outbound
				if len(cfg.Outbounds) != 1 {
					t.Errorf("expected 1 outbound (direct), got %d", len(cfg.Outbounds))
				}
				if cfg.Outbounds[0].Tag != "direct" {
					t.Errorf("expected direct outbound, got %s", cfg.Outbounds[0].Tag)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := &RoutingBuilder{}
			got, err := builder.BuildXrayConfig(tt.cfg, tt.distNodes, tt.nodePool)

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
					return
				}
				if tt.errMsg != "" && !contains(err.Error(), tt.errMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errMsg, err.Error())
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if got == nil {
				t.Error("expected config, got nil")
				return
			}

			if tt.checkFunc != nil {
				tt.checkFunc(t, got)
			}
		})
	}
}

func TestXrayConfig_ToJSON(t *testing.T) {
	cfg := &XrayConfig{
		Log: &LogConfig{
			Loglevel: "warning",
		},
		Inbounds: []*InboundConfig{
			{
				Tag:      "in",
				Port:     10808,
				Protocol: "vless",
			},
		},
		Outbounds: []*OutboundConfig{
			{
				Tag:      "out",
				Protocol: "vless",
			},
		},
	}

	json, err := cfg.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON failed: %v", err)
	}

	if json == "" {
		t.Error("expected non-empty JSON")
	}

	// Basic validation
	if !contains(json, `"log"`) {
		t.Error("expected log section in JSON")
	}
	if !contains(json, `"inbounds"`) {
		t.Error("expected inbounds section in JSON")
	}
	if !contains(json, `"outbounds"`) {
		t.Error("expected outbounds section in JSON")
	}
}

func TestStatsCollector_CollectOnce(t *testing.T) {
	// Create temporary database
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test.db"
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer st.Close()

	// Create test paths
	path1, err := st.CreateDistributionPath(&store.DistributionPath{
		Name:             "Test Path 1",
		Path:             "/test1",
		UpstreamNodeKeys: []string{"node1:443"},
		Enabled:          true,
	})
	if err != nil {
		t.Fatalf("failed to create path: %v", err)
	}

	path2, err := st.CreateDistributionPath(&store.DistributionPath{
		Name:             "Test Path 2",
		Path:             "/test2",
		UpstreamNodeKeys: []string{"node2:443"},
		Enabled:          false, // Disabled
	})
	if err != nil {
		t.Fatalf("failed to create path: %v", err)
	}

	// Mock Xray stats client
	mockClient := &mockXrayStatsClient{
		stats: map[string]*PathStats{
			"path_1": {
				Upload:      1024,
				Download:    2048,
				Connections: 5,
			},
			"path_2": {
				Upload:      512,
				Download:    1024,
				Connections: 2,
			},
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	collector := NewStatsCollector(mockClient, st, time.Minute, logger)

	// Collect stats
	err = collector.CollectOnce()
	if err != nil {
		t.Fatalf("CollectOnce failed: %v", err)
	}

	// Verify stats were recorded for enabled path only
	stats1, err := st.GetDistributionStats(path1.ID, 10)
	if err != nil {
		t.Fatalf("failed to get stats for path 1: %v", err)
	}
	if len(stats1) != 1 {
		t.Errorf("expected 1 stat record for path 1, got %d", len(stats1))
	}
	if stats1[0].Upload != 1024 || stats1[0].Download != 2048 {
		t.Errorf("unexpected stats: upload=%d, download=%d", stats1[0].Upload, stats1[0].Download)
	}

	// Verify disabled path was not collected
	stats2, err := st.GetDistributionStats(path2.ID, 10)
	if err != nil {
		t.Fatalf("failed to get stats for path 2: %v", err)
	}
	if len(stats2) != 0 {
		t.Errorf("expected 0 stat records for disabled path, got %d", len(stats2))
	}

	// Verify totals were updated
	updatedPath1, err := st.GetDistributionPath(path1.ID)
	if err != nil {
		t.Fatalf("failed to get updated path: %v", err)
	}
	if updatedPath1.TotalUpload != 1024 || updatedPath1.TotalDownload != 2048 {
		t.Errorf("unexpected totals: upload=%d, download=%d",
			updatedPath1.TotalUpload, updatedPath1.TotalDownload)
	}
}

func TestStatsCollector_ErrorHandling(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test.db"
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer st.Close()

	// Create test path
	_, err = st.CreateDistributionPath(&store.DistributionPath{
		Name:             "Test Path",
		Path:             "/test",
		UpstreamNodeKeys: []string{"node:443"},
		Enabled:          true,
	})
	if err != nil {
		t.Fatalf("failed to create path: %v", err)
	}

	// Mock client that returns errors
	mockClient := &mockXrayStatsClient{
		err: errors.New("xray connection failed"),
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	collector := NewStatsCollector(mockClient, st, time.Minute, logger)

	// Should not fail completely, just log warnings
	err = collector.CollectOnce()
	if err != nil {
		t.Errorf("CollectOnce should handle errors gracefully, got: %v", err)
	}
}

func TestStatsCollector_Lifecycle(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test.db"
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer st.Close()

	mockClient := &mockXrayStatsClient{
		stats: make(map[string]*PathStats),
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	collector := NewStatsCollector(mockClient, st, 100*time.Millisecond, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// Start collector
	collector.Start(ctx)

	// Wait for context to be cancelled
	<-ctx.Done()

	// Stop should be safe to call
	collector.Stop()
}

func TestBuildNodeMap(t *testing.T) {
	nodes := []*subscription.Node{
		{
			Server: "node1.example.com",
			Port:   443,
		},
		{
			Server: "node2.example.com",
			Port:   8443,
			SNI:    "sni.example.com",
		},
	}

	nodeMap := buildNodeMap(nodes)

	if len(nodeMap) != 2 {
		t.Errorf("expected 2 nodes in map, got %d", len(nodeMap))
	}

	key1 := "node1.example.com:443"
	if _, ok := nodeMap[key1]; !ok {
		t.Errorf("expected node with key %s", key1)
	}

	key2 := "node2.example.com:8443:sni.example.com"
	if _, ok := nodeMap[key2]; !ok {
		t.Errorf("expected node with key %s", key2)
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
