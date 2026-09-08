package config

import (
	"os"
	"testing"
	"time"
)

func TestLoad(t *testing.T) {
	content := `
server:
  host: "127.0.0.1"
  port: 8080
storage:
  path: "test.db"
health_check:
  interval: 15m
  latency_threshold: 500
  test_url: "https://www.google.com"
  timeout:
    latency: 5s
    request: 10s
  concurrent: 30
  retry: 2
fetch:
  connect_timeout: 20s
  read_timeout: 90s
filter:
  nodes_per_region: 10
  deduplicate: true
log:
  level: "info"
  format: "json"
`
	tmpfile, err := os.CreateTemp("", "config-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpfile.Name())

	if _, err := tmpfile.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	tmpfile.Close()

	cfg, err := Load(tmpfile.Name())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Server.Port != 8080 {
		t.Errorf("Port = %d, want 8080", cfg.Server.Port)
	}
	if cfg.HealthCheck.Interval != 15*time.Minute {
		t.Errorf("Interval = %v, want 15m", cfg.HealthCheck.Interval)
	}
	if cfg.Fetch.ConnectTimeout != 20*time.Second {
		t.Errorf("Fetch.ConnectTimeout = %v, want 20s", cfg.Fetch.ConnectTimeout)
	}
	if cfg.Fetch.ReadTimeout != 90*time.Second {
		t.Errorf("Fetch.ReadTimeout = %v, want 90s", cfg.Fetch.ReadTimeout)
	}
}

// TestLoadFetchDefaults fetch 段缺省时补默认(issue #156:15s/120s 拆分)。
func TestLoadFetchDefaults(t *testing.T) {
	content := `
server:
  host: "127.0.0.1"
  port: 8080
storage:
  path: "test.db"
health_check:
  interval: 15m
  latency_threshold: 500
  test_url: "https://www.google.com"
filter:
  nodes_per_region: 10
`
	tmpfile, err := os.CreateTemp("", "config-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpfile.Name())

	if _, err := tmpfile.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	tmpfile.Close()

	cfg, err := Load(tmpfile.Name())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Fetch.ConnectTimeout != 15*time.Second {
		t.Errorf("Fetch.ConnectTimeout = %v, want default 15s", cfg.Fetch.ConnectTimeout)
	}
	if cfg.Fetch.ReadTimeout != 120*time.Second {
		t.Errorf("Fetch.ReadTimeout = %v, want default 120s", cfg.Fetch.ReadTimeout)
	}
}
