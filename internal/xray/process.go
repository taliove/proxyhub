package xray

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/xtls/xray-core/core"
)

// ProcessManager manages Xray-core instance lifecycle
type ProcessManager struct {
	configPath string
	instance   *core.Instance
	logger     Logger
	mu         sync.RWMutex
	ctx        context.Context
	cancel     context.CancelFunc
}

// Logger defines logging interface for ProcessManager
type Logger interface {
	Info(msg string, args ...interface{})
	Error(msg string, args ...interface{})
}

// NewProcessManager creates a new ProcessManager instance
func NewProcessManager(configPath string, logger Logger) *ProcessManager {
	return &ProcessManager{
		configPath: configPath,
		logger:     logger,
	}
}

// Start creates and starts Xray instance
func (pm *ProcessManager) Start() error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pm.instance != nil {
		return errors.New("xray instance already running")
	}

	// Verify config file exists
	if _, err := os.Stat(pm.configPath); err != nil {
		return fmt.Errorf("config file not found: %w", err)
	}

	// Read config file
	configData, err := os.ReadFile(pm.configPath)
	if err != nil {
		return fmt.Errorf("read xray config: %w", err)
	}

	// Load Xray config from JSON bytes
	config, err := core.LoadConfig("json", configData)
	if err != nil {
		return fmt.Errorf("load xray config: %w", err)
	}

	// Create Xray instance
	instance, err := core.New(config)
	if err != nil {
		return fmt.Errorf("create xray instance: %w", err)
	}

	// Start Xray instance
	ctx, cancel := context.WithCancel(context.Background())
	if err := instance.Start(); err != nil {
		cancel()
		return fmt.Errorf("start xray instance: %w", err)
	}

	pm.instance = instance
	pm.ctx = ctx
	pm.cancel = cancel
	pm.logger.Info("xray instance started")
	return nil
}

// Stop gracefully stops Xray instance
func (pm *ProcessManager) Stop() error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pm.instance == nil {
		return errors.New("xray instance not running")
	}

	// Cancel context
	if pm.cancel != nil {
		pm.cancel()
	}

	// Close instance
	if err := pm.instance.Close(); err != nil {
		pm.logger.Error("xray instance close error", "error", err)
		return fmt.Errorf("close xray instance: %w", err)
	}

	pm.instance = nil
	pm.ctx = nil
	pm.cancel = nil
	pm.logger.Info("xray instance stopped")
	return nil
}

// Restart restarts Xray instance
func (pm *ProcessManager) Restart() error {
	if err := pm.Stop(); err != nil && err.Error() != "xray instance not running" {
		return err
	}
	return pm.Start()
}

// IsRunning checks if Xray instance is running
func (pm *ProcessManager) IsRunning() bool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.instance != nil
}
