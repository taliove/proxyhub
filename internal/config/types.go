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
	// TrustedProxies 声明受信反代的对端 CIDR(或单 IP)。只有这些对端携带的
	// X-Forwarded-For / X-Real-IP 才会被采信。未设置(缺省)沿用 loopback
	// 惯例(Caddy 拓扑);显式置为空列表表示不信任任何对端(直连暴露部署)。
	TrustedProxies []string `yaml:"trusted_proxies"`
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
