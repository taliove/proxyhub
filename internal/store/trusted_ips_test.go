package store

import (
	"testing"
	"time"
)

// mustUser creates a user for trusted-IP tests.
func mustUser(t *testing.T, s *Store, name string) *User {
	t.Helper()
	u, err := s.CreateUser(name, "$2a$10$hash", RoleUser, false)
	if err != nil {
		t.Fatalf("CreateUser(%q): %v", name, err)
	}
	return u
}

func TestAddTrustedIP_TrustsFor30Days(t *testing.T) {
	s := newTestStore(t)
	u := mustUser(t, s, "alice")

	if err := s.AddTrustedIP(u.ID, "203.0.113.10"); err != nil {
		t.Fatalf("AddTrustedIP: %v", err)
	}

	trusted, err := s.IsTrustedIP(u.ID, "203.0.113.10")
	if err != nil {
		t.Fatalf("IsTrustedIP: %v", err)
	}
	if !trusted {
		t.Error("IsTrustedIP = false, want true right after AddTrustedIP")
	}

	list, err := s.ListTrustedIPs(u.ID)
	if err != nil {
		t.Fatalf("ListTrustedIPs: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("ListTrustedIPs len = %d, want 1", len(list))
	}
	want := time.Now().UTC().Add(TrustedIPTTL)
	if diff := list[0].ExpiresAt.Sub(want); diff > time.Minute || diff < -time.Minute {
		t.Errorf("ExpiresAt = %v, want ~%v (30 days out)", list[0].ExpiresAt, want)
	}
	if list[0].LastUsedAt.IsZero() {
		t.Error("LastUsedAt is zero, want the creation time")
	}
}

func TestAddTrustedIP_Validation(t *testing.T) {
	s := newTestStore(t)
	u := mustUser(t, s, "alice")

	if err := s.AddTrustedIP(u.ID, "  "); err == nil {
		t.Error("AddTrustedIP with blank ip: expected error, got nil")
	}
	if err := s.AddTrustedIP(0, "203.0.113.10"); err == nil {
		t.Error("AddTrustedIP with zero user id: expected error, got nil")
	}
}

func TestAddTrustedIP_ReTrustExtendsExpiry(t *testing.T) {
	s := newTestStore(t)
	u := mustUser(t, s, "alice")

	now := time.Now().UTC()
	if err := s.addTrustedIPAt(u.ID, "203.0.113.10", now.Add(-20*24*time.Hour)); err != nil {
		t.Fatalf("addTrustedIPAt: %v", err)
	}
	if err := s.AddTrustedIP(u.ID, "203.0.113.10"); err != nil {
		t.Fatalf("AddTrustedIP (re-trust): %v", err)
	}

	list, err := s.ListTrustedIPs(u.ID)
	if err != nil {
		t.Fatalf("ListTrustedIPs: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("ListTrustedIPs len = %d, want 1 (upsert, not duplicate)", len(list))
	}
	if list[0].ExpiresAt.Before(now.Add(29 * 24 * time.Hour)) {
		t.Errorf("ExpiresAt = %v, want re-trust to reset the 30 day window", list[0].ExpiresAt)
	}
}

func TestIsTrustedIP_ExpiredNotTrusted(t *testing.T) {
	s := newTestStore(t)
	u := mustUser(t, s, "alice")

	// Trusted 31 days ago: the 30 day window has closed.
	if err := s.addTrustedIPAt(u.ID, "203.0.113.10", time.Now().UTC().Add(-31*24*time.Hour)); err != nil {
		t.Fatalf("addTrustedIPAt: %v", err)
	}

	trusted, err := s.IsTrustedIP(u.ID, "203.0.113.10")
	if err != nil {
		t.Fatalf("IsTrustedIP: %v", err)
	}
	if trusted {
		t.Error("IsTrustedIP = true for an expired entry, want false")
	}

	// The row is still listed (management UI shows it) but flagged expired.
	list, err := s.ListTrustedIPs(u.ID)
	if err != nil {
		t.Fatalf("ListTrustedIPs: %v", err)
	}
	if len(list) != 1 || !list[0].Expired() {
		t.Errorf("ListTrustedIPs = %+v, want one expired entry", list)
	}
}

func TestIsTrustedIP_UnknownIPAndUserIsolation(t *testing.T) {
	s := newTestStore(t)
	alice := mustUser(t, s, "alice")
	bob := mustUser(t, s, "bob")

	if err := s.AddTrustedIP(alice.ID, "203.0.113.10"); err != nil {
		t.Fatalf("AddTrustedIP: %v", err)
	}

	if trusted, _ := s.IsTrustedIP(alice.ID, "203.0.113.11"); trusted {
		t.Error("unknown IP reported as trusted")
	}
	if trusted, _ := s.IsTrustedIP(bob.ID, "203.0.113.10"); trusted {
		t.Error("trust leaked across users; must be per-user")
	}
}

func TestRevokeTrustedIP(t *testing.T) {
	s := newTestStore(t)
	u := mustUser(t, s, "alice")

	if err := s.AddTrustedIP(u.ID, "203.0.113.10"); err != nil {
		t.Fatalf("AddTrustedIP: %v", err)
	}
	if err := s.RevokeTrustedIP(u.ID, "203.0.113.10"); err != nil {
		t.Fatalf("RevokeTrustedIP: %v", err)
	}

	if trusted, _ := s.IsTrustedIP(u.ID, "203.0.113.10"); trusted {
		t.Error("IsTrustedIP = true after revoke, want false")
	}
	list, _ := s.ListTrustedIPs(u.ID)
	if len(list) != 0 {
		t.Errorf("ListTrustedIPs len = %d after revoke, want 0", len(list))
	}
	// Revoking an absent entry is not an error (idempotent).
	if err := s.RevokeTrustedIP(u.ID, "203.0.113.10"); err != nil {
		t.Errorf("second RevokeTrustedIP: %v, want nil (idempotent)", err)
	}
}

func TestRevokeAllTrustedIPs(t *testing.T) {
	s := newTestStore(t)
	alice := mustUser(t, s, "alice")
	bob := mustUser(t, s, "bob")

	for _, ip := range []string{"203.0.113.10", "203.0.113.11"} {
		if err := s.AddTrustedIP(alice.ID, ip); err != nil {
			t.Fatalf("AddTrustedIP: %v", err)
		}
	}
	if err := s.AddTrustedIP(bob.ID, "203.0.113.12"); err != nil {
		t.Fatalf("AddTrustedIP: %v", err)
	}

	n, err := s.RevokeAllTrustedIPs(alice.ID)
	if err != nil {
		t.Fatalf("RevokeAllTrustedIPs: %v", err)
	}
	if n != 2 {
		t.Errorf("RevokeAllTrustedIPs = %d, want 2", n)
	}
	if list, _ := s.ListTrustedIPs(alice.ID); len(list) != 0 {
		t.Errorf("alice still has %d trusted IPs", len(list))
	}
	if list, _ := s.ListTrustedIPs(bob.ID); len(list) != 1 {
		t.Errorf("bob's trusted IPs were collateral damage: %d, want 1", len(list))
	}
}

func TestTouchTrustedIP_NoWriteWithin24h(t *testing.T) {
	s := newTestStore(t)
	u := mustUser(t, s, "alice")

	created := time.Now().UTC().Add(-12 * time.Hour)
	if err := s.addTrustedIPAt(u.ID, "203.0.113.10", created); err != nil {
		t.Fatalf("addTrustedIPAt: %v", err)
	}
	before, _ := s.ListTrustedIPs(u.ID)

	renewed, err := s.TouchTrustedIP(u.ID, "203.0.113.10")
	if err != nil {
		t.Fatalf("TouchTrustedIP: %v", err)
	}
	if renewed {
		t.Error("TouchTrustedIP renewed within 24h, want no write")
	}

	after, _ := s.ListTrustedIPs(u.ID)
	if !after[0].LastUsedAt.Equal(before[0].LastUsedAt) {
		t.Errorf("LastUsedAt changed within 24h: %v -> %v", before[0].LastUsedAt, after[0].LastUsedAt)
	}
	if !after[0].ExpiresAt.Equal(before[0].ExpiresAt) {
		t.Errorf("ExpiresAt changed within 24h: %v -> %v", before[0].ExpiresAt, after[0].ExpiresAt)
	}
}

func TestTouchTrustedIP_RenewsAfter24h(t *testing.T) {
	s := newTestStore(t)
	u := mustUser(t, s, "alice")

	created := time.Now().UTC().Add(-25 * time.Hour)
	if err := s.addTrustedIPAt(u.ID, "203.0.113.10", created); err != nil {
		t.Fatalf("addTrustedIPAt: %v", err)
	}
	before, _ := s.ListTrustedIPs(u.ID)

	renewed, err := s.TouchTrustedIP(u.ID, "203.0.113.10")
	if err != nil {
		t.Fatalf("TouchTrustedIP: %v", err)
	}
	if !renewed {
		t.Error("TouchTrustedIP = false after 24h, want renewal")
	}

	after, _ := s.ListTrustedIPs(u.ID)
	if !after[0].LastUsedAt.After(before[0].LastUsedAt) {
		t.Errorf("LastUsedAt not advanced: %v -> %v", before[0].LastUsedAt, after[0].LastUsedAt)
	}
	if !after[0].ExpiresAt.After(before[0].ExpiresAt) {
		t.Errorf("ExpiresAt not extended: %v -> %v", before[0].ExpiresAt, after[0].ExpiresAt)
	}
}

func TestTouchTrustedIP_ExpiredOrMissingIsNoop(t *testing.T) {
	s := newTestStore(t)
	u := mustUser(t, s, "alice")

	if renewed, err := s.TouchTrustedIP(u.ID, "203.0.113.99"); err != nil || renewed {
		t.Errorf("TouchTrustedIP(missing) = %v, %v; want false, nil", renewed, err)
	}

	if err := s.addTrustedIPAt(u.ID, "203.0.113.10", time.Now().UTC().Add(-40*24*time.Hour)); err != nil {
		t.Fatalf("addTrustedIPAt: %v", err)
	}
	renewed, err := s.TouchTrustedIP(u.ID, "203.0.113.10")
	if err != nil {
		t.Fatalf("TouchTrustedIP: %v", err)
	}
	if renewed {
		t.Error("expired entry was renewed; expiry must not be resurrected by a touch")
	}
	if trusted, _ := s.IsTrustedIP(u.ID, "203.0.113.10"); trusted {
		t.Error("expired entry became trusted after touch")
	}
}

// seedAudit inserts an audit row with an explicit UTC created_at.
func seedAudit(t *testing.T, s *Store, eventType, ip, username, detail string, at time.Time) {
	t.Helper()
	_, err := s.db.Exec(
		`INSERT INTO audit_logs (event_type, ip, username, detail, created_at) VALUES (?, ?, ?, ?, ?)`,
		eventType, ip, username, detail, at.UTC().Format("2006-01-02 15:04:05"))
	if err != nil {
		t.Fatalf("seed audit: %v", err)
	}
}

func TestGetTrustRecommendationCount_CountsOnlyMFASuccesses(t *testing.T) {
	s := newTestStore(t)
	now := time.Now().UTC()

	// Counted: login_success carrying an mfa= marker inside the 30 day window.
	seedAudit(t, s, "login_success", "203.0.113.10", "alice", "mfa=totp", now.Add(-1*time.Hour))
	seedAudit(t, s, "login_success", "203.0.113.10", "alice", "mfa=recovery", now.Add(-48*time.Hour))
	seedAudit(t, s, "login_success", "203.0.113.10", "alice", "session=abc mfa=totp", now.Add(-10*24*time.Hour))

	// Not counted: no mfa marker / trusted-IP skip / other event type /
	// other IP / other user / outside the 30 day window.
	seedAudit(t, s, "login_success", "203.0.113.10", "alice", "", now.Add(-2*time.Hour))
	seedAudit(t, s, "login_success", "203.0.113.10", "alice", "mfa_skipped=trusted_ip", now.Add(-3*time.Hour))
	seedAudit(t, s, "login_failure", "203.0.113.10", "alice", "mfa=totp", now.Add(-4*time.Hour))
	seedAudit(t, s, "login_success", "203.0.113.11", "alice", "mfa=totp", now.Add(-5*time.Hour))
	seedAudit(t, s, "login_success", "203.0.113.10", "bob", "mfa=totp", now.Add(-6*time.Hour))
	seedAudit(t, s, "login_success", "203.0.113.10", "alice", "mfa=totp", now.Add(-31*24*time.Hour))

	got, err := s.GetTrustRecommendationCount("alice", "203.0.113.10")
	if err != nil {
		t.Fatalf("GetTrustRecommendationCount: %v", err)
	}
	if got != 3 {
		t.Errorf("GetTrustRecommendationCount = %d, want 3", got)
	}

	zero, err := s.GetTrustRecommendationCount("carol", "203.0.113.10")
	if err != nil {
		t.Fatalf("GetTrustRecommendationCount(carol): %v", err)
	}
	if zero != 0 {
		t.Errorf("GetTrustRecommendationCount(carol) = %d, want 0", zero)
	}
}

func TestGetTrustRecommendationCount_BlankArgs(t *testing.T) {
	s := newTestStore(t)

	if _, err := s.GetTrustRecommendationCount("", "203.0.113.10"); err == nil {
		t.Error("blank username: expected error, got nil")
	}
	if _, err := s.GetTrustRecommendationCount("alice", ""); err == nil {
		t.Error("blank ip: expected error, got nil")
	}
}
