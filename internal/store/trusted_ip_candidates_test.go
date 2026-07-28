package store

import (
	"testing"
	"time"
)

func TestListRecentMFALoginIPs_DistinctMostRecentFirst(t *testing.T) {
	s := newTestStore(t)
	now := time.Now().UTC()

	// Two qualifying addresses, the second one seen more recently.
	seedAudit(t, s, "login_success", "203.0.113.10", "alice", "mfa=totp", now.Add(-72*time.Hour))
	seedAudit(t, s, "login_success", "203.0.113.10", "alice", "mfa=recovery", now.Add(-48*time.Hour))
	seedAudit(t, s, "login_success", "203.0.113.11", "alice", "mfa=totp", now.Add(-1*time.Hour))

	// Ignored: no mfa marker, trusted-IP skip, wrong event type, other user,
	// outside the 30 day window.
	seedAudit(t, s, "login_success", "203.0.113.20", "alice", "", now.Add(-2*time.Hour))
	seedAudit(t, s, "login_success", "203.0.113.21", "alice", "mfa_skipped=trusted_ip", now.Add(-2*time.Hour))
	seedAudit(t, s, "login_failure", "203.0.113.22", "alice", "mfa=totp", now.Add(-2*time.Hour))
	seedAudit(t, s, "login_success", "203.0.113.23", "bob", "mfa=totp", now.Add(-2*time.Hour))
	seedAudit(t, s, "login_success", "203.0.113.24", "alice", "mfa=totp", now.Add(-31*24*time.Hour))

	got, err := s.ListRecentMFALoginIPs("alice")
	if err != nil {
		t.Fatalf("ListRecentMFALoginIPs: %v", err)
	}
	want := []string{"203.0.113.11", "203.0.113.10"}
	if len(got) != len(want) {
		t.Fatalf("ListRecentMFALoginIPs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("ips[%d] = %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}
}

func TestListRecentMFALoginIPs_UnknownUserIsEmpty(t *testing.T) {
	s := newTestStore(t)
	seedAudit(t, s, "login_success", "203.0.113.10", "alice", "mfa=totp", time.Now().UTC())

	got, err := s.ListRecentMFALoginIPs("carol")
	if err != nil {
		t.Fatalf("ListRecentMFALoginIPs(carol): %v", err)
	}
	if len(got) != 0 {
		t.Errorf("ListRecentMFALoginIPs(carol) = %v, want empty", got)
	}
}

func TestListRecentMFALoginIPs_BlankUsername(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.ListRecentMFALoginIPs("  "); err == nil {
		t.Error("blank username: expected error, got nil")
	}
}
