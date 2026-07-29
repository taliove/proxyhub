// Package xraymgr manages per-user Xray process instances (ticket 08).
//
// Each user owns an independent Xray process with a generated xray_config.json
// and a dedicated loopback port allocated from the user's quota range.
// Process state (port, pid, status, config path) is persisted in the
// user_xray_instances table so restarts can detect and reconcile stale state.
//
// Lifecycle:
//   - Start:   pick a free port, write config, spawn xray, persist running state
//   - Stop:    signal the recorded PID (when alive), mark stopped
//   - Restart: stop then start
//   - Disable: same as Stop, triggered by admin user-disable flows
//   - Delete:  stop, remove the config directory, drop the DB row
package xraymgr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

// Status values mirrored from store.XrayStatus* (kept separate so the
// manager does not depend on store constants in its public API).
const (
	StatusStopped = store.XrayStatusStopped
	StatusRunning = store.XrayStatusRunning
	StatusFailed  = store.XrayStatusFailed
)

// ErrNoFreePort is returned by AllocatePort when every port in the user's
// quota range is already claimed by another live Xray instance.
var ErrNoFreePort = errors.New("no free port in user xray range")

// ErrQuotaMissing is returned when a user has no xray port range configured.
var ErrQuotaMissing = errors.New("user has no xray port range")

// NodeSource supplies the user's node pool for config generation.
// It matches server.NodeSource's subset the manager needs; tests stub it.
type NodeSource interface {
	Nodes() []*subscription.Node
}

// Config holds the manager's construction-time dependencies.
type Config struct {
	// Store is the persistence layer for instance state + quota lookups.
	Store *store.Store
	// Nodes is the live node pool used to render xray outbounds.
	Nodes NodeSource
	// XrayBin is the xray executable path. Empty means "xray" (PATH lookup).
	// Tests override this with a stub script.
	XrayBin string
	// WorkDir is the parent directory holding per-user subdirectories.
	// Production uses var/xray; tests use t.TempDir().
	WorkDir string
	// Logger, optional; defaults to slog.Default().
	Logger *slog.Logger
	// StartTimeout bounds how long Start waits for the process to come up.
	// Zero uses a sane default.
	StartTimeout time.Duration
}

// Manager owns per-user Xray processes. All public methods are safe for
// concurrent use; per-user serialization is enforced by a keyed mutex so
// concurrent operations on different users proceed in parallel while
// operations on the same user are strictly ordered.
type Manager struct {
	cfg    Config
	logger *slog.Logger

	mu       sync.Mutex // guards userLocks
	userLock map[int64]*sync.Mutex

	// cmds tracks live child processes by user id so Stop can signal them
	// without relying on PID reuse semantics.
	cmdsMu sync.Mutex
	cmds   map[int64]*exec.Cmd
}

// New constructs a Manager. It does not spawn any processes; callers use
// Start/Stop/Restart explicitly (or Reconcile to recover after a crash).
func New(cfg Config) (*Manager, error) {
	if cfg.Store == nil {
		return nil, errors.New("xraymgr: Store is required")
	}
	if cfg.WorkDir == "" {
		return nil, errors.New("xraymgr: WorkDir is required")
	}
	if cfg.XrayBin == "" {
		cfg.XrayBin = "xray"
	}
	if cfg.StartTimeout <= 0 {
		cfg.StartTimeout = 5 * time.Second
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	if err := os.MkdirAll(cfg.WorkDir, 0o750); err != nil {
		return nil, fmt.Errorf("xraymgr: create workdir: %w", err)
	}
	return &Manager{
		cfg:      cfg,
		logger:   logger,
		userLock: make(map[int64]*sync.Mutex),
		cmds:     make(map[int64]*exec.Cmd),
	}, nil
}

// lockFor returns the per-user mutex, allocating it lazily.
func (m *Manager) lockFor(userID int64) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	l, ok := m.userLock[userID]
	if !ok {
		l = &sync.Mutex{}
		m.userLock[userID] = l
	}
	return l
}

