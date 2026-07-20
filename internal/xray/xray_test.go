package xray

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

// mockLogger implements Logger interface for testing
type mockLogger struct {
	infoLogs  []string
	errorLogs []string
}

func (m *mockLogger) Info(msg string, args ...interface{}) {
	m.infoLogs = append(m.infoLogs, msg)
}

func (m *mockLogger) Error(msg string, args ...interface{}) {
	m.errorLogs = append(m.errorLogs, msg)
}

func TestConfigBuilder_BuildConfig(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *store.DistributionConfig
		paths   []*store.DistributionPath
		nodes   []*subscription.Node
		wantErr bool
	}{
		{
			name: "valid vless+grpc config",
			cfg: &store.DistributionConfig{
				ID:         1,
				Enabled:    true,
				ListenPort: 10808,
				Domain:     "example.com",
				Protocol:   "vless",
				Network:    "grpc",
				UUID:       "12345678-1234-1234-1234-123456789012",
				TLS:        true,
				CertPath:   "/path/to/cert.pem",
				KeyPath:    "/path/to/key.pem",
			},
			paths: []*store.DistributionPath{
				{
					ID:               1,
					Name:             "hk-path",
					Path:             "hk",
					UpstreamNodeKeys: []string{"1.2.3.4:443:node.example.com"},
					LBStrategy:       "random",
					Enabled:          true,
				},
			},
			nodes: []*subscription.Node{
				{
					Name:            "HK Node",
					Type:            "vless",
					Server:          "1.2.3.4",
					Port:            443,
					UUID:            "87654321-4321-4321-4321-210987654321",
					Network:         "grpc",
					TLS:             true,
					SNI:             "node.example.com",
					GrpcServiceName: "GunService",
					Region:          "香港",
					Source:          "test",
					Available:       true,
				},
			},
			wantErr: false,
		},
		{
			name: "valid vmess+ws config",
			cfg: &store.DistributionConfig{
				ID:         1,
				Enabled:    true,
				ListenPort: 10809,
				Protocol:   "vmess",
				Network:    "ws",
				UUID:       "12345678-1234-1234-1234-123456789012",
				TLS:        false,
			},
			paths: []*store.DistributionPath{
				{
					ID:               2,
					Name:             "jp-path",
					Path:             "jp",
					UpstreamNodeKeys: []string{"5.6.7.8:80"},
					LBStrategy:       "random",
					Enabled:          true,
				},
			},
			nodes: []*subscription.Node{
				{
					Name:      "JP Node",
					Type:      "vmess",
					Server:    "5.6.7.8",
					Port:      80,
					UUID:      "11111111-2222-3333-4444-555555555555",
					Network:   "ws",
					TLS:       false,
					Region:    "日本",
					Source:    "test",
					Available: true,
				},
			},
			wantErr: false,
		},
		{
			name:    "nil config error",
			cfg:     nil,
			paths:   []*store.DistributionPath{},
			nodes:   []*subscription.Node{},
			wantErr: true,
		},
		{
			name: "no paths error",
			cfg: &store.DistributionConfig{
				Protocol: "vless",
				Network:  "grpc",
			},
			paths:   []*store.DistributionPath{},
			nodes:   []*subscription.Node{},
			wantErr: true,
		},
		{
			name: "disabled path ignored",
			cfg: &store.DistributionConfig{
				ID:         1,
				ListenPort: 10808,
				Protocol:   "vless",
				Network:    "grpc",
				UUID:       "12345678-1234-1234-1234-123456789012",
				TLS:        false,
			},
			paths: []*store.DistributionPath{
				{
					ID:               1,
					Name:             "disabled-path",
					Path:             "test",
					UpstreamNodeKeys: []string{"1.2.3.4:443"},
					Enabled:          false,
				},
			},
			nodes: []*subscription.Node{
				{
					Type:   "vless",
					Server: "1.2.3.4",
					Port:   443,
					UUID:   "test-uuid",
				},
			},
			wantErr: true, // No enabled paths
		},
	}

	builder := NewConfigBuilder()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, err := builder.BuildConfig(tt.cfg, tt.paths, tt.nodes)
			if (err != nil) != tt.wantErr {
				t.Errorf("BuildConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if config == nil {
					t.Error("BuildConfig() returned nil config")
					return
				}

				// Verify inbound
				if len(config.Inbounds) < 1 {
					t.Error("Config should have at least one inbound")
				}

				mainInbound := config.Inbounds[0]
				if mainInbound.Protocol != tt.cfg.Protocol {
					t.Errorf("Inbound protocol = %v, want %v", mainInbound.Protocol, tt.cfg.Protocol)
				}
				if mainInbound.Port != tt.cfg.ListenPort {
					t.Errorf("Inbound port = %v, want %v", mainInbound.Port, tt.cfg.ListenPort)
				}

				// Verify outbounds exist
				if len(config.Outbounds) == 0 {
					t.Error("Config should have at least one outbound")
				}

				// Verify routing
				if config.Routing == nil {
					t.Error("Config should have routing")
				}
				if len(config.Routing.Rules) == 0 {
					t.Error("Routing should have rules")
				}

				// Verify stats and API
				if config.Stats == nil {
					t.Error("Config should have stats enabled")
				}
				if config.API == nil {
					t.Error("Config should have API enabled")
				}
			}
		})
	}
}

