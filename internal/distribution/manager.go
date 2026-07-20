package distribution

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"

	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
	"github.com/taliove/proxyhub/internal/xray"
)

// Manager orchestrates the traffic distribution feature
type Manager struct {
	store          *store.Store
	xrayProcess    *xray.ProcessManager
	xrayConfigPath string
	routingBuilder *RoutingBuilder
	statsCollector *StatsCollector
	logger         *slog.Logger
	mu             sync.RWMutex
}

// NewManager creates a new distribution manager
func NewManager(st *store.Store, xrayBinary string, xrayConfigPath string, logger *slog.Logger) *Manager {
	processManager := xray.NewProcessManager(xrayConfigPath, &loggerAdapter{logger: logger})

	return &Manager{
		store:          st,
		xrayProcess:    processManager,
		xrayConfigPath: xrayConfigPath,
		routingBuilder: &RoutingBuilder{},
		logger:         logger,
	}
}

// Start starts Xray process if distribution is enabled
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cfg, err := m.store.GetDistributionConfig()
	if err != nil {
		return fmt.Errorf("get distribution config: %w", err)
	}

	if !cfg.Enabled {
		m.logger.Info("distribution feature is disabled, skipping Xray startup")
		return nil
	}

	// Generate Xray config
	if err := m.regenerateConfig(ctx); err != nil {
		return fmt.Errorf("generate xray config: %w", err)
	}

	// Start Xray process
	if err := m.xrayProcess.Start(); err != nil {
		return fmt.Errorf("start xray process: %w", err)
	}

	m.logger.Info("distribution manager started", "config_path", m.xrayConfigPath)
	return nil
}

// Stop stops the Xray process gracefully
func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.xrayProcess.IsRunning() {
		m.logger.Info("xray process not running, nothing to stop")
		return nil
	}

	if err := m.xrayProcess.Stop(); err != nil {
		return fmt.Errorf("stop xray process: %w", err)
	}

	if m.statsCollector != nil {
		m.statsCollector = nil
	}

	m.logger.Info("distribution manager stopped")
	return nil
}

// Reload regenerates Xray config and restarts the process
func (m *Manager) Reload(ctx context.Context, nodes []*subscription.Node) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cfg, err := m.store.GetDistributionConfig()
	if err != nil {
		return fmt.Errorf("get distribution config: %w", err)
	}

	if !cfg.Enabled {
		// If disabled, stop if running
		if m.xrayProcess.IsRunning() {
			m.logger.Info("distribution disabled, stopping xray")
			if err := m.xrayProcess.Stop(); err != nil {
				return fmt.Errorf("stop xray: %w", err)
			}
		}
		return nil
	}

	// Generate new config
	if err := m.regenerateConfigWithNodes(ctx, nodes); err != nil {
		return fmt.Errorf("regenerate xray config: %w", err)
	}

	// Restart Xray
	if m.xrayProcess.IsRunning() {
		if err := m.xrayProcess.Restart(); err != nil {
			return fmt.Errorf("restart xray: %w", err)
		}
	} else {
		if err := m.xrayProcess.Start(); err != nil {
			return fmt.Errorf("start xray: %w", err)
		}
	}

	m.logger.Info("distribution config reloaded")
	return nil
}

// IsRunning checks if Xray process is running
func (m *Manager) IsRunning() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.xrayProcess.IsRunning()
}

// regenerateConfig generates Xray config file from current store state
func (m *Manager) regenerateConfig(ctx context.Context) error {
	// This is called during startup, nodes need to be fetched separately
	// For now, we'll generate config without nodes and expect Reload to be called
	return m.regenerateConfigWithNodes(ctx, nil)
}

// regenerateConfigWithNodes generates Xray config with provided nodes
func (m *Manager) regenerateConfigWithNodes(ctx context.Context, nodePool []*subscription.Node) error {
	cfg, err := m.store.GetDistributionConfig()
	if err != nil {
		return fmt.Errorf("get distribution config: %w", err)
	}

	// Fetch distribution nodes instead of distribution paths
	distNodes, err := m.store.ListDistributionNodes()
	if err != nil {
		return fmt.Errorf("list distribution nodes: %w", err)
	}

	xrayConfig, err := m.routingBuilder.BuildXrayConfig(cfg, distNodes, nodePool)
	if err != nil {
		return fmt.Errorf("build xray config: %w", err)
	}

	// Write config to file
	if err := writeXrayConfig(m.xrayConfigPath, xrayConfig); err != nil {
		return fmt.Errorf("write xray config: %w", err)
	}

	m.logger.Info("xray config regenerated",
		"path", m.xrayConfigPath,
		"enabled_nodes", len(distNodes))
	return nil
}

// loggerAdapter adapts slog.Logger to xray.Logger interface
type loggerAdapter struct {
	logger *slog.Logger
}

func (l *loggerAdapter) Info(msg string, args ...interface{}) {
	l.logger.Info(msg, args...)
}

func (l *loggerAdapter) Error(msg string, args ...interface{}) {
	l.logger.Error(msg, args...)
}

// writeXrayConfig writes Xray config to file
func writeXrayConfig(path string, config *XrayConfig) error {
	if config == nil {
		return errors.New("xray config is nil")
	}

	jsonData, err := config.ToJSON()
	if err != nil {
		return fmt.Errorf("convert config to JSON: %w", err)
	}

	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create config file: %w", err)
	}
	defer file.Close()

	if _, err := file.WriteString(jsonData); err != nil {
		return fmt.Errorf("write config file: %w", err)
	}

	return nil
}
