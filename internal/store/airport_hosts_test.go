package store

import "testing"

// issue #116:机场 hosts 保真的存储缝——迁移补列、覆盖更新(含清空)、按来源名读取。

func TestAirportHosts_RoundTrip(t *testing.T) {
	s := newTestStore(t)

	a, err := s.CreateAirportForUser(1, "机场A", "https://example.com/sub")
	if err != nil {
		t.Fatalf("CreateAirportForUser() error = %v", err)
	}

	// 初始无 hosts
	got, err := s.GetAirportByID(a.ID)
	if err != nil {
		t.Fatalf("GetAirportByID() error = %v", err)
	}
	if len(got.Hosts) != 0 {
		t.Errorf("initial Hosts = %v, want empty", got.Hosts)
	}

	// 覆盖更新(拉取路径)
	hosts := map[string]string{"poisoned.example.com": "alias.example.com"}
	if err := s.UpdateAirportHosts(a.ID, hosts); err != nil {
		t.Fatalf("UpdateAirportHosts() error = %v", err)
	}
	got, _ = s.GetAirportByID(a.ID)
	if got.Hosts["poisoned.example.com"] != "alias.example.com" {
		t.Errorf("Hosts = %v, want captured", got.Hosts)
	}

	// 覆盖为空 = 显式清空(上游不再声明 hosts 时不残留旧映射)
	if err := s.UpdateAirportHosts(a.ID, nil); err != nil {
		t.Fatalf("UpdateAirportHosts(nil) error = %v", err)
	}
	got, _ = s.GetAirportByID(a.ID)
	if len(got.Hosts) != 0 {
		t.Errorf("Hosts after clear = %v, want empty", got.Hosts)
	}
}

func TestGetAirportsHostsForUser(t *testing.T) {
	s := newTestStore(t)

	a1, _ := s.CreateAirportForUser(1, "机场A", "https://example.com/1")
	a2, _ := s.CreateAirportForUser(1, "机场B", "https://example.com/2")
	other, _ := s.CreateAirportForUser(2, "别人的机场", "https://example.com/3")
	_ = s.UpdateAirportHosts(a1.ID, map[string]string{"a.example.com": "192.0.2.1"})
	_ = s.UpdateAirportHosts(a2.ID, map[string]string{"b.example.com": "192.0.2.2"})
	_ = s.UpdateAirportHosts(other.ID, map[string]string{"x.example.com": "192.0.2.3"})

	got, err := s.GetAirportsHostsForUser(1, []string{"机场A", "机场B", "不存在的机场"})
	if err != nil {
		t.Fatalf("GetAirportsHostsForUser() error = %v", err)
	}
	if got["机场A"]["a.example.com"] != "192.0.2.1" || got["机场B"]["b.example.com"] != "192.0.2.2" {
		t.Errorf("hosts = %v, want per-airport maps", got)
	}
	if _, ok := got["不存在的机场"]; ok {
		t.Error("unknown airport name should not appear in result")
	}

	// 用户隔离:user 2 查 user 1 的机场名拿不到
	got2, err := s.GetAirportsHostsForUser(2, []string{"机场A"})
	if err != nil {
		t.Fatalf("GetAirportsHostsForUser(2) error = %v", err)
	}
	if len(got2) != 0 {
		t.Errorf("cross-user hosts = %v, want empty", got2)
	}
}