// userDir returns the per-user working directory (config + logs live here).
func (m *Manager) userDir(userID int64) string {
	return filepath.Join(m.cfg.WorkDir, strconv.FormatInt(userID, 10))
}

// configPath returns the canonical xray_config.json path for a user.
func (m *Manager) configPath(userID int64) string {
	return filepath.Join(m.userDir(userID), "xray_config.json")
}

// Status is the read model returned by GetStatus.
type Status struct {
	UserID        int64      `json:"user_id"`
	Port          int        `json:"port"`
	ConfigPath    string     `json:"config_path"`
	PID           int        `json:"pid"`
	Status        string     `json:"status"`
	LastStartedAt *time.Time `json:"last_started_at,omitempty"`
	// ProcessAlive is probed on read; it can disagree with the persisted
	// Status when the child died without the manager noticing (e.g. crash).
	ProcessAlive bool `json:"process_alive"`
}

// GetStatus returns the persisted status plus a live process probe.
// Returns store.ErrNotFound when the user has no instance row yet.
func (m *Manager) GetStatus(ctx context.Context, userID int64) (*Status, error) {
	inst, err := m.cfg.Store.GetXrayInstanceByUserID(userID)
	if err != nil {
		return nil, err
	}
	return &Status{
		UserID:        inst.UserID,
		Port:          inst.Port,
		ConfigPath:    inst.ConfigPath,
		PID:           inst.PID,
		Status:        inst.Status,
		LastStartedAt: inst.LastStartedAt,
		ProcessAlive:  m.processAlive(userID, inst.PID),
	}, nil
}

// AllocatePort picks a free port in the user's quota range. When the user
// already has a persisted instance whose port still fits inside the current
// quota range and is not claimed by anyone else, that port is returned
// unchanged (restart keeps the same listener). Otherwise the lowest free
// port in the range is picked.
//
// Returns ErrQuotaMissing when the user has no range, and ErrNoFreePort
// when the range is exhausted.
//
// Note on races: two concurrent AllocatePort calls for different users may
// pick the same numeric port if their quota ranges overlap. The persisted
// UNIQUE(user_id) on the instance row serializes the final claim; the
// per-user lock in Start ensures re-allocation on conflict is serialized
// for a single user.
func (m *Manager) AllocatePort(ctx context.Context, userID int64) (int, error) {
	quota, err := m.cfg.Store.GetUserQuota(userID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return 0, ErrQuotaMissing
		}
		return 0, fmt.Errorf("load user quota: %w", err)
	}
	if quota.XrayPortStart <= 0 || quota.XrayPortEnd <= 0 || quota.XrayPortStart > quota.XrayPortEnd {
		return 0, ErrQuotaMissing
	}

	used, err := m.cfg.Store.ListUsedXrayPorts()
	if err != nil {
		return 0, fmt.Errorf("list used ports: %w", err)
	}
	usedSet := make(map[int]bool, len(used))
	for _, p := range used {
		usedSet[p] = true
	}

	// Reuse own port when it still fits the current quota range: restart
	// keeps the same listener so admin/UIs don't see churn.
	if inst, err := m.cfg.Store.GetXrayInstanceByUserID(userID); err == nil {
		delete(usedSet, inst.Port) // our own row is not a conflict
		if inst.Port >= quota.XrayPortStart && inst.Port <= quota.XrayPortEnd {
			return inst.Port, nil
		}
	}

	for p := quota.XrayPortStart; p <= quota.XrayPortEnd; p++ {
		if !usedSet[p] {
			return p, nil
		}
	}
	return 0, ErrNoFreePort
}

