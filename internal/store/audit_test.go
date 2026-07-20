package store

import (
	"testing"
	"time"
)

func TestAuditLogs_RecordAndList(t *testing.T) {
	st := newTestStore(t)

	if err := st.RecordAuditEvent("login_success", "1.1.1.1", "admin", ""); err != nil {
		t.Fatalf("RecordAuditEvent() error = %v", err)
	}
	if err := st.RecordAuditEvent("login_failure", "2.2.2.2", "attacker", ""); err != nil {
		t.Fatalf("RecordAuditEvent() error = %v", err)
	}

	events, total, err := st.ListAuditEvents(AuditFilter{}, 50, 0)
	if err != nil {
		t.Fatalf("ListAuditEvents() error = %v", err)
	}
	if total != 2 {
		t.Fatalf("total = %d, want 2", total)
	}
	if len(events) != 2 {
		t.Fatalf("len(events) = %d, want 2", len(events))
	}
	// 倒序：最新在前
	if events[0].EventType != "login_failure" || events[1].EventType != "login_success" {
		t.Errorf("order wrong: got %v, %v", events[0].EventType, events[1].EventType)
	}
}

func TestAuditLogs_FilterByEventType(t *testing.T) {
	st := newTestStore(t)
	st.RecordAuditEvent("login_success", "1.1.1.1", "admin", "")
	st.RecordAuditEvent("login_failure", "2.2.2.2", "bad", "")
	st.RecordAuditEvent("honeypot_ban", "3.3.3.3", "root", "banned")

	events, total, _ := st.ListAuditEvents(AuditFilter{EventTypes: []string{"login_failure", "honeypot_ban"}}, 50, 0)
	if total != 2 {
		t.Fatalf("filtered total = %d, want 2", total)
	}
	for _, e := range events {
		if e.EventType == "login_success" {
			t.Error("login_success should be filtered out")
		}
	}
}

func TestAuditLogs_FilterByIP(t *testing.T) {
	st := newTestStore(t)
	st.RecordAuditEvent("login_failure", "1.1.1.1", "a", "")
	st.RecordAuditEvent("login_failure", "2.2.2.2", "b", "")

	events, total, _ := st.ListAuditEvents(AuditFilter{IP: "1.1.1.1"}, 50, 0)
	if total != 1 || events[0].IP != "1.1.1.1" {
		t.Errorf("IP filter failed: total=%d, ip=%s", total, events[0].IP)
	}
}

func TestAuditLogs_FilterByTimeRange(t *testing.T) {
	st := newTestStore(t)
	// 手动插入一条明确的旧记录 + 一条新记录（CURRENT_TIMESTAMP）
	st.db.Exec(`INSERT INTO audit_logs (event_type, ip, username, created_at) VALUES (?, ?, ?, ?)`,
		"old", "1.1.1.1", "u", "2020-01-01 00:00:00")
	st.RecordAuditEvent("new", "2.2.2.2", "u", "")

	// Since 2021 → 只应看到 new
	_, total, _ := st.ListAuditEvents(AuditFilter{Since: time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)}, 50, 0)
	if total != 1 {
		t.Errorf("since 2021: total=%d, want 1 (only new)", total)
	}

	// Since 2019 → 两条都应看到
	_, total, _ = st.ListAuditEvents(AuditFilter{Since: time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC)}, 50, 0)
	if total != 2 {
		t.Errorf("since 2019: total=%d, want 2 (both)", total)
	}
}

func TestAuditLogs_Pagination(t *testing.T) {
	st := newTestStore(t)
	for i := 0; i < 5; i++ {
		st.RecordAuditEvent("login_failure", "1.1.1.1", "a", "")
	}

	events, total, _ := st.ListAuditEvents(AuditFilter{}, 2, 0)
	if total != 5 || len(events) != 2 {
		t.Fatalf("page 1: total=%d len=%d, want 5 / 2", total, len(events))
	}
	events, total, _ = st.ListAuditEvents(AuditFilter{}, 2, 2)
	if total != 5 || len(events) != 2 {
		t.Fatalf("page 2: total=%d len=%d, want 5 / 2", total, len(events))
	}
}

func TestAuditLogs_Prune(t *testing.T) {
	st := newTestStore(t)

	// 插入两条：一条明确是"旧"的（用手动时间），一条是"新"的（CURRENT_TIMESTAMP）
	st.db.Exec(`INSERT INTO audit_logs (event_type, ip, username, created_at) VALUES (?, ?, ?, ?)`,
		"old", "1.1.1.1", "u", "2020-01-01T00:00:00Z")
	st.RecordAuditEvent("new", "2.2.2.2", "u", "")

	// 删除 2021 年之前的，应该只删 old
	cutoff := time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := st.PruneAuditLogs(cutoff); err != nil {
		t.Fatalf("PruneAuditLogs() error = %v", err)
	}

	events, total, _ := st.ListAuditEvents(AuditFilter{}, 50, 0)
	if total != 1 {
		t.Fatalf("after prune: total=%d, want 1", total)
	}
	if events[0].EventType != "new" {
		t.Errorf("remaining event type=%s, want 'new'", events[0].EventType)
	}
}

func TestBannedIPs_List(t *testing.T) {
	st := newTestStore(t)
	now := time.Now()
	st.BanIP("1.1.1.1", 1*time.Hour, now)
	st.RecordLoginFailure("2.2.2.2", 5, 1*time.Hour, now) // 未达阈值，fail_count 记录但不封

	banned, err := st.ListBannedIPs()
	if err != nil {
		t.Fatalf("ListBannedIPs() error = %v", err)
	}
	if len(banned) != 2 {
		t.Fatalf("len(banned) = %d, want 2 (1 封禁 + 1 失败计数)", len(banned))
	}
	// 应有 1.1.1.1 的 banned_until 在未来
	found := false
	for _, b := range banned {
		if b.IP == "1.1.1.1" && !b.BannedUntil.IsZero() && b.BannedUntil.After(now) {
			found = true
		}
	}
	if !found {
		t.Error("1.1.1.1 should be banned with future banned_until")
	}
}

func TestBannedIPs_Unban(t *testing.T) {
	st := newTestStore(t)
	st.BanIP("1.1.1.1", 1*time.Hour, time.Now())

	if err := st.UnbanIP("1.1.1.1"); err != nil {
		t.Fatalf("UnbanIP() error = %v", err)
	}

	banned, _ := st.ListBannedIPs()
	for _, b := range banned {
		if b.IP == "1.1.1.1" && !b.BannedUntil.IsZero() {
			t.Error("1.1.1.1 should be unbanned (banned_until cleared)")
		}
	}
}
