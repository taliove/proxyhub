package distribution

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

func TestManager_Lifecycle(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test.db"
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer st.Close()

	// Save disabled config
	err = st.SaveDistributionConfig(&store.DistributionConfig{
		Enabled:    false,
		ListenPort: 10808,
		Protocol:   "vless",
		Network:    "grpc",
		UUID:       "test-uuid",
	})
	if err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	configPath := tmpDir + "/xray.json"
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	mgr := NewManager(st, "xray", configPath, logger)

	// Start with disabled config should not start xray
	ctx := context.Background()
	err = mgr.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if mgr.IsRunning() {
		t.Error("expected xray not to be running when disabled")
	}

	// Stop should be safe even if not running
	err = mgr.Stop()
	if err != nil {
		t.Errorf("Stop failed: %v", err)
	}
}

func TestManager_Reload(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test.db"
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer st.Close()

	// Save disabled config initially
	err = st.SaveDistributionConfig(&store.DistributionConfig{
		Enabled:    false,
		ListenPort: 10808,
		Protocol:   "vless",
		Network:    "grpc",
		UUID:       "test-uuid",
	})
	if err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	// Create a distribution node
	err = st.CreateDistributionNode(&store.DistributionNode{
		Name:             "Test Node",
		Region:           "US",
		DistributionPath: "/test",
		UpstreamNodeKeys: []string{"node.example.com:443"},
		Enabled:          true,
	})
	if err != nil {
		t.Fatalf("failed to create distribution node: %v", err)
	}

	configPath := tmpDir + "/xray.json"
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	mgr := NewManager(st, "xray", configPath, logger)

	ctx := context.Background()
	nodes := []*subscription.Node{
		{
			Name:    "Test Node",
			Type:    "vless",
			Server:  "node.example.com",
			Port:    443,
			UUID:    "node-uuid",
			Network: "grpc",
		},
	}

	// Reload with disabled config should succeed but not generate config
	err = mgr.Reload(ctx, nodes)
	if err != nil {
		t.Errorf("Reload failed: %v", err)
	}

	// Verify xray is not running
	if mgr.IsRunning() {
		t.Error("expected xray not to be running with disabled config")
	}

	// Now enable the config and test Reload generates the config
	err = st.SaveDistributionConfig(&store.DistributionConfig{
		Enabled:    true,
		ListenPort: 10808,
		Protocol:   "vless",
		Network:    "grpc",
		UUID:       "test-uuid",
	})
	if err != nil {
		t.Fatalf("failed to save enabled config: %v", err)
	}

	// This time Reload should generate config
	// Note: It will fail to start xray (not installed), but config should be generated
	_ = mgr.Reload(ctx, nodes) // Ignore error since xray won't start in test env

	// Verify config file was created
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Error("expected xray config file to be created")
	}
}

func TestManager_ReloadDisabled(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test.db"
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer st.Close()

	// Save disabled config
	err = st.SaveDistributionConfig(&store.DistributionConfig{
		Enabled:    false,
		ListenPort: 10808,
		Protocol:   "vless",
		Network:    "grpc",
		UUID:       "test-uuid",
	})
	if err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	configPath := tmpDir + "/xray.json"
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	mgr := NewManager(st, "xray", configPath, logger)

	ctx := context.Background()

	// Reload with disabled config should succeed without starting xray
	err = mgr.Reload(ctx, nil)
	if err != nil {
		t.Errorf("Reload failed: %v", err)
	}

	if mgr.IsRunning() {
		t.Error("expected xray not to be running after reload with disabled config")
	}
}

func TestLoggerAdapter(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	adapter := &loggerAdapter{logger: logger}

	// Should not panic
	adapter.Info("test info", "key", "value")
	adapter.Error("test error", "key", "value")
}

func TestWriteXrayConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := tmpDir + "/xray.json"

	config := &XrayConfig{
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
				Protocol: "freedom",
			},
		},
	}

	err := writeXrayConfig(configPath, config)
	if err != nil {
		t.Fatalf("writeXrayConfig failed: %v", err)
	}

	// Verify file was created
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Error("expected config file to be created")
	}

	// Read and verify content
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read config file: %v", err)
	}

	content := string(data)
	if !contains(content, `"log"`) || !contains(content, `"inbounds"`) {
		t.Error("config file content is invalid")
	}
}

func TestWriteXrayConfig_NilConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := tmpDir + "/xray.json"

	err := writeXrayConfig(configPath, nil)
	if err == nil {
		t.Error("expected error for nil config")
	}
	if !contains(err.Error(), "nil") {
		t.Errorf("expected error message to contain 'nil', got: %v", err)
	}
}

