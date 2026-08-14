package store

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// newUserXrayTestStore opens a fresh in-memory-style store for user_xray tests.
// We still go through the on-disk OpenForTesting() path (t.TempDir) so migrations apply
// exactly the same as production.
func newUserXrayTestStore(t *testing.T) *Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := OpenForTesting(dbPath)
	if err != nil {
		t.Fatalf("OpenForTesting() error = %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// TestXrayInstance_TableExists verifies migration 019 creates the table with
// the expected unique constraint on user_id.
func TestXrayInstance_TableExists(t *testing.T) {
	s := newUserXrayTestStore(t)

	inst := &XrayInstance{
		UserID:     1,
		Port:       11000,
		ConfigPath: "var/xray/1/xray_config.json",
		Status:     XrayStatusStopped,
	}
	if err := s.CreateOrUpdateXrayInstance(inst); err != nil {
		t.Fatalf("CreateOrUpdateXrayInstance() error = %v", err)
	}

	// Second create with the same user_id must not fail (upsert), and must
	// not produce a second row.
	inst2 := &XrayInstance{
		UserID:     1,
		Port:       11001,
		ConfigPath: "var/xray/1/xray_config.json",
		Status:     XrayStatusRunning,
		PID:        4242,
	}
	if err := s.CreateOrUpdateXrayInstance(inst2); err != nil {
		t.Fatalf("CreateOrUpdateXrayInstance() upsert error = %v", err)
	}

	got, err := s.GetXrayInstanceByUserID(1)
	if err != nil {
		t.Fatalf("GetXrayInstanceByUserID() error = %v", err)
	}
	if got.Port != 11001 {
		t.Errorf("Port = %d, want 11001 (upsert should overwrite)", got.Port)
	}
	if got.Status != XrayStatusRunning {
		t.Errorf("Status = %q, want %q", got.Status, XrayStatusRunning)
	}
	if got.PID != 4242 {
		t.Errorf("PID = %d, want 4242", got.PID)
	}
}

// TestXrayInstance_GetByUserID_NotFound covers the ErrNotFound path.
func TestXrayInstance_GetByUserID_NotFound(t *testing.T) {
	s := newUserXrayTestStore(t)
	_, err := s.GetXrayInstanceByUserID(999)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetXrayInstanceByUserID() err = %v, want ErrNotFound", err)
	}
}

// TestXrayInstance_UpdateStatus verifies status + pid + last_started_at update.
func TestXrayInstance_UpdateStatus(t *testing.T) {
	s := newUserXrayTestStore(t)

	inst := &XrayInstance{
		UserID:     7,
		Port:       12000,
		ConfigPath: "var/xray/7/xray_config.json",
		Status:     XrayStatusStopped,
	}
	if err := s.CreateOrUpdateXrayInstance(inst); err != nil {
		t.Fatalf("CreateOrUpdateXrayInstance() error = %v", err)
	}

	started := time.Now().UTC().Truncate(time.Second)
	if err := s.UpdateXrayInstanceStatus(7, XrayStatusRunning, 5555, started); err != nil {
		t.Fatalf("UpdateXrayInstanceStatus() error = %v", err)
	}

	got, err := s.GetXrayInstanceByUserID(7)
	if err != nil {
		t.Fatalf("GetXrayInstanceByUserID() error = %v", err)
	}
	if got.Status != XrayStatusRunning {
		t.Errorf("Status = %q, want running", got.Status)
	}
	if got.PID != 5555 {
		t.Errorf("PID = %d, want 5555", got.PID)
	}
	if got.LastStartedAt == nil {
		t.Fatalf("LastStartedAt = nil, want %v", started)
	}
	if !got.LastStartedAt.Equal(started) {
		t.Errorf("LastStartedAt = %v, want %v", got.LastStartedAt, started)
	}
}

// TestXrayInstance_UpdateStatus_NotFound ensures updating a missing user errors.
func TestXrayInstance_UpdateStatus_NotFound(t *testing.T) {
	s := newUserXrayTestStore(t)
	err := s.UpdateXrayInstanceStatus(12345, XrayStatusRunning, 1, time.Now())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("UpdateXrayInstanceStatus() err = %v, want ErrNotFound", err)
	}
}

// TestXrayInstance_Delete verifies Delete removes the row and is idempotent.
func TestXrayInstance_Delete(t *testing.T) {
	s := newUserXrayTestStore(t)

	inst := &XrayInstance{UserID: 3, Port: 13000, ConfigPath: "x", Status: XrayStatusStopped}
	if err := s.CreateOrUpdateXrayInstance(inst); err != nil {
		t.Fatalf("CreateOrUpdateXrayInstance() error = %v", err)
	}
	if err := s.DeleteXrayInstance(3); err != nil {
		t.Fatalf("DeleteXrayInstance() error = %v", err)
	}
	if _, err := s.GetXrayInstanceByUserID(3); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after delete, GetXrayInstanceByUserID() err = %v, want ErrNotFound", err)
	}

	// Delete is idempotent: deleting a missing user must not error.
	if err := s.DeleteXrayInstance(3); err != nil {
		t.Fatalf("DeleteXrayInstance() second call error = %v, want nil (idempotent)", err)
	}
}

// TestXrayInstance_ListAll verifies listing returns all rows ordered by user_id.
func TestXrayInstance_ListAll(t *testing.T) {
	s := newUserXrayTestStore(t)

	for _, uid := range []int64{9, 2, 5} {
		inst := &XrayInstance{
			UserID:     uid,
			Port:       20000 + int(uid),
			ConfigPath: "x",
			Status:     XrayStatusStopped,
		}
		if err := s.CreateOrUpdateXrayInstance(inst); err != nil {
			t.Fatalf("CreateOrUpdateXrayInstance(uid=%d) error = %v", uid, err)
		}
	}

	list, err := s.ListXrayInstances()
	if err != nil {
		t.Fatalf("ListXrayInstances() error = %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("len(ListXrayInstances()) = %d, want 3", len(list))
	}
	// Ordered by user_id ascending.
	wantOrder := []int64{2, 5, 9}
	for i, want := range wantOrder {
		if list[i].UserID != want {
			t.Errorf("list[%d].UserID = %d, want %d", i, list[i].UserID, want)
		}
	}
}

// TestXrayInstance_ListUsedPorts verifies the port-occupancy query used by the
// allocator: returns every port currently held by any user instance.
func TestXrayInstance_ListUsedPorts(t *testing.T) {
	s := newUserXrayTestStore(t)

	for i, uid := range []int64{1, 2, 3} {
		inst := &XrayInstance{
			UserID:     uid,
			Port:       30000 + i,
			ConfigPath: "x",
			Status:     XrayStatusStopped,
		}
		if err := s.CreateOrUpdateXrayInstance(inst); err != nil {
			t.Fatalf("CreateOrUpdateXrayInstance() error = %v", err)
		}
	}

	ports, err := s.ListUsedXrayPorts()
	if err != nil {
		t.Fatalf("ListUsedXrayPorts() error = %v", err)
	}
	if len(ports) != 3 {
		t.Fatalf("len(ListUsedXrayPorts()) = %d, want 3", len(ports))
	}
	want := map[int]bool{30000: true, 30001: true, 30002: true}
	for _, p := range ports {
		if !want[p] {
			t.Errorf("unexpected port %d in result", p)
		}
		delete(want, p)
	}
	if len(want) != 0 {
		t.Errorf("missing ports: %v", want)
	}
}
