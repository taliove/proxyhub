package store

import "testing"

func TestGlobalStats(t *testing.T) {
	st := newTestStore(t)
	ep1, _ := st.CreateEndpoint("dev")
	ep2, _ := st.CreateEndpoint("prod")

	// ep1: 2 次拉取（2 个不同 IP），ep2: 1 次
	st.RecordPull(PullRecord{EndpointID: ep1.ID, IP: "1.1.1.1", UserAgent: "clash"})
	st.RecordPull(PullRecord{EndpointID: ep1.ID, IP: "2.2.2.2", UserAgent: "clash"})
	st.RecordPull(PullRecord{EndpointID: ep2.ID, IP: "1.1.1.1", UserAgent: "v2ray"})

	total, uniqueIPs, activeEndpoints, err := st.GlobalStats()
	if err != nil {
		t.Fatalf("GlobalStats() error = %v", err)
	}
	if total != 3 {
		t.Errorf("total pulls = %d, want 3", total)
	}
	if uniqueIPs != 2 {
		t.Errorf("unique IPs = %d, want 2 (1.1.1.1, 2.2.2.2)", uniqueIPs)
	}
	if activeEndpoints != 2 {
		t.Errorf("active endpoints = %d, want 2", activeEndpoints)
	}
}

func TestGlobalStats_Empty(t *testing.T) {
	st := newTestStore(t)
	total, uniqueIPs, active, err := st.GlobalStats()
	if err != nil {
		t.Fatalf("GlobalStats() on empty error = %v", err)
	}
	if total != 0 || uniqueIPs != 0 || active != 0 {
		t.Errorf("empty stats = %d/%d/%d, want 0/0/0", total, uniqueIPs, active)
	}
}

func TestPullTrend(t *testing.T) {
	st := newTestStore(t)
	ep, _ := st.CreateEndpoint("dev")

	st.RecordPull(PullRecord{EndpointID: ep.ID, IP: "1.1.1.1", UserAgent: "clash"})
	st.RecordPull(PullRecord{EndpointID: ep.ID, IP: "2.2.2.2", UserAgent: "clash"})

	trend, err := st.PullTrend(7)
	if err != nil {
		t.Fatalf("PullTrend() error = %v", err)
	}
	// 今天该订阅地址应有 2 次拉取
	if len(trend) == 0 {
		t.Fatal("PullTrend() returned no points, want at least 1")
	}
	var found bool
	for _, p := range trend {
		if p.EndpointID == ep.ID && p.Count == 2 {
			found = true
			if p.Alias != "dev" {
				t.Errorf("trend alias = %q, want dev", p.Alias)
			}
		}
	}
	if !found {
		t.Errorf("expected trend point with endpoint=%d count=2, got %+v", ep.ID, trend)
	}
}

func TestPullTrend_Empty(t *testing.T) {
	st := newTestStore(t)
	trend, err := st.PullTrend(7)
	if err != nil {
		t.Fatalf("PullTrend() on empty error = %v", err)
	}
	if len(trend) != 0 {
		t.Errorf("empty trend = %d points, want 0", len(trend))
	}
}