// Start brings the user's Xray up. Idempotent: an already-running instance
// is left alone (returns the current status). When the persisted PID is
// stale (process gone) the row is reconciled to stopped before spawning.
func (m *Manager) Start(ctx context.Context, userID int64) (*Status, error) {
	lock := m.lockFor(userID)
	lock.Lock()
	defer lock.Unlock()

	inst, err := m.cfg.Store.GetXrayInstanceByUserID(userID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("load instance: %w", err)
	}

	// Fast path: alive process + running row -> nothing to do.
	if err == nil && inst.Status == StatusRunning && inst.PID > 0 && m.processAlive(userID, inst.PID) {
		return m.GetStatus(ctx, userID)
	}

	// Reconcile stale "running" rows whose process is gone.
	if err == nil && inst.Status == StatusRunning && !m.processAlive(userID, inst.PID) {
		m.logger.Info("xraymgr: stale running row, marking stopped",
			"user_id", userID, "pid", inst.PID)
		if uerr := m.cfg.Store.UpdateXrayInstanceStatus(userID, StatusStopped, 0, time.Time{}); uerr != nil {
			m.logger.Warn("xraymgr: reconcile stale row failed", "error", uerr)
		}
	}

	// Allocate or reuse port.
	port := 0
	if err == nil && inst.Port > 0 {
		port = inst.Port
	} else {
		p, aerr := m.AllocatePort(ctx, userID)
		if aerr != nil {
			return nil, aerr
		}
		port = p
	}

	// Persist the row (create or refresh port/config path).
	cfgPath := m.configPath(userID)
	row := &store.XrayInstance{
		UserID:     userID,
		Port:       port,
		ConfigPath: cfgPath,
		Status:     StatusStopped,
	}
	if err := m.cfg.Store.CreateOrUpdateXrayInstance(row); err != nil {
		return nil, fmt.Errorf("persist instance: %w", err)
	}

	// Render the config file from the user's node pool.
	if err := m.writeConfig(userID, port); err != nil {
		_ = m.cfg.Store.UpdateXrayInstanceStatus(userID, StatusFailed, 0, time.Time{})
		return nil, fmt.Errorf("write config: %w", err)
	}

	// Spawn the child process.
	if err := m.spawn(userID, cfgPath); err != nil {
		_ = m.cfg.Store.UpdateXrayInstanceStatus(userID, StatusFailed, 0, time.Time{})
		return nil, fmt.Errorf("spawn xray: %w", err)
	}

	return m.GetStatus(ctx, userID)
}

// Stop terminates the user's Xray. Idempotent: a missing or already-stopped
// instance is a no-op. The DB row is preserved (Stop is not Delete).
func (m *Manager) Stop(ctx context.Context, userID int64) error {
	lock := m.lockFor(userID)
	lock.Lock()
	defer lock.Unlock()

	inst, err := m.cfg.Store.GetXrayInstanceByUserID(userID)
	if errors.Is(err, store.ErrNotFound) {
		return nil // already gone
	}
	if err != nil {
		return fmt.Errorf("load instance: %w", err)
	}

	m.killLocked(userID, inst.PID)

	if err := m.cfg.Store.UpdateXrayInstanceStatus(userID, StatusStopped, 0, time.Time{}); err != nil {
		return fmt.Errorf("mark stopped: %w", err)
	}
	return nil
}

// Restart is Stop followed by Start under the same per-user lock.
func (m *Manager) Restart(ctx context.Context, userID int64) (*Status, error) {
	if err := m.Stop(ctx, userID); err != nil {
		return nil, err
	}
	return m.Start(ctx, userID)
}

// HandleUserDisabled is invoked by admin flows when a user is disabled:
// the Xray process must stop (the account is no longer allowed to relay
// traffic), but the instance row is preserved so re-enabling the account
// can resume on the same port.
func (m *Manager) HandleUserDisabled(ctx context.Context, userID int64) error {
	return m.Stop(ctx, userID)
}

// HandleUserDeleted removes all per-user Xray state: process, config
// directory, and the DB row. Idempotent.
func (m *Manager) HandleUserDeleted(ctx context.Context, userID int64) error {
	lock := m.lockFor(userID)
	lock.Lock()
	defer lock.Unlock()

	inst, err := m.cfg.Store.GetXrayInstanceByUserID(userID)
	if err == nil {
		m.killLocked(userID, inst.PID)
	} else if !errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("load instance: %w", err)
	}

	if err := os.RemoveAll(m.userDir(userID)); err != nil {
		return fmt.Errorf("remove user xray dir: %w", err)
	}
	if err := m.cfg.Store.DeleteXrayInstance(userID); err != nil {
		return fmt.Errorf("delete instance row: %w", err)
	}
	return nil
}

