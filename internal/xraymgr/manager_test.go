package xraymgr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

// fakeNodes implements NodeSource with a fixed pool.
type fakeNodes struct {
	nodes []*subscription.Node
}

func (f *fakeNodes) Nodes() []*subscription.Node { return f.nodes }

// newTestManager builds a Manager backed by a temp-dir store and a stub
// xray binary that sleeps forever. The stub is a POSIX shell script: it
// prints its PID to a well-known file so tests can prove the manager
// started the child and then signal it correctly.
func newTestManager(t *testing.T, nodes []*subscription.Node) (*Manager, *store.Store, string) {
	t.Helper()

	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.db")
	st, err := store.OpenForTesting(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { st.Close() })

	workDir := filepath.Join(tmp, "xray")
	binPath := filepath.Join(tmp, "xray-stub.sh")

	// Stub xray: POSIX shell script that just sleeps. Tests assert the
	// manager wrote the config and tracked the PID; we never invoke real
	// xray here (would require the binary on CI).
	stub := `#!/bin/sh
# xray stub: record args, then sleep so the manager sees a live process.
echo "$@" > "` + filepath.Join(tmp, "xray.args") + `"
exec sleep 3600
`
	if err := os.WriteFile(binPath, []byte(stub), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}

	m, err := New(Config{
		Store:   st,
		Nodes:   &fakeNodes{nodes: nodes},
		XrayBin: binPath,
		WorkDir: workDir,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return m, st, tmp
}

// seedUserWithQuota inserts a user + quota row and returns the user id.
// Tests rely on the ticket 01/06 user tables having been migrated by Open().
func seedUserWithQuota(t *testing.T, st *store.Store, username string, portStart, portEnd int) int64 {
	t.Helper()
	u, err := st.CreateUser(username, "x", store.RoleUser, false)
	if err != nil {
		t.Fatalf("CreateUser(%s) error = %v", username, err)
	}
	if err := st.UpsertUserQuota(&store.UserQuota{
		UserID:        u.ID,
		MaxAirports:   10,
		MaxEndpoints:  10,
		XrayPortStart: portStart,
		XrayPortEnd:   portEnd,
	}); err != nil {
		t.Fatalf("UpsertUserQuota() error = %v", err)
	}
	return u.ID
}

// TestAllocatePort_FirstFree verifies the lowest free port in the user's
// range is picked, skipping ports already claimed by other users.
func TestAllocatePort_FirstFree(t *testing.T) {
	m, st, _ := newTestManager(t, nil)
	ctx := context.Background()

	userA := seedUserWithQuota(t, st, "alice", 20000, 20010)
	userB := seedUserWithQuota(t, st, "bob", 20000, 20010)

	// userA takes 20000.
	portA, err := m.AllocatePort(ctx, userA)
	if err != nil {
		t.Fatalf("AllocatePort(userA) error = %v", err)
	}
	if portA != 20000 {
		t.Fatalf("AllocatePort(userA) = %d, want 20000", portA)
	}
	if err := st.CreateOrUpdateXrayInstance(&store.XrayInstance{
		UserID:     userA,
		Port:       portA,
		ConfigPath: "x",
		Status:     store.XrayStatusStopped,
	}); err != nil {
		t.Fatalf("persist userA instance: %v", err)
	}

	// userB must skip 20000 (taken) and land on 20001.
	portB, err := m.AllocatePort(ctx, userB)
	if err != nil {
		t.Fatalf("AllocatePort(userB) error = %v", err)
	}
	if portB != 20001 {
		t.Fatalf("AllocatePort(userB) = %d, want 20001 (must skip taken)", portB)
	}
}

// TestAllocatePort_ExhaustedRange verifies ErrNoFreePort when every port in
// the range is taken.
func TestAllocatePort_ExhaustedRange(t *testing.T) {
	m, st, _ := newTestManager(t, nil)
	ctx := context.Background()

	// Tiny 2-port range shared by three users -> third one exhausts.
	userA := seedUserWithQuota(t, st, "u1", 21000, 21001)
	userB := seedUserWithQuota(t, st, "u2", 21000, 21001)
	userC := seedUserWithQuota(t, st, "u3", 21000, 21001)

	for i, uid := range []int64{userA, userB} {
		p, err := m.AllocatePort(ctx, uid)
		if err != nil {
			t.Fatalf("AllocatePort(user %d) error = %v", i, err)
		}
		if err := st.CreateOrUpdateXrayInstance(&store.XrayInstance{
			UserID: uid, Port: p, ConfigPath: "x", Status: store.XrayStatusStopped,
		}); err != nil {
			t.Fatalf("persist user %d: %v", i, err)
		}
	}

	_, err := m.AllocatePort(ctx, userC)
	if !errors.Is(err, ErrNoFreePort) {
		t.Fatalf("AllocatePort exhausted err = %v, want ErrNoFreePort", err)
	}
}

// TestAllocatePort_NoQuota verifies ErrQuotaMissing when the user has no
// xray range configured (zero start/end).
func TestAllocatePort_NoQuota(t *testing.T) {
	m, st, _ := newTestManager(t, nil)
	ctx := context.Background()

	u, err := st.CreateUser("noquota", "x", store.RoleUser, false)
	if err != nil {
		t.Fatalf("CreateUser error = %v", err)
	}
	if err := st.UpsertUserQuota(&store.UserQuota{UserID: u.ID}); err != nil {
		t.Fatalf("UpsertUserQuota error = %v", err)
	}

	if _, err := m.AllocatePort(ctx, u.ID); !errors.Is(err, ErrQuotaMissing) {
		t.Fatalf("AllocatePort err = %v, want ErrQuotaMissing", err)
	}
}

// TestAllocatePort_ReuseOwnPort verifies a user re-starting keeps their
// existing port (the allocator must not treat the user's own row as a
// conflict).
func TestAllocatePort_ReuseOwnPort(t *testing.T) {
	m, st, _ := newTestManager(t, nil)
	ctx := context.Background()

	uid := seedUserWithQuota(t, st, "self", 22000, 22010)
	if err := st.CreateOrUpdateXrayInstance(&store.XrayInstance{
		UserID: uid, Port: 22005, ConfigPath: "x", Status: store.XrayStatusStopped,
	}); err != nil {
		t.Fatalf("persist: %v", err)
	}

	p, err := m.AllocatePort(ctx, uid)
	if err != nil {
		t.Fatalf("AllocatePort error = %v", err)
	}
	if p != 22005 {
		t.Fatalf("AllocatePort = %d, want 22005 (reuse own)", p)
	}
}

// TestStart_WritesConfigAndSpawns covers the happy path: config file is
// written under var/xray/<uid>/xray_config.json, the stub process is
// spawned, and the DB row reflects running state with a live PID.
func TestStart_WritesConfigAndSpawns(t *testing.T) {
	nodes := []*subscription.Node{
		{
			Name:   "test-vless",
			Type:   "vless",
			Server: "example.com",
			Port:   443,
			UUID:   "00000000-0000-0000-0000-000000000000",
		},
	}
	m, st, tmp := newTestManager(t, nodes)
	ctx := context.Background()

	uid := seedUserWithQuota(t, st, "happy", 23000, 23010)

	st1, err := m.Start(ctx, uid)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() { _ = m.Stop(context.Background(), uid) }()

	if st1.Status != StatusRunning {
		t.Errorf("Status = %q, want running", st1.Status)
	}
	if st1.Port != 23000 {
		t.Errorf("Port = %d, want 23000", st1.Port)
	}
	if st1.PID <= 0 {
		t.Errorf("PID = %d, want > 0", st1.PID)
	}
	if !st1.ProcessAlive {
		t.Errorf("ProcessAlive = false, want true (stub sleep)")
	}
	if st1.LastStartedAt == nil {
		t.Errorf("LastStartedAt = nil, want non-nil")
	}

	// Config file exists at the expected path with the allocated port.
	wantCfg := filepath.Join(tmp, "xray", strconv.FormatInt(uid, 10), "xray_config.json")
	if st1.ConfigPath != wantCfg {
		t.Errorf("ConfigPath = %q, want %q", st1.ConfigPath, wantCfg)
	}
	data, err := os.ReadFile(wantCfg)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parse config: %v", err)
	}
	inbounds, ok := cfg["inbounds"].([]any)
	if !ok || len(inbounds) == 0 {
		t.Fatalf("inbounds missing in config: %v", cfg)
	}
	in0 := inbounds[0].(map[string]any)
	if int(in0["port"].(float64)) != 23000 {
		t.Errorf("config inbound port = %v, want 23000", in0["port"])
	}
	if in0["listen"] != "127.0.0.1" {
		t.Errorf("config listen = %v, want 127.0.0.1 (loopback-only)", in0["listen"])
	}
	// Outbound rendered from the pool node.
	outbounds := cfg["outbounds"].([]any)
	if len(outbounds) != 1 {
		t.Fatalf("outbounds len = %d, want 1", len(outbounds))
	}
	ob := outbounds[0].(map[string]any)
	if ob["protocol"] != "vless" {
		t.Errorf("outbound protocol = %v, want vless", ob["protocol"])
	}
}

// TestStart_Idempotent verifies a second Start on a running instance is a
// no-op and does not change port/PID.
func TestStart_Idempotent(t *testing.T) {
	m, st, _ := newTestManager(t, nil)
	ctx := context.Background()
	uid := seedUserWithQuota(t, st, "idem", 24000, 24010)

	st1, err := m.Start(ctx, uid)
	if err != nil {
		t.Fatalf("first Start: %v", err)
	}
	defer func() { _ = m.Stop(context.Background(), uid) }()

	st2, err := m.Start(ctx, uid)
	if err != nil {
		t.Fatalf("second Start: %v", err)
	}
	if st2.Port != st1.Port || st2.PID != st1.PID {
		t.Errorf("second Start changed state: port %d->%d, pid %d->%d",
			st1.Port, st2.Port, st1.PID, st2.PID)
	}
}

// TestStop_KillsProcess verifies Stop terminates the child and persists
// stopped state.
func TestStop_KillsProcess(t *testing.T) {
	m, st, _ := newTestManager(t, nil)
	ctx := context.Background()
	uid := seedUserWithQuota(t, st, "stopme", 25000, 25010)

	st1, err := m.Start(ctx, uid)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	pid := st1.PID

	if err := m.Stop(ctx, uid); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// Give the OS a beat to actually reap the process (SIGTERM is async).
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && processAliveByPID(pid) {
		time.Sleep(20 * time.Millisecond)
	}
	if processAliveByPID(pid) {
		t.Errorf("process %d still alive after Stop", pid)
	}

	st2, err := m.GetStatus(ctx, uid)
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if st2.Status != StatusStopped {
		t.Errorf("Status = %q, want stopped", st2.Status)
	}
	if st2.PID != 0 {
		t.Errorf("PID = %d, want 0 after stop", st2.PID)
	}
}

// TestStop_IdempotentWhenMissing verifies Stop on a user with no instance
// is a no-op (returns nil, not error).
func TestStop_IdempotentWhenMissing(t *testing.T) {
	m, st, _ := newTestManager(t, nil)
	ctx := context.Background()
	u, err := st.CreateUser("never", "x", store.RoleUser, false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := m.Stop(ctx, u.ID); err != nil {
		t.Fatalf("Stop on missing user: %v, want nil", err)
	}
}

// TestRestart_ReissuesProcess verifies Restart produces a new PID while
// keeping the same port.
func TestRestart_ReissuesProcess(t *testing.T) {
	m, st, _ := newTestManager(t, nil)
	ctx := context.Background()
	uid := seedUserWithQuota(t, st, "restart", 26000, 26010)

	st1, err := m.Start(ctx, uid)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = m.Stop(context.Background(), uid) }()

	st2, err := m.Restart(ctx, uid)
	if err != nil {
		t.Fatalf("Restart: %v", err)
	}
	if st2.Port != st1.Port {
		t.Errorf("Restart changed port: %d -> %d, want stable", st1.Port, st2.Port)
	}
	if st2.PID == st1.PID {
		t.Errorf("Restart kept PID %d, want a fresh process", st1.PID)
	}
	if st2.Status != StatusRunning {
		t.Errorf("Status after restart = %q, want running", st2.Status)
	}
}

// TestHandleUserDisabled_StopsProcess verifies disabled users get their
// Xray stopped while the DB row is preserved (so re-enable can resume).
func TestHandleUserDisabled_StopsProcess(t *testing.T) {
	m, st, _ := newTestManager(t, nil)
	ctx := context.Background()
	uid := seedUserWithQuota(t, st, "disable", 27000, 27010)

	if _, err := m.Start(ctx, uid); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if err := m.HandleUserDisabled(ctx, uid); err != nil {
		t.Fatalf("HandleUserDisabled: %v", err)
	}

	st2, err := m.GetStatus(ctx, uid)
	if err != nil {
		t.Fatalf("GetStatus: %v (row must survive disable)", err)
	}
	if st2.Status != StatusStopped {
		t.Errorf("Status = %q, want stopped", st2.Status)
	}
}

// TestHandleUserDeleted_RemovesState verifies deleted users get everything
// cleaned up: process killed, config dir removed, DB row dropped.
func TestHandleUserDeleted_RemovesState(t *testing.T) {
	m, st, tmp := newTestManager(t, nil)
	ctx := context.Background()
	uid := seedUserWithQuota(t, st, "delete", 28000, 28010)

	st1, err := m.Start(ctx, uid)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	pid := st1.PID

	if err := m.HandleUserDeleted(ctx, uid); err != nil {
		t.Fatalf("HandleUserDeleted: %v", err)
	}

	// DB row gone.
	if _, err := st.GetXrayInstanceByUserID(uid); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("GetXrayInstanceByUserID err = %v, want ErrNotFound", err)
	}
	// Config dir gone.
	userDir := filepath.Join(tmp, "xray", strconv.FormatInt(uid, 10))
	if _, err := os.Stat(userDir); !os.IsNotExist(err) {
		t.Errorf("user dir still exists: %v", err)
	}
	// Process dead (with the usual SIGTERM async grace).
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && processAliveByPID(pid) {
		time.Sleep(20 * time.Millisecond)
	}
	if processAliveByPID(pid) {
		t.Errorf("process %d still alive after delete", pid)
	}
}

