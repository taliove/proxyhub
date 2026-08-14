package store

import (
	"path/filepath"
	"testing"
)

// TestMigratePullLogStatus_SchemaInPlace verifies a freshly opened database
// carries the status column and its lookup index.
func TestMigratePullLogStatus_SchemaInPlace(t *testing.T) {
	s := newTestStore(t)

	if !s.columnExistsUnlocked("pull_logs", "status") {
		t.Error("pull_logs.status column missing after migrate()")
	}
	if !indexExists(t, s, "idx_pull_logs_endpoint_status") {
		t.Error("idx_pull_logs_endpoint_status index missing after migrate()")
	}
}

// TestMigratePullLogStatus_Idempotent verifies repeated migration runs neither
// fail nor rewrite existing rows, both in place and across a reopen.
func TestMigratePullLogStatus_Idempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := OpenForTesting(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	ep, err := s.CreateEndpoint("idempotent")
	if err != nil {
		t.Fatalf("CreateEndpoint: %v", err)
	}
	if err := s.RecordPull(PullRecord{
		EndpointID: ep.ID, IP: "203.0.113.9", Status: PullStatusBadToken,
	}); err != nil {
		t.Fatalf("RecordPull: %v", err)
	}

	// Re-running the migration in place must be a no-op.
	if err := s.migratePullLogStatus(); err != nil {
		t.Fatalf("second migratePullLogStatus: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Reopening runs migrate() again over a populated database.
	s2, err := OpenForTesting(dbPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()

	stats, err := s2.EndpointStats(ep.ID)
	if err != nil {
		t.Fatalf("EndpointStats after reopen: %v", err)
	}
	if len(stats) != 1 || stats[0].Status != PullStatusBadToken {
		t.Fatalf("status lost across migration re-run: %+v", stats)
	}
}

// TestMigratePullLogStatus_LegacyRowsDefaultOK verifies rows written before the
// status column existed read back as ok: until ticket 01 only delivered pulls
// were recorded.
func TestMigratePullLogStatus_LegacyRowsDefaultOK(t *testing.T) {
	s := newTestStore(t)
	ep, _ := s.CreateEndpoint("legacy")

	// Simulate a pre-migration insert by omitting the status column entirely.
	if _, err := s.db.Exec(
		`INSERT INTO pull_logs (endpoint_id, ip, user_agent) VALUES (?, ?, ?)`,
		ep.ID, "198.51.100.4", "ClashX",
	); err != nil {
		t.Fatalf("legacy insert: %v", err)
	}

	stats, err := s.EndpointStats(ep.ID)
	if err != nil {
		t.Fatalf("EndpointStats: %v", err)
	}
	if len(stats) != 1 || stats[0].Status != PullStatusOK {
		t.Fatalf("legacy row status = %+v, want ok", stats)
	}
}

// TestAggregateStats_IgnoreBlockedPulls pins the aggregate caliber: 汇总数字
// (总拉取/独立 IP/活跃订阅/趋势) 只算成功下发, 被拦尝试不得虚高统计
// (ADR 0028: 统计只反映真实客户端拉取)。
func TestAggregateStats_IgnoreBlockedPulls(t *testing.T) {
	s := newTestStore(t)
	ep, _ := s.CreateEndpoint("aggregate")

	s.RecordPull(PullRecord{EndpointID: ep.ID, IP: "1.1.1.1", Status: PullStatusOK})
	s.RecordPull(PullRecord{EndpointID: ep.ID, IP: "2.2.2.2", Status: PullStatusBadToken})
	s.RecordPull(PullRecord{EndpointID: ep.ID, IP: "3.3.3.3", Status: PullStatusDisabled})

	total, uniqueIPs, active, err := s.GlobalStats()
	if err != nil {
		t.Fatalf("GlobalStats: %v", err)
	}
	if total != 1 || uniqueIPs != 1 || active != 1 {
		t.Errorf("stats = %d/%d/%d, want 1/1/1 (blocked pulls excluded)", total, uniqueIPs, active)
	}

	trend, err := s.PullTrend(7)
	if err != nil {
		t.Fatalf("PullTrend: %v", err)
	}
	if len(trend) != 1 || trend[0].Count != 1 {
		t.Errorf("trend = %+v, want a single point with count 1", trend)
	}

	// 明细侧相反:三条记录都在,被拦尝试正是它的价值。
	stats, _ := s.EndpointStats(ep.ID)
	if len(stats) != 3 {
		t.Errorf("len(EndpointStats) = %d, want 3 (detail keeps blocked rows)", len(stats))
	}
}

func TestIsValidPullStatus(t *testing.T) {
	for _, st := range []string{
		PullStatusOK, PullStatusRateLimited, PullStatusGeoBlocked,
		PullStatusGeoWouldBlock, PullStatusBlacklisted, PullStatusDisabled,
		PullStatusBadToken,
	} {
		if !IsValidPullStatus(st) {
			t.Errorf("IsValidPullStatus(%q) = false, want true", st)
		}
	}
	for _, st := range []string{"", "OK", "banned", "geo"} {
		if IsValidPullStatus(st) {
			t.Errorf("IsValidPullStatus(%q) = true, want false", st)
		}
	}
}

// TestRecordPull_StatusDefaultsToOK keeps the pre-ticket call sites (which pass
// no status) recording delivered pulls.
func TestRecordPull_StatusDefaultsToOK(t *testing.T) {
	s := newTestStore(t)
	ep, _ := s.CreateEndpoint("default status")

	if err := s.RecordPull(PullRecord{EndpointID: ep.ID, IP: "1.2.3.4"}); err != nil {
		t.Fatalf("RecordPull: %v", err)
	}
	stats, _ := s.EndpointStats(ep.ID)
	if len(stats) != 1 || stats[0].Status != PullStatusOK {
		t.Fatalf("stats = %+v, want one ok row", stats)
	}
}

// TestRecordPull_RejectsUnknownStatus keeps the status set closed: a typo in a
// guard must fail at write time, not leak a value no reader understands.
func TestRecordPull_RejectsUnknownStatus(t *testing.T) {
	s := newTestStore(t)
	ep, _ := s.CreateEndpoint("bad status")

	if err := s.RecordPull(PullRecord{
		EndpointID: ep.ID, IP: "1.2.3.4", Status: "throttled",
	}); err == nil {
		t.Error("RecordPull with unknown status expected error, got nil")
	}
	stats, _ := s.EndpointStats(ep.ID)
	if len(stats) != 0 {
		t.Errorf("rejected pull must not be persisted, got %+v", stats)
	}
}

// TestEndpointStats_GroupsByIPAndStatus verifies the IP detail view splits one
// IP into per-status rows so blocked attempts are distinguishable from pulls
// that were actually served.
func TestEndpointStats_GroupsByIPAndStatus(t *testing.T) {
	s := newTestStore(t)
	ep, _ := s.CreateEndpoint("mixed")

	for i := 0; i < 3; i++ {
		s.RecordPull(PullRecord{EndpointID: ep.ID, IP: "1.2.3.4", Status: PullStatusOK})
	}
	s.RecordPull(PullRecord{EndpointID: ep.ID, IP: "1.2.3.4", Status: PullStatusBadToken})
	s.RecordPull(PullRecord{EndpointID: ep.ID, IP: "5.6.7.8", Status: PullStatusDisabled})
	s.SaveGeo(GeoInfo{IP: "1.2.3.4", Country: "中国", Region: "广东", City: "深圳", ISP: "电信"})

	stats, err := s.EndpointStats(ep.ID)
	if err != nil {
		t.Fatalf("EndpointStats: %v", err)
	}
	if len(stats) != 3 {
		t.Fatalf("len(stats) = %d, want 3 (ip x status rows): %+v", len(stats), stats)
	}

	// Ordered by count desc: the three ok pulls come first.
	if stats[0].IP != "1.2.3.4" || stats[0].Status != PullStatusOK || stats[0].Count != 3 {
		t.Errorf("stats[0] = %+v, want 1.2.3.4/ok/3", stats[0])
	}
	if stats[0].City != "深圳" {
		t.Errorf("geo join lost: %+v", stats[0])
	}

	byStatus := map[string]*IPStat{}
	for _, st := range stats {
		byStatus[st.Status] = st
	}
	if got := byStatus[PullStatusBadToken]; got == nil || got.IP != "1.2.3.4" || got.Count != 1 {
		t.Errorf("bad_token row = %+v, want 1.2.3.4/1", got)
	}
	if got := byStatus[PullStatusDisabled]; got == nil || got.IP != "5.6.7.8" || got.Count != 1 {
		t.Errorf("disabled row = %+v, want 5.6.7.8/1", got)
	}
	if byStatus[PullStatusOK].LastPull.IsZero() {
		t.Error("LastPull not parsed for the ok row")
	}
}
