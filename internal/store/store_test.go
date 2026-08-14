package store

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := OpenForTesting(dbPath)
	if err != nil {
		t.Fatalf("OpenForTesting() error = %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestCreateEndpoint(t *testing.T) {
	s := newTestStore(t)

	ep, err := s.CreateEndpoint("老爸的手机")
	if err != nil {
		t.Fatalf("CreateEndpoint() error = %v", err)
	}

	if ep.Alias != "老爸的手机" {
		t.Errorf("Alias = %s, want 老爸的手机", ep.Alias)
	}
	if len(ep.Path) != 16 {
		t.Errorf("len(Path) = %d, want 16", len(ep.Path))
	}
	if len(ep.Token) != 32 {
		t.Errorf("len(Token) = %d, want 32", len(ep.Token))
	}
	if !ep.Enabled {
		t.Error("Enabled = false, want true")
	}
}

func TestCreateEndpoint_EmptyAlias(t *testing.T) {
	s := newTestStore(t)

	if _, err := s.CreateEndpoint(""); err == nil {
		t.Error("CreateEndpoint(\"\") expected error, got nil")
	}
}

func TestCreateEndpoint_UniquePaths(t *testing.T) {
	s := newTestStore(t)

	ep1, _ := s.CreateEndpoint("A")
	ep2, _ := s.CreateEndpoint("B")

	if ep1.Path == ep2.Path {
		t.Error("two endpoints share the same path")
	}
	if ep1.Token == ep2.Token {
		t.Error("two endpoints share the same token")
	}
}

func TestGetEndpointByPath(t *testing.T) {
	s := newTestStore(t)

	created, _ := s.CreateEndpoint("测试")
	got, err := s.GetEndpointByPath(created.Path)
	if err != nil {
		t.Fatalf("GetEndpointByPath() error = %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("ID = %d, want %d", got.ID, created.ID)
	}
}

func TestGetEndpointByPath_NotFound(t *testing.T) {
	s := newTestStore(t)

	_, err := s.GetEndpointByPath("nonexistent")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestListEndpoints(t *testing.T) {
	s := newTestStore(t)

	s.CreateEndpoint("A")
	s.CreateEndpoint("B")

	eps, err := s.ListEndpoints()
	if err != nil {
		t.Fatalf("ListEndpoints() error = %v", err)
	}
	if len(eps) != 2 {
		t.Errorf("len = %d, want 2", len(eps))
	}
}

func TestSetEndpointEnabled(t *testing.T) {
	s := newTestStore(t)

	ep, _ := s.CreateEndpoint("测试")
	if err := s.SetEndpointEnabled(ep.ID, false); err != nil {
		t.Fatalf("SetEndpointEnabled() error = %v", err)
	}

	got, _ := s.GetEndpointByID(ep.ID)
	if got.Enabled {
		t.Error("Enabled = true, want false")
	}
}

func TestSetEndpointEnabled_NotFound(t *testing.T) {
	s := newTestStore(t)

	err := s.SetEndpointEnabled(999, false)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestDeleteEndpoint(t *testing.T) {
	s := newTestStore(t)

	ep, _ := s.CreateEndpoint("测试")
	if err := s.DeleteEndpoint(ep.ID); err != nil {
		t.Fatalf("DeleteEndpoint() error = %v", err)
	}

	_, err := s.GetEndpointByID(ep.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestRecordPullAndStats(t *testing.T) {
	s := newTestStore(t)

	ep, _ := s.CreateEndpoint("统计测试")

	for i := 0; i < 3; i++ {
		if err := s.RecordPull(PullRecord{
			EndpointID: ep.ID, IP: "1.2.3.4", UserAgent: "ClashX",
		}); err != nil {
			t.Fatalf("RecordPull() error = %v", err)
		}
	}
	s.RecordPull(PullRecord{EndpointID: ep.ID, IP: "5.6.7.8", UserAgent: "v2rayN"})

	// 保存一个 IP 的地理信息
	s.SaveGeo(GeoInfo{IP: "1.2.3.4", Country: "中国", Region: "广东", City: "深圳", ISP: "电信"})

	stats, err := s.EndpointStats(ep.ID)
	if err != nil {
		t.Fatalf("EndpointStats() error = %v", err)
	}
	if len(stats) != 2 {
		t.Fatalf("len(stats) = %d, want 2", len(stats))
	}

	// 按次数降序，1.2.3.4 在前
	if stats[0].IP != "1.2.3.4" || stats[0].Count != 3 {
		t.Errorf("stats[0] = %s/%d, want 1.2.3.4/3", stats[0].IP, stats[0].Count)
	}
	if stats[0].City != "深圳" {
		t.Errorf("City = %s, want 深圳", stats[0].City)
	}
	// 未解析的 IP 地理字段为空
	if stats[1].Country != "" {
		t.Errorf("stats[1].Country = %s, want empty", stats[1].Country)
	}
}

func TestRecordPull_EmptyIP(t *testing.T) {
	s := newTestStore(t)
	if err := s.RecordPull(PullRecord{EndpointID: 1, IP: ""}); err == nil {
		t.Error("RecordPull with empty IP expected error, got nil")
	}
}

func TestGeoCache(t *testing.T) {
	s := newTestStore(t)

	_, err := s.GetGeo("9.9.9.9")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}

	s.SaveGeo(GeoInfo{IP: "9.9.9.9", Country: "美国"})
	geo, err := s.GetGeo("9.9.9.9")
	if err != nil {
		t.Fatalf("GetGeo() error = %v", err)
	}
	if geo.Country != "美国" {
		t.Errorf("Country = %s, want 美国", geo.Country)
	}
}

func TestUnresolvedIPs(t *testing.T) {
	s := newTestStore(t)

	ep, _ := s.CreateEndpoint("测试")
	s.RecordPull(PullRecord{EndpointID: ep.ID, IP: "1.1.1.1"})
	s.RecordPull(PullRecord{EndpointID: ep.ID, IP: "2.2.2.2"})
	s.SaveGeo(GeoInfo{IP: "1.1.1.1", Country: "US"})

	ips, err := s.UnresolvedIPs(10)
	if err != nil {
		t.Fatalf("UnresolvedIPs() error = %v", err)
	}
	if len(ips) != 1 || ips[0] != "2.2.2.2" {
		t.Errorf("ips = %v, want [2.2.2.2]", ips)
	}
}

func TestIP2Ban(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()

	// 前 2 次失败不触发封禁（阈值 3）
	for i := 0; i < 2; i++ {
		banned, err := s.RecordLoginFailure("10.0.0.1", 3, time.Hour, now)
		if err != nil {
			t.Fatalf("RecordLoginFailure() error = %v", err)
		}
		if banned {
			t.Errorf("failure %d triggered ban, want no ban", i+1)
		}
	}

	// 第 3 次触发封禁
	banned, err := s.RecordLoginFailure("10.0.0.1", 3, time.Hour, now)
	if err != nil {
		t.Fatalf("RecordLoginFailure() error = %v", err)
	}
	if !banned {
		t.Error("3rd failure did not trigger ban")
	}

	// 封禁期内
	isBanned, err := s.IsBanned("10.0.0.1", now.Add(30*time.Minute))
	if err != nil {
		t.Fatalf("IsBanned() error = %v", err)
	}
	if !isBanned {
		t.Error("IsBanned = false during ban period, want true")
	}

	// 封禁过期
	isBanned, _ = s.IsBanned("10.0.0.1", now.Add(2*time.Hour))
	if isBanned {
		t.Error("IsBanned = true after ban expired, want false")
	}
}

func TestResetLoginFailures(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()

	s.RecordLoginFailure("10.0.0.2", 3, time.Hour, now)
	s.RecordLoginFailure("10.0.0.2", 3, time.Hour, now)

	if err := s.ResetLoginFailures("10.0.0.2"); err != nil {
		t.Fatalf("ResetLoginFailures() error = %v", err)
	}

	// 重置后重新计数，2 次失败不应触发封禁
	banned, _ := s.RecordLoginFailure("10.0.0.2", 3, time.Hour, now)
	if banned {
		t.Error("ban triggered after reset, want no ban")
	}
}

func TestSettings(t *testing.T) {
	s := newTestStore(t)

	_, err := s.GetSetting("admin_path")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}

	if err := s.SetSetting("admin_path", "abc123"); err != nil {
		t.Fatalf("SetSetting() error = %v", err)
	}

	val, err := s.GetSetting("admin_path")
	if err != nil {
		t.Fatalf("GetSetting() error = %v", err)
	}
	if val != "abc123" {
		t.Errorf("value = %s, want abc123", val)
	}

	// 覆盖更新
	s.SetSetting("admin_path", "xyz789")
	val, _ = s.GetSetting("admin_path")
	if val != "xyz789" {
		t.Errorf("value = %s, want xyz789", val)
	}
}