// TestHandleUserDeleted_IdempotentWhenMissing verifies deletion on a user
// with no prior state is a clean no-op.
func TestHandleUserDeleted_IdempotentWhenMissing(t *testing.T) {
	m, st, _ := newTestManager(t, nil)
	ctx := context.Background()
	u, err := st.CreateUser("gone", "x", store.RoleUser, false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := m.HandleUserDeleted(ctx, u.ID); err != nil {
		t.Fatalf("HandleUserDeleted on missing: %v, want nil", err)
	}
}

// TestBuildXrayConfig_EmptyPool verifies an empty node pool still produces
// a valid config with a blackhole outbound (port reservation path).
func TestBuildXrayConfig_EmptyPool(t *testing.T) {
	cfg := buildXrayConfig(10808, nil)
	inbounds := cfg["inbounds"].([]map[string]any)
	if inbounds[0]["port"] != 10808 {
		t.Errorf("inbound port = %v, want 10808", inbounds[0]["port"])
	}
	outbounds := cfg["outbounds"].([]map[string]any)
	if len(outbounds) != 1 || outbounds[0]["protocol"] != "blackhole" {
		t.Errorf("empty pool should fall back to blackhole outbound, got %v", outbounds)
	}
}

// TestBuildXrayConfig_SkipsUnsupportedNodes verifies unsupported protocols
// are dropped rather than producing invalid xray config.
func TestBuildXrayConfig_SkipsUnsupportedNodes(t *testing.T) {
	nodes := []*subscription.Node{
		{Type: "hysteria2", Server: "x", Port: 1}, // unsupported
		{Type: "ss", Server: "example.com", Port: 8388, Cipher: "aes-256-gcm", Password: "p"},
	}
	cfg := buildXrayConfig(11000, nodes)
	outbounds := cfg["outbounds"].([]map[string]any)
	if len(outbounds) != 1 {
		t.Fatalf("outbounds len = %d, want 1 (hysteria2 skipped)", len(outbounds))
	}
	if outbounds[0]["protocol"] != "shadowsocks" {
		t.Errorf("outbound protocol = %v, want shadowsocks", outbounds[0]["protocol"])
	}
}

// TestSpawnFailure_MarksFailed verifies a missing xray binary flips the row
// to failed instead of leaving it stuck in "starting".
func TestSpawnFailure_MarksFailed(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.db")
	st, err := store.OpenForTesting(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	m, err := New(Config{
		Store:   st,
		Nodes:   &fakeNodes{},
		XrayBin: filepath.Join(tmp, "no-such-binary"),
		WorkDir: filepath.Join(tmp, "xray"),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	uid := seedUserWithQuota(t, st, "spawnfail", 29000, 29010)
	_, err = m.Start(context.Background(), uid)
	if err == nil {
		t.Fatalf("Start with missing binary should fail")
	}
	if !strings.Contains(err.Error(), "spawn xray") {
		t.Errorf("error = %v, want wrapped 'spawn xray'", err)
	}

	inst, gerr := st.GetXrayInstanceByUserID(uid)
	if gerr != nil {
		t.Fatalf("GetXrayInstanceByUserID: %v", gerr)
	}
	if inst.Status != store.XrayStatusFailed {
		t.Errorf("Status = %q, want failed after spawn error", inst.Status)
	}
}

// TestListAll_ReturnsAllInstances verifies the admin overview helper.
func TestListAll_ReturnsAllInstances(t *testing.T) {
	m, st, _ := newTestManager(t, nil)
	ctx := context.Background()

	for i, name := range []string{"l1", "l2", "l3"} {
		uid := seedUserWithQuota(t, st, name, 30000+i*10, 30009+i*10)
		if err := st.CreateOrUpdateXrayInstance(&store.XrayInstance{
			UserID: uid, Port: 30000 + i*10, ConfigPath: "x", Status: store.XrayStatusStopped,
		}); err != nil {
			t.Fatalf("persist %d: %v", i, err)
		}
	}

	list, err := m.ListAll(ctx)
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("len(ListAll) = %d, want 3", len(list))
	}
}

// Ensure unused imports stay referenced.
var _ = fmt.Sprintf
