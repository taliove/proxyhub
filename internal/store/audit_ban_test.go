package store

import (
	"testing"
	"time"
)

// TestBanIP_WithDuration tests manual IP banning with various durations
func TestBanIP_WithDuration(t *testing.T) {
	st := newTestStore(t)
	now := time.Now()

	// Test 1 hour ban
	until, err := st.BanIP("1.1.1.1", 1*time.Hour, now)
	if err != nil {
		t.Fatalf("BanIP(1h) error = %v", err)
	}
	expectedUntil := now.Add(1 * time.Hour)
	if until.Sub(expectedUntil) > 1*time.Second {
		t.Errorf("banned_until = %v, want ~%v", until, expectedUntil)
	}

	// Test 24 hour ban
	until, err = st.BanIP("2.2.2.2", 24*time.Hour, now)
	if err != nil {
		t.Fatalf("BanIP(24h) error = %v", err)
	}
	expectedUntil = now.Add(24 * time.Hour)
	if until.Sub(expectedUntil) > 1*time.Second {
		t.Errorf("banned_until = %v, want ~%v", until, expectedUntil)
	}

	// Test permanent ban (100 years as convention)
	until, err = st.BanIP("3.3.3.3", 100*365*24*time.Hour, now)
	if err != nil {
		t.Fatalf("BanIP(permanent) error = %v", err)
	}
	expectedUntil = now.Add(100 * 365 * 24 * time.Hour)
	if until.Sub(expectedUntil) > 1*time.Second {
		t.Errorf("banned_until = %v, want ~%v", until, expectedUntil)
	}

	// Verify all three are in the list
	banned, err := st.ListBannedIPs()
	if err != nil {
		t.Fatalf("ListBannedIPs() error = %v", err)
	}
	if len(banned) != 3 {
		t.Fatalf("len(banned) = %d, want 3", len(banned))
	}
}

// TestBanIP_UpdateExisting tests that re-banning updates the banned_until time
func TestBanIP_UpdateExisting(t *testing.T) {
	st := newTestStore(t)
	now := time.Now()

	// Initial ban for 1 hour
	until1, err := st.BanIP("1.1.1.1", 1*time.Hour, now)
	if err != nil {
		t.Fatalf("BanIP() first error = %v", err)
	}

	// Re-ban for 24 hours (should update)
	later := now.Add(30 * time.Minute)
	until2, err := st.BanIP("1.1.1.1", 24*time.Hour, later)
	if err != nil {
		t.Fatalf("BanIP() second error = %v", err)
	}

	// Second ban should extend the duration
	if !until2.After(until1) {
		t.Errorf("second ban until = %v should be after first = %v", until2, until1)
	}

	// Should still have only one record
	banned, _ := st.ListBannedIPs()
	count := 0
	for _, b := range banned {
		if b.IP == "1.1.1.1" {
			count++
			if b.BannedUntil.Sub(until2) > 1*time.Second {
				t.Errorf("banned_until = %v, want ~%v", b.BannedUntil, until2)
			}
		}
	}
	if count != 1 {
		t.Errorf("count of 1.1.1.1 = %d, want 1", count)
	}
}