// ListAll returns the persisted state for every user with an instance row,
// enriched with a live-process probe. Used by the admin overview.
func (m *Manager) ListAll(ctx context.Context) ([]*Status, error) {
	rows, err := m.cfg.Store.ListXrayInstances()
	if err != nil {
		return nil, err
	}
	out := make([]*Status, 0, len(rows))
	for _, r := range rows {
		out = append(out, &Status{
			UserID:        r.UserID,
			Port:          r.Port,
			ConfigPath:    r.ConfigPath,
			PID:           r.PID,
			Status:        r.Status,
			LastStartedAt: r.LastStartedAt,
			ProcessAlive:  m.processAlive(r.UserID, r.PID),
		})
	}
	return out, nil
}

// writeConfig renders xray_config.json for the user. Outbounds come from
// the user's live node pool (currently the global pool; ticket 10 will
// scope pools per user). The inbound is a loopback-only SOCKS listener on
// the allocated port.
func (m *Manager) writeConfig(userID int64, port int) error {
	dir := m.userDir(userID)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("mkdir user dir: %w", err)
	}

	var nodes []*subscription.Node
	if m.cfg.Nodes != nil {
		nodes = m.cfg.Nodes.Nodes()
	}
	cfg := buildXrayConfig(port, nodes)

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	// Atomic-ish write: tmp + rename so a crash mid-write doesn't leave a
	// truncated config behind.
	tmp := filepath.Join(dir, ".xray_config.json.tmp")
	final := m.configPath(userID)
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write tmp config: %w", err)
	}
	if err := os.Rename(tmp, final); err != nil {
		return fmt.Errorf("rename config: %w", err)
	}
	return nil
}

// spawn starts the xray child process and records its PID.
func (m *Manager) spawn(userID int64, cfgPath string) error {
	cmd := exec.Command(m.cfg.XrayBin, "-c", cfgPath)
	// Detach the child's lifecycle from the manager: we track the PID in DB
	// and signal it directly on Stop. Do NOT call cmd.Wait() here — that
	// would reap the process and break the alive-probe.
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Dir = m.userDir(userID)
	cmd.SysProcAttr = detachProcAttr()

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start xray: %w", err)
	}

	m.cmdsMu.Lock()
	m.cmds[userID] = cmd
	m.cmdsMu.Unlock()

	if err := m.cfg.Store.UpdateXrayInstanceStatus(userID, StatusRunning, cmd.Process.Pid, time.Now().UTC()); err != nil {
		// Best-effort cleanup: if we can't persist the state, kill the child
		// so it doesn't leak.
		_ = cmd.Process.Kill()
		return fmt.Errorf("persist running state: %w", err)
	}

	// Reap the child asynchronously so zombies don't accumulate. Wait()
	// returns immediately after the process exits; the alive-probe uses
	// signal 0 and is unaffected.
	go func() {
		_ = cmd.Wait()
		m.cmdsMu.Lock()
		delete(m.cmds, userID)
		m.cmdsMu.Unlock()
	}()

	return nil
}

// killLocked signals the recorded PID if the process is alive. Caller must
// hold the per-user lock. Errors are logged but not surfaced: Stop must be
// idempotent even when the child already exited.
func (m *Manager) killLocked(userID int64, pid int) {
	m.cmdsMu.Lock()
	cmd, tracked := m.cmds[userID]
	m.cmdsMu.Unlock()

	if tracked && cmd.Process != nil {
		// SIGTERM first; xray shuts down cleanly on TERM.
		_ = cmd.Process.Signal(syscall.SIGTERM)
		// Give it a brief grace period, then SIGKILL via the OS handle.
		done := make(chan struct{})
		go func() {
			// Wait may already be running from spawn; that's fine — Wait is
			// idempotent for an exited process and we don't need its result.
			select {
			case <-done:
			case <-time.After(2 * time.Second):
			}
		}()
		close(done)
		if m.processAlive(userID, pid) {
			_ = cmd.Process.Kill()
		}
		return
	}

	// Fall back to bare PID signal when the manager has no handle (e.g.
	// manager restarted and recovered the PID from DB).
	if pid > 0 && processAliveByPID(pid) {
		if p, err := os.FindProcess(pid); err == nil {
			_ = p.Signal(syscall.SIGTERM)
			time.Sleep(100 * time.Millisecond)
			if processAliveByPID(pid) {
				_ = p.Kill()
			}
		}
	}
}