func TestConfigBuilder_WriteAndValidateConfig(t *testing.T) {
	builder := NewConfigBuilder()

	cfg := &store.DistributionConfig{
		ID:         1,
		ListenPort: 10808,
		Protocol:   "vless",
		Network:    "grpc",
		UUID:       "12345678-1234-1234-1234-123456789012",
		TLS:        false,
	}

	paths := []*store.DistributionPath{
		{
			ID:               1,
			Name:             "test-path",
			Path:             "test",
			UpstreamNodeKeys: []string{"1.2.3.4:443"},
			Enabled:          true,
		},
	}

	nodes := []*subscription.Node{
		{
			Type:            "vless",
			Server:          "1.2.3.4",
			Port:            443,
			UUID:            "test-uuid",
			Network:         "grpc",
			TLS:             false,
			GrpcServiceName: "GunService",
		},
	}

	config, err := builder.BuildConfig(cfg, paths, nodes)
	if err != nil {
		t.Fatalf("BuildConfig() error = %v", err)
	}

	// Test WriteConfig
	tmpFile := "/tmp/xray-test-config.json"
	defer os.Remove(tmpFile)

	err = builder.WriteConfig(config, tmpFile)
	if err != nil {
		t.Fatalf("WriteConfig() error = %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(tmpFile); err != nil {
		t.Errorf("Config file not created: %v", err)
	}

	// Verify JSON is valid
	data, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("Failed to read config file: %v", err)
	}

	var parsedConfig Config
	if err := json.Unmarshal(data, &parsedConfig); err != nil {
		t.Errorf("Config file is not valid JSON: %v", err)
	}

	// Note: ValidateConfig() requires xray binary, skip in test environment
	// In real deployment, this would call xray -test -c <path>
}

