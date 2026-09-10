package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config file: %w", err)
	}

	// 环境变量覆盖:容器部署不便改 yaml,setup token 走注入。
	if v := os.Getenv("PROXYHUB_SETUP_TOKEN"); v != "" {
		cfg.Server.SetupToken = v
	}

	if err := validate(&cfg); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}

	applyDefaults(&cfg)
	return &cfg, nil
}

func applyDefaults(cfg *Config) {
	if cfg.Storage.Path == "" {
		// Local runtime state belongs under var/ (gitignored); never
		// default to the repository root.
		cfg.Storage.Path = filepath.Join("var", "data", "data.db")
	}
	if cfg.HealthCheck.Concurrent <= 0 {
		cfg.HealthCheck.Concurrent = 30
	}
	if cfg.HealthCheck.Timeout.Latency <= 0 {
		cfg.HealthCheck.Timeout.Latency = 5 * time.Second
	}
	if cfg.HealthCheck.Timeout.Request <= 0 {
		cfg.HealthCheck.Timeout.Request = 10 * time.Second
	}
	if cfg.Server.Host == "" {
		cfg.Server.Host = "127.0.0.1"
	}
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8080
	}
	// 订阅拉取超时拆分(issue #143):建连/响应头 15s,体读取总时长 120s。
	if cfg.Fetch.ConnectTimeout <= 0 {
		cfg.Fetch.ConnectTimeout = 15 * time.Second
	}
	if cfg.Fetch.ReadTimeout <= 0 {
		cfg.Fetch.ReadTimeout = 120 * time.Second
	}
}

func validate(cfg *Config) error {
	if cfg.Server.Port <= 0 || cfg.Server.Port > 65535 {
		return fmt.Errorf("invalid port: %d", cfg.Server.Port)
	}
	if cfg.HealthCheck.Interval <= 0 {
		return fmt.Errorf("health check interval must be positive")
	}
	if cfg.HealthCheck.LatencyThreshold <= 0 {
		return fmt.Errorf("latency threshold must be positive")
	}
	if cfg.HealthCheck.TestURL == "" {
		return fmt.Errorf("test url is required")
	}
	if cfg.Filter.NodesPerRegion <= 0 {
		return fmt.Errorf("nodes per region must be positive")
	}
	return nil
}
