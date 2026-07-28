package server

import (
	"testing"
	"time"
)

// TestChargeLoginFailure_SharedCounterAcrossStages pins the deduplicated
// accounting: password, captcha and MFA failures all charge one per-IP counter
// against one threshold, and only the attempt that crosses it reports banned
// and writes the threshold_ban audit row.
func TestChargeLoginFailure_SharedCounterAcrossStages(t *testing.T) {
	srv, st := newTestServer(t, nil)
	if err := st.SetSetting("ban_threshold", "3"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	policy := srv.loadSecurityPolicy()
	if policy.BanThreshold != 3 {
		t.Fatalf("ban_threshold = %d, want 3", policy.BanThreshold)
	}
	const ip = "198.51.100.7"

	// One failure per stage: three stages, one counter, threshold reached on
	// the third regardless of which stage produced it.
	stages := []failureReason{failureReasonPassword, failureReasonCaptcha, failureReasonMFA}
	for i, reason := range stages {
		banned := srv.chargeLoginFailure(ip, "owner", "", policy, reason)
		wantBanned := i == len(stages)-1
		if banned != wantBanned {
			t.Fatalf("charge #%d (%s) banned = %v, want %v", i+1, reason, banned, wantBanned)
		}
		// The store zeroes fail_count when it arms the ban, so only the
		// pre-threshold charges are observable on the counter.
		wantCount := i + 1
		if wantBanned {
			wantCount = 0
		}
		if got := failCountFor(t, st, ip); got != wantCount {
			t.Fatalf("fail_count after charge #%d = %d, want %d", i+1, got, wantCount)
		}
	}

	if got := auditEventCount(t, st, "threshold_ban"); got != 1 {
		t.Errorf("threshold_ban rows = %d, want exactly 1", got)
	}
	banned, err := st.IsBanned(ip, time.Now())
	if err != nil {
		t.Fatalf("IsBanned: %v", err)
	}
	if !banned {
		t.Error("IP not banned after the threshold was crossed")
	}
}

// TestChargeLoginFailure_BelowThresholdWritesNoBanAudit a failure short of the
// threshold only moves the counter: no ban, no threshold_ban row.
func TestChargeLoginFailure_BelowThresholdWritesNoBanAudit(t *testing.T) {
	srv, st := newTestServer(t, nil)
	policy := srv.loadSecurityPolicy() // default threshold 5
	const ip = "198.51.100.8"

	if srv.chargeLoginFailure(ip, "owner", "", policy, failureReasonPassword) {
		t.Fatal("chargeLoginFailure() = true on the first failure, want false")
	}
	if got := failCountFor(t, st, ip); got != 1 {
		t.Errorf("fail_count = %d, want 1", got)
	}
	if got := auditEventCount(t, st, "threshold_ban"); got != 0 {
		t.Errorf("threshold_ban rows = %d, want 0 below the threshold", got)
	}
}