func TestConfigBuilder_BuildInbound(t *testing.T) {
	builder := NewConfigBuilder()

	tests := []struct {
		name    string
		cfg     *store.DistributionConfig
		wantErr bool
	}{
		{
			name: "vless+grpc with TLS",
			cfg: &store.DistributionConfig{
				ListenPort: 10808,
				Protocol:   "vless",
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
			name: "vmess+ws without TLS",
			cfg: &store.DistributionConfig{
				ListenPort: 10809,
				Protocol:   "vmess",
				Network:    "ws",
				UUID:       "test-uuid",
				TLS:        false,
			},
			wantErr: false,
		},
		{
			name: "unsupported protocol",
			cfg: &store.DistributionConfig{
				ListenPort: 10808,
				Protocol:   "unsupported",
				Network:    "tcp",
			},
			wantErr: true,
		},
		{
			name: "TLS without cert",
			cfg: &store.DistributionConfig{
				ListenPort: 10808,
				Protocol:   "vless",
				Network:    "grpc",
				TLS:        true,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inbound, err := builder.buildInbound(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("buildInbound() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if inbound.Protocol != tt.cfg.Protocol {
					t.Errorf("Protocol = %v, want %v", inbound.Protocol, tt.cfg.Protocol)
				}
				if inbound.Port != tt.cfg.ListenPort {
					t.Errorf("Port = %v, want %v", inbound.Port, tt.cfg.ListenPort)
				}
				if inbound.Listen != "127.0.0.1" {
					t.Errorf("Listen = %v, want 127.0.0.1 (inbounds must never bind to a public interface)", inbound.Listen)
				}
				if inbound.StreamSettings.Network != tt.cfg.Network {
					t.Errorf("Network = %v, want %v", inbound.StreamSettings.Network, tt.cfg.Network)
				}
			}
		})
	}
}

func TestConfigBuilder_BuildOutbound(t *testing.T) {
	builder := NewConfigBuilder()

	nodeMap := map[string]*subscription.Node{
		"1.2.3.4:443": {
			Type:            "vless",
			Server:          "1.2.3.4",
			Port:            443,
			UUID:            "test-uuid",
			Network:         "grpc",
			TLS:             true,
			SNI:             "node.example.com",
			GrpcServiceName: "GunService",
		},
		"5.6.7.8:80": {
			Type:    "vmess",
			Server:  "5.6.7.8",
			Port:    80,
			UUID:    "vmess-uuid",
			Network: "ws",
			TLS:     false,
		},
		"9.10.11.12:443": {
			Type:     "trojan",
			Server:   "9.10.11.12",
			Port:     443,
			Password: "trojan-pass",
			Network:  "tcp",
			TLS:      true,
			SNI:      "trojan.example.com",
		},
	}

	tests := []struct {
		name    string
		path    *store.DistributionPath
		wantErr bool
	}{
		{
			name: "vless outbound",
			path: &store.DistributionPath{
				Name:             "vless-path",
				UpstreamNodeKeys: []string{"1.2.3.4:443"},
			},
			wantErr: false,
		},
		{
			name: "vmess outbound",
			path: &store.DistributionPath{
				Name:             "vmess-path",
				UpstreamNodeKeys: []string{"5.6.7.8:80"},
			},
			wantErr: false,
		},
		{
			name: "trojan outbound",
			path: &store.DistributionPath{
				Name:             "trojan-path",
				UpstreamNodeKeys: []string{"9.10.11.12:443"},
			},
			wantErr: false,
		},
		{
			name: "no upstream nodes",
			path: &store.DistributionPath{
				Name:             "empty-path",
				UpstreamNodeKeys: []string{},
			},
			wantErr: true,
		},
		{
			name: "node not found",
			path: &store.DistributionPath{
				Name:             "missing-path",
				UpstreamNodeKeys: []string{"99.99.99.99:999"},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outbound, err := builder.buildOutbound(tt.path, nodeMap)
			if (err != nil) != tt.wantErr {
				t.Errorf("buildOutbound() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if outbound.Tag != "out-"+tt.path.Name {
					t.Errorf("Tag = %v, want %v", outbound.Tag, "out-"+tt.path.Name)
				}

				nodeKey := tt.path.UpstreamNodeKeys[0]
				expectedType := nodeMap[nodeKey].Type
				if outbound.Protocol != expectedType {
					t.Errorf("Protocol = %v, want %v", outbound.Protocol, expectedType)
				}
			}
		})
	}
}

func TestProcessManager_NewProcessManager(t *testing.T) {
	logger := &mockLogger{}
	pm := NewProcessManager("/tmp/xray-config.json", logger)

	if pm == nil {
		t.Fatal("NewProcessManager() returned nil")
	}
	if pm.configPath != "/tmp/xray-config.json" {
		t.Errorf("configPath = %v, want /tmp/xray-config.json", pm.configPath)
	}
	if pm.logger != logger {
		t.Error("logger not set correctly")
	}
}

func TestProcessManager_IsRunning(t *testing.T) {
	logger := &mockLogger{}
	pm := NewProcessManager("/tmp/xray-config.json", logger)

	// Process not started, should return false
	if pm.IsRunning() {
		t.Error("IsRunning() = true for non-started process")
	}
}

func TestStatsClient_NewStatsClient(t *testing.T) {
	tests := []struct {
		name     string
		apiAddr  string
		expected string
	}{
		{
			name:     "default address",
			apiAddr:  "",
			expected: "127.0.0.1:10085",
		},
		{
			name:     "custom address",
			apiAddr:  "192.168.1.100:8080",
			expected: "192.168.1.100:8080",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewStatsClient(tt.apiAddr)
			if client == nil {
				t.Fatal("NewStatsClient() returned nil")
			}
			if client.apiAddr != tt.expected {
				t.Errorf("apiAddr = %v, want %v", client.apiAddr, tt.expected)
			}
		})
	}
}

func TestStatsClient_GetPathStats(t *testing.T) {
	client := NewStatsClient("")

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{
			name:    "valid path",
			path:    "test-path",
			wantErr: false,
		},
		{
			name:    "empty path",
			path:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stats, err := client.GetPathStats(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetPathStats() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if stats == nil {
					t.Error("GetPathStats() returned nil stats")
					return
				}
				// Mock implementation returns non-zero values
				if stats.Upload == 0 && stats.Download == 0 {
					t.Error("Stats should have non-zero values in mock implementation")
				}
			}
		})
	}
}

func TestStatsClient_GetAllStats(t *testing.T) {
	client := NewStatsClient("")

	stats, err := client.GetAllStats()
	if err != nil {
		t.Errorf("GetAllStats() error = %v", err)
	}
	if len(stats) == 0 {
		t.Error("GetAllStats() should return mock data")
	}
}