func TestBuildInbound(t *testing.T) {
	builder := &RoutingBuilder{}

	tests := []struct {
		name    string
		cfg     *store.DistributionConfig
		wantErr bool
	}{
		{
			name: "vless protocol",
			cfg: &store.DistributionConfig{
				Protocol:   "vless",
				ListenPort: 10808,
				Network:    "grpc",
				UUID:       "test-uuid",
				TLS:        true,
				Domain:     "example.com",
				CertPath:   "/path/to/cert",
				KeyPath:    "/path/to/key",
			},
			wantErr: false,
		},
		{
			name: "vmess protocol",
			cfg: &store.DistributionConfig{
				Protocol:   "vmess",
				ListenPort: 10808,
				Network:    "ws",
				UUID:       "test-uuid",
			},
			wantErr: false,
		},
		{
			name: "trojan protocol",
			cfg: &store.DistributionConfig{
				Protocol:   "trojan",
				ListenPort: 10808,
				Network:    "tcp",
				UUID:       "test-password",
			},
			wantErr: false,
		},
		{
			name: "unsupported protocol",
			cfg: &store.DistributionConfig{
				Protocol:   "shadowsocks",
				ListenPort: 10808,
				Network:    "tcp",
				UUID:       "test",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inbound, err := builder.buildInbound(tt.cfg)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if inbound == nil {
				t.Error("expected inbound, got nil")
			}
			if inbound.Port != tt.cfg.ListenPort {
				t.Errorf("expected port %d, got %d", tt.cfg.ListenPort, inbound.Port)
			}
			if inbound.Listen != "127.0.0.1" {
				t.Errorf("expected listen 127.0.0.1 (inbound must never bind a public interface), got %q", inbound.Listen)
			}
		})
	}
}

func TestBuildOutbound(t *testing.T) {
	builder := &RoutingBuilder{}

	tests := []struct {
		name    string
		node    *subscription.Node
		wantErr bool
	}{
		{
			name: "vless node",
			node: &subscription.Node{
				Type:    "vless",
				Server:  "node.example.com",
				Port:    443,
				UUID:    "node-uuid",
				Network: "grpc",
				TLS:     true,
				SNI:     "node.example.com",
			},
			wantErr: false,
		},
		{
			name: "vmess node",
			node: &subscription.Node{
				Type:    "vmess",
				Server:  "node.example.com",
				Port:    443,
				UUID:    "node-uuid",
				AlterID: 0,
				Network: "ws",
			},
			wantErr: false,
		},
		{
			name: "trojan node",
			node: &subscription.Node{
				Type:     "trojan",
				Server:   "node.example.com",
				Port:     443,
				Password: "password",
				Network:  "tcp",
				TLS:      true,
			},
			wantErr: false,
		},
		{
			name: "unsupported node type",
			node: &subscription.Node{
				Type:   "shadowsocks",
				Server: "node.example.com",
				Port:   443,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outbound, err := builder.buildOutbound("test-tag", tt.node)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if outbound == nil {
				t.Error("expected outbound, got nil")
			}
			if outbound.Tag != "test-tag" {
				t.Errorf("expected tag 'test-tag', got %s", outbound.Tag)
			}
		})
	}
}

func TestBuildBalancer(t *testing.T) {
	builder := &RoutingBuilder{}

	nodes := []*subscription.Node{
		{
			Type:    "vless",
			Server:  "node1.example.com",
			Port:    443,
			UUID:    "node1-uuid",
			Network: "grpc",
		},
		{
			Type:    "vless",
			Server:  "node2.example.com",
			Port:    443,
			UUID:    "node2-uuid",
			Network: "grpc",
		},
	}

	tests := []struct {
		name     string
		strategy string
		wantErr  bool
	}{
		{
			name:     "random strategy",
			strategy: "random",
			wantErr:  false,
		},
		{
			name:     "round_robin strategy",
			strategy: "round_robin",
			wantErr:  false,
		},
		{
			name:     "leastping strategy",
			strategy: "leastping",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			balancer, nodeOutbounds, err := builder.buildBalancer("test-tag", tt.strategy, nodes)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if balancer == nil {
				t.Error("expected balancer, got nil")
			}
			if balancer.Protocol != "balancer" {
				t.Errorf("expected balancer protocol, got %s", balancer.Protocol)
			}
			if len(nodeOutbounds) != 2 {
				t.Errorf("expected 2 node outbounds, got %d", len(nodeOutbounds))
			}
		})
	}
}

func TestBuildRoutingRule(t *testing.T) {
	builder := &RoutingBuilder{}

	tests := []struct {
		name     string
		cfg      *store.DistributionConfig
		distNode *store.DistributionNode
		wantErr  bool
	}{
		{
			name: "grpc routing",
			cfg: &store.DistributionConfig{
				Network: "grpc",
			},
			distNode: &store.DistributionNode{
				Name:             "Test Node",
				DistributionPath: "/test",
			},
			wantErr: false,
		},
		{
			name: "websocket routing",
			cfg: &store.DistributionConfig{
				Network: "ws",
			},
			distNode: &store.DistributionNode{
				Name:             "Test Node",
				DistributionPath: "/ws-path",
			},
			wantErr: false,
		},
		{
			name: "tcp routing not supported",
			cfg: &store.DistributionConfig{
				Network: "tcp",
			},
			distNode: &store.DistributionNode{
				Name:             "Test Node",
				DistributionPath: "/test",
			},
			wantErr: true,
		},
		{
			name: "grpc with empty distribution_path",
			cfg: &store.DistributionConfig{
				Network: "grpc",
			},
			distNode: &store.DistributionNode{
				Name:             "Empty Path Node",
				DistributionPath: "",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule, err := builder.buildRoutingRule(tt.cfg, tt.distNode, "test-outbound")
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if rule == nil {
				t.Error("expected rule, got nil")
			}
			if rule.OutboundTag != "test-outbound" {
				t.Errorf("expected outbound tag 'test-outbound', got %s", rule.OutboundTag)
			}
		})
	}
}