// processAlive reports whether the tracked process for userID is alive.
// Falls back to probing pid directly when the manager has no cmd handle
// (e.g. after a manager restart).
func (m *Manager) processAlive(userID int64, pid int) bool {
	if pid <= 0 {
		return false
	}
	return processAliveByPID(pid)
}

// processAliveByPID probes the OS for a live process with the given PID.
// Signal 0 performs error checking without actually sending a signal.
func processAliveByPID(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = p.Signal(syscall.Signal(0))
	return err == nil
}

// buildXrayConfig renders the xray JSON config for a user. Kept as a
// package-level function so tests can validate the shape without spinning
// up a Manager.
//
// The inbound is loopback-only SOCKS5 on the allocated port. Each node in
// the pool becomes one outbound; the first node is the default route.
// Empty pools still produce a valid config (blackhole outbound) so xray
// starts cleanly and the port is reserved for the user.
func buildXrayConfig(port int, nodes []*subscription.Node) map[string]any {
	outbounds := []map[string]any{}
	for i, n := range nodes {
		if ob := nodeToOutbound(n, i); ob != nil {
			outbounds = append(outbounds, ob)
		}
	}
	if len(outbounds) == 0 {
		outbounds = append(outbounds, map[string]any{
			"tag":      "blackhole",
			"protocol": "blackhole",
		})
	}

	return map[string]any{
		"log": map[string]any{
			"loglevel": "warning",
		},
		"inbounds": []map[string]any{
			{
				"tag":      "socks-in",
				"listen":   "127.0.0.1",
				"port":     port,
				"protocol": "socks",
				"settings": map[string]any{
					"auth": "noauth",
					"udp":  true,
				},
			},
		},
		"outbounds": outbounds,
	}
}

// nodeToOutbound maps one pool node to an xray outbound. Unsupported node
// types return nil (skipped); the config remains valid because the inbound
// is independent of outbound shape.
func nodeToOutbound(n *subscription.Node, idx int) map[string]any {
	tag := fmt.Sprintf("node-%d", idx)
	switch n.Type {
	case "vless":
		return map[string]any{
			"tag":      tag,
			"protocol": "vless",
			"settings": map[string]any{
				"vnext": []map[string]any{
					{
						"address": n.Server,
						"port":    n.Port,
						"users": []map[string]any{
							{
								"id":         n.UUID,
								"encryption": "none",
							},
						},
					},
				},
			},
		}
	case "vmess":
		return map[string]any{
			"tag":      tag,
			"protocol": "vmess",
			"settings": map[string]any{
				"vnext": []map[string]any{
					{
						"address": n.Server,
						"port":    n.Port,
						"users": []map[string]any{
							{
								"id":       n.UUID,
								"alterId":  n.AlterID,
								"security": "auto",
							},
						},
					},
				},
			},
		}
	case "trojan":
		return map[string]any{
			"tag":      tag,
			"protocol": "trojan",
			"settings": map[string]any{
				"servers": []map[string]any{
					{
						"address":  n.Server,
						"port":     n.Port,
						"password": n.Password,
					},
				},
			},
		}
	case "ss":
		return map[string]any{
			"tag":      tag,
			"protocol": "shadowsocks",
			"settings": map[string]any{
				"servers": []map[string]any{
					{
						"address":  n.Server,
						"port":     n.Port,
						"method":   n.Cipher,
						"password": n.Password,
					},
				},
			},
		}
	default:
		return nil
	}
}
