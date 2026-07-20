package config

import "time"

// Config 应用配置（简化版）
type Config struct {
	Server      ServerConfig      `yaml:"server"`
	Storage     StorageConfig     `yaml:"storage"`
	HealthCheck HealthCheckConfig `yaml:"health_check"`
	Filter      FilterConfig      `yaml:"filter"`
	Log         LogConfig         `yaml:"log"`
}

type StorageConfig struct {
	Path string `yaml:"path"`
}

type ServerConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

type HealthCheckConfig struct {
	Interval         time.Duration `yaml:"interval"`
	LatencyThreshold int           `yaml:"latency_threshold"`
	TestURL          string        `yaml:"test_url"`
	Timeout          TimeoutConfig `yaml:"timeout"`
	Concurrent       int           `yaml:"concurrent"`
	Retry            int           `yaml:"retry"`
}

type TimeoutConfig struct {
	Latency time.Duration `yaml:"latency"`
	Request time.Duration `yaml:"request"`
}

type FilterConfig struct {
	NodesPerRegion int  `yaml:"nodes_per_region"`
	Deduplicate    bool `yaml:"deduplicate"`
}

type LogConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}