func TestStatsClient_ResetPathStats(t *testing.T) {
	client := NewStatsClient("")

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{
			name:    "valid path",
			path:    "test-path",
			wantErr: false,
		},
		{
			name:    "empty path",
			path:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := client.ResetPathStats(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("ResetPathStats() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestConfig_JSONMarshaling(t *testing.T) {
	config := &Config{
		Log: &LogConfig{
			Loglevel: "warning",
		},
		Inbounds: []*Inbound{
			{
				Tag:      "test-in",
				Port:     10808,
				Protocol: "vless",
				Settings: map[string]interface{}{
					"clients": []map[string]interface{}{
						{"id": "test-uuid"},
					},
				},
				StreamSettings: &StreamSettings{
					Network: "grpc",
					GRPCSettings: &GRPCSettings{
						ServiceName: "GunService",
					},
				},
			},
		},
		Outbounds: []*Outbound{
			{
				Tag:      "test-out",
				Protocol: "freedom",
				Settings: map[string]interface{}{},
			},
		},
		Routing: &RoutingConfig{
			Rules: []*Rule{
				{
					Type:        "field",
					OutboundTag: "test-out",
				},
			},
		},
		Stats: &StatsConfig{},
		API: &APIConfig{
			Tag:      "api",
			Services: []string{"StatsService"},
		},
	}

	// Test marshaling
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("Failed to marshal config: %v", err)
	}

	// Test unmarshaling
	var unmarshaled Config
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("Failed to unmarshal config: %v", err)
	}

	// Verify key fields
	if len(unmarshaled.Inbounds) != 1 {
		t.Errorf("Inbounds count = %v, want 1", len(unmarshaled.Inbounds))
	}
	if len(unmarshaled.Outbounds) != 1 {
		t.Errorf("Outbounds count = %v, want 1", len(unmarshaled.Outbounds))
	}
	if unmarshaled.Log.Loglevel != "warning" {
		t.Errorf("Loglevel = %v, want warning", unmarshaled.Log.Loglevel)
	}
}

func TestNodeKey_Compatibility(t *testing.T) {
	// Test that node keys match expected format
	node := &subscription.Node{
		Server: "1.2.3.4",
		Port:   443,
	}

	key := node.NodeKey()
	expected := "1.2.3.4:443"
	if key != expected {
		t.Errorf("NodeKey() = %v, want %v", key, expected)
	}

	// Test with SNI
	node.SNI = "node.example.com"
	key = node.NodeKey()
	expected = "1.2.3.4:443:node.example.com"
	if key != expected {
		t.Errorf("NodeKey() with SNI = %v, want %v", key, expected)
	}
}

// TestImmutability verifies that builder methods don't mutate input
func TestImmutability(t *testing.T) {
	builder := NewConfigBuilder()

	originalCfg := &store.DistributionConfig{
		ListenPort: 10808,
		Protocol:   "vless",
		Network:    "grpc",
		UUID:       "original-uuid",
	}

	originalPaths := []*store.DistributionPath{
		{
			Name:             "original-path",
			UpstreamNodeKeys: []string{"1.2.3.4:443"},
			Enabled:          true,
		},
	}

	originalNodes := []*subscription.Node{
		{
			Type:   "vless",
			Server: "1.2.3.4",
			Port:   443,
			UUID:   "node-uuid",
		},
	}

	// Store original values
	origPort := originalCfg.ListenPort
	origPathName := originalPaths[0].Name
	origNodeServer := originalNodes[0].Server

	// Build config
	_, err := builder.BuildConfig(originalCfg, originalPaths, originalNodes)
	if err != nil {
		t.Fatalf("BuildConfig() error = %v", err)
	}

	// Verify no mutation
	if originalCfg.ListenPort != origPort {
		t.Error("BuildConfig() mutated input config")
	}
	if originalPaths[0].Name != origPathName {
		t.Error("BuildConfig() mutated input paths")
	}
	if originalNodes[0].Server != origNodeServer {
		t.Error("BuildConfig() mutated input nodes")
	}
}

// Benchmark config generation
func BenchmarkBuildConfig(b *testing.B) {
	builder := NewConfigBuilder()

	cfg := &store.DistributionConfig{
		ListenPort: 10808,
		Protocol:   "vless",
		Network:    "grpc",
		UUID:       "12345678-1234-1234-1234-123456789012",
		TLS:        false,
	}

	paths := []*store.DistributionPath{
		{
			Name:             "bench-path",
			UpstreamNodeKeys: []string{"1.2.3.4:443"},
			Enabled:          true,
		},
	}

	nodes := []*subscription.Node{
		{
			Type:            "vless",
			Server:          "1.2.3.4",
			Port:            443,
			UUID:            "test-uuid",
			Network:         "grpc",
			GrpcServiceName: "GunService",
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := builder.BuildConfig(cfg, paths, nodes)
		if err != nil {
			b.Fatal(err)
		}
	}
}
