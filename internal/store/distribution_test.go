package store

import (
	"testing"
	"time"
)

func TestGetDistributionConfig_Default(t *testing.T) {
	st := newTestStore(t)

	cfg, err := st.GetDistributionConfig()
	if err != nil {
		t.Fatalf("GetDistributionConfig() error = %v", err)
	}

	// 验证默认值
	if cfg.ID != 1 {
		t.Errorf("ID = %d, want 1", cfg.ID)
	}
	if cfg.Enabled {
		t.Errorf("Enabled = true, want false")
	}
	if cfg.ListenPort != 10808 {
		t.Errorf("ListenPort = %d, want 10808", cfg.ListenPort)
	}
	if cfg.Protocol != "vless" {
		t.Errorf("Protocol = %q, want vless", cfg.Protocol)
	}
	if cfg.Network != "tcp" {
		t.Errorf("Network = %q, want tcp", cfg.Network)
	}
}

func TestSaveDistributionConfig(t *testing.T) {
	st := newTestStore(t)

	newCfg := &DistributionConfig{
		Enabled:    true,
		ListenPort: 8443,
		Domain:     "proxy.example.com",
		Protocol:   "vmess",
		Network:    "ws",
		UUID:       "test-uuid-123",
		TLS:        true,
		CertPath:   "/path/to/cert.pem",
		KeyPath:    "/path/to/key.pem",
	}

	if err := st.SaveDistributionConfig(newCfg); err != nil {
		t.Fatalf("SaveDistributionConfig() error = %v", err)
	}

	// 读取验证
	got, err := st.GetDistributionConfig()
	if err != nil {
		t.Fatalf("GetDistributionConfig() error = %v", err)
	}

	if !got.Enabled {
		t.Errorf("Enabled = false, want true")
	}
	if got.ListenPort != 8443 {
		t.Errorf("ListenPort = %d, want 8443", got.ListenPort)
	}
	if got.Domain != "proxy.example.com" {
		t.Errorf("Domain = %q, want proxy.example.com", got.Domain)
	}
	if got.Protocol != "vmess" {
		t.Errorf("Protocol = %q, want vmess", got.Protocol)
	}
	if got.Network != "ws" {
		t.Errorf("Network = %q, want ws", got.Network)
	}
	if got.UUID != "test-uuid-123" {
		t.Errorf("UUID = %q, want test-uuid-123", got.UUID)
	}
	if !got.TLS {
		t.Errorf("TLS = false, want true")
	}
	if got.CertPath != "/path/to/cert.pem" {
		t.Errorf("CertPath = %q", got.CertPath)
	}
	if got.KeyPath != "/path/to/key.pem" {
		t.Errorf("KeyPath = %q", got.KeyPath)
	}
}

func TestSaveDistributionConfig_Validation(t *testing.T) {
	st := newTestStore(t)

	// 无效端口
	err := st.SaveDistributionConfig(&DistributionConfig{
		ListenPort: 0,
		Protocol:   "vless",
	})
	if err == nil {
		t.Error("SaveDistributionConfig with port 0 expected error, got nil")
	}

	err = st.SaveDistributionConfig(&DistributionConfig{
		ListenPort: 70000,
		Protocol:   "vless",
	})
	if err == nil {
		t.Error("SaveDistributionConfig with port 70000 expected error, got nil")
	}

	// 空协议
	err = st.SaveDistributionConfig(&DistributionConfig{
		ListenPort: 8080,
		Protocol:   "",
	})
	if err == nil {
		t.Error("SaveDistributionConfig with empty protocol expected error, got nil")
	}
}

func TestSaveDistributionConfig_Upsert(t *testing.T) {
	st := newTestStore(t)

	// 第一次保存
	cfg1 := &DistributionConfig{
		Enabled:    true,
		ListenPort: 8080,
		Protocol:   "vless",
	}
	if err := st.SaveDistributionConfig(cfg1); err != nil {
		t.Fatalf("SaveDistributionConfig() first error = %v", err)
	}

	// 第二次保存（upsert）
	cfg2 := &DistributionConfig{
		Enabled:    false,
		ListenPort: 9090,
		Protocol:   "vmess",
		Domain:     "updated.example.com",
	}
	if err := st.SaveDistributionConfig(cfg2); err != nil {
		t.Fatalf("SaveDistributionConfig() second error = %v", err)
	}

	// 验证更新成功
	got, _ := st.GetDistributionConfig()
	if got.Enabled {
		t.Errorf("Enabled = true, want false")
	}
	if got.ListenPort != 9090 {
		t.Errorf("ListenPort = %d, want 9090", got.ListenPort)
	}
	if got.Domain != "updated.example.com" {
		t.Errorf("Domain = %q", got.Domain)
	}
}

func TestCreateDistributionPath(t *testing.T) {
	st := newTestStore(t)

	path := &DistributionPath{
		Name:             "Test Path",
		Path:             "/proxy/test",
		UpstreamNodeKeys: []string{"1.1.1.1:443", "2.2.2.2:443"},
		LBStrategy:       "round-robin",
		Enabled:          true,
	}

	created, err := st.CreateDistributionPath(path)
	if err != nil {
		t.Fatalf("CreateDistributionPath() error = %v", err)
	}

	if created.ID == 0 {
		t.Error("ID = 0, want > 0")
	}
	if created.Name != "Test Path" {
		t.Errorf("Name = %q, want Test Path", created.Name)
	}
	if created.Path != "/proxy/test" {
		t.Errorf("Path = %q, want /proxy/test", created.Path)
	}
	if len(created.UpstreamNodeKeys) != 2 {
		t.Errorf("len(UpstreamNodeKeys) = %d, want 2", len(created.UpstreamNodeKeys))
	}
	if created.LBStrategy != "round-robin" {
		t.Errorf("LBStrategy = %q, want round-robin", created.LBStrategy)
	}
	if !created.Enabled {
		t.Error("Enabled = false, want true")
	}
}

func TestCreateDistributionPath_DefaultLBStrategy(t *testing.T) {
	st := newTestStore(t)

	path := &DistributionPath{
		Name:             "Test",
		Path:             "/test",
		UpstreamNodeKeys: []string{"1.1.1.1:443"},
		LBStrategy:       "", // 空，应使用默认值
		Enabled:          true,
	}

	created, err := st.CreateDistributionPath(path)
	if err != nil {
		t.Fatalf("CreateDistributionPath() error = %v", err)
	}

	if created.LBStrategy != "random" {
		t.Errorf("LBStrategy = %q, want random (default)", created.LBStrategy)
	}
}

func TestCreateDistributionPath_Validation(t *testing.T) {
	st := newTestStore(t)

	// 空名称
	_, err := st.CreateDistributionPath(&DistributionPath{
		Name:             "",
		Path:             "/test",
		UpstreamNodeKeys: []string{"1.1.1.1:443"},
	})
	if err == nil {
		t.Error("CreateDistributionPath with empty name expected error, got nil")
	}

	// 空路径
	_, err = st.CreateDistributionPath(&DistributionPath{
		Name:             "Test",
		Path:             "",
		UpstreamNodeKeys: []string{"1.1.1.1:443"},
	})
	if err == nil {
		t.Error("CreateDistributionPath with empty path expected error, got nil")
	}
}

func TestListDistributionPaths(t *testing.T) {
	st := newTestStore(t)

	// 创建多个路径
	_, _ = st.CreateDistributionPath(&DistributionPath{
		Name:             "Path A",
		Path:             "/proxy/a",
		UpstreamNodeKeys: []string{"1.1.1.1:443"},
		Enabled:          true,
	})
	_, _ = st.CreateDistributionPath(&DistributionPath{
		Name:             "Path B",
		Path:             "/proxy/b",
		UpstreamNodeKeys: []string{"2.2.2.2:443"},
		Enabled:          false,
	})

	paths, err := st.ListDistributionPaths()
	if err != nil {
		t.Fatalf("ListDistributionPaths() error = %v", err)
	}

	if len(paths) != 2 {
		t.Fatalf("len(paths) = %d, want 2", len(paths))
	}

	// 验证第一个路径
	if paths[0].Name != "Path A" {
		t.Errorf("paths[0].Name = %q, want Path A", paths[0].Name)
	}
	if !paths[0].Enabled {
		t.Error("paths[0].Enabled = false, want true")
	}

	// 验证第二个路径
	if paths[1].Name != "Path B" {
		t.Errorf("paths[1].Name = %q, want Path B", paths[1].Name)
	}
	if paths[1].Enabled {
		t.Error("paths[1].Enabled = true, want false")
	}
}

func TestGetDistributionPath(t *testing.T) {
	st := newTestStore(t)

	created, _ := st.CreateDistributionPath(&DistributionPath{
		Name:             "Test",
		Path:             "/test",
		UpstreamNodeKeys: []string{"1.1.1.1:443"},
		Enabled:          true,
	})

	got, err := st.GetDistributionPath(created.ID)
	if err != nil {
		t.Fatalf("GetDistributionPath() error = %v", err)
	}

	if got.ID != created.ID {
		t.Errorf("ID = %d, want %d", got.ID, created.ID)
	}
	if got.Name != "Test" {
		t.Errorf("Name = %q, want Test", got.Name)
	}
}

func TestGetDistributionPath_NotFound(t *testing.T) {
	st := newTestStore(t)

	_, err := st.GetDistributionPath(999)
	if err != ErrNotFound {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestGetDistributionPathByPath(t *testing.T) {
	st := newTestStore(t)

	created, _ := st.CreateDistributionPath(&DistributionPath{
		Name:             "Test",
		Path:             "/proxy/test",
		UpstreamNodeKeys: []string{"1.1.1.1:443"},
		Enabled:          true,
	})

	got, err := st.GetDistributionPathByPath("/proxy/test")
	if err != nil {
		t.Fatalf("GetDistributionPathByPath() error = %v", err)
	}

	if got.ID != created.ID {
		t.Errorf("ID = %d, want %d", got.ID, created.ID)
	}
}

func TestGetDistributionPathByPath_NotFound(t *testing.T) {
	st := newTestStore(t)

	_, err := st.GetDistributionPathByPath("/nonexistent")
	if err != ErrNotFound {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestUpdateDistributionPath(t *testing.T) {
	st := newTestStore(t)

	created, _ := st.CreateDistributionPath(&DistributionPath{
		Name:             "Original",
		Path:             "/original",
		UpstreamNodeKeys: []string{"1.1.1.1:443"},
		LBStrategy:       "random",
		Enabled:          true,
	})

	// 更新
	created.Name = "Updated"
	created.Path = "/updated"
	created.UpstreamNodeKeys = []string{"2.2.2.2:443", "3.3.3.3:443"}
	created.LBStrategy = "least-conn"
	created.Enabled = false

	if err := st.UpdateDistributionPath(created); err != nil {
		t.Fatalf("UpdateDistributionPath() error = %v", err)
	}

	// 验证更新
	got, _ := st.GetDistributionPath(created.ID)
	if got.Name != "Updated" {
		t.Errorf("Name = %q, want Updated", got.Name)
	}
	if got.Path != "/updated" {
		t.Errorf("Path = %q, want /updated", got.Path)
	}
	if len(got.UpstreamNodeKeys) != 2 {
		t.Errorf("len(UpstreamNodeKeys) = %d, want 2", len(got.UpstreamNodeKeys))
	}
	if got.LBStrategy != "least-conn" {
		t.Errorf("LBStrategy = %q, want least-conn", got.LBStrategy)
	}
	if got.Enabled {
		t.Error("Enabled = true, want false")
	}
}

func TestUpdateDistributionPath_NotFound(t *testing.T) {
	st := newTestStore(t)

	err := st.UpdateDistributionPath(&DistributionPath{
		ID:               999,
		Name:             "Test",
		Path:             "/test",
		UpstreamNodeKeys: []string{"1.1.1.1:443"},
	})
	if err != ErrNotFound {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestDeleteDistributionPath(t *testing.T) {
	st := newTestStore(t)

	created, _ := st.CreateDistributionPath(&DistributionPath{
		Name:             "Test",
		Path:             "/test",
		UpstreamNodeKeys: []string{"1.1.1.1:443"},
		Enabled:          true,
	})

	if err := st.DeleteDistributionPath(created.ID); err != nil {
		t.Fatalf("DeleteDistributionPath() error = %v", err)
	}

	// 验证已删除
	_, err := st.GetDistributionPath(created.ID)
	if err != ErrNotFound {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestDeleteDistributionPath_CascadeStats(t *testing.T) {
	st := newTestStore(t)

	path, _ := st.CreateDistributionPath(&DistributionPath{
		Name:             "Test",
		Path:             "/test",
		UpstreamNodeKeys: []string{"1.1.1.1:443"},
		Enabled:          true,
	})

	// 记录统计
	st.RecordDistributionStat(&DistributionStat{
		PathID:      path.ID,
		Timestamp:   time.Now(),
		Upload:      1000,
		Download:    2000,
		Connections: 5,
	})

	// 删除路径
	if err := st.DeleteDistributionPath(path.ID); err != nil {
		t.Fatalf("DeleteDistributionPath() error = %v", err)
	}

	// 验证统计也被删除（级联）
	stats, _ := st.GetDistributionStats(path.ID, 10)
	if len(stats) != 0 {
		t.Errorf("len(stats) = %d, want 0 (cascade delete)", len(stats))
	}
}

func TestRecordDistributionStat(t *testing.T) {
	st := newTestStore(t)

	path, _ := st.CreateDistributionPath(&DistributionPath{
		Name:             "Test",
		Path:             "/test",
		UpstreamNodeKeys: []string{"1.1.1.1:443"},
		Enabled:          true,
	})

	now := time.Now().Truncate(time.Second)
	stat := &DistributionStat{
		PathID:      path.ID,
		Timestamp:   now,
		Upload:      1024,
		Download:    2048,
		Connections: 3,
	}

	if err := st.RecordDistributionStat(stat); err != nil {
		t.Fatalf("RecordDistributionStat() error = %v", err)
	}

	// 验证记录
	stats, _ := st.GetDistributionStats(path.ID, 10)
	if len(stats) != 1 {
		t.Fatalf("len(stats) = %d, want 1", len(stats))
	}

	got := stats[0]
	if got.PathID != path.ID {
		t.Errorf("PathID = %d, want %d", got.PathID, path.ID)
	}
	if got.Upload != 1024 {
		t.Errorf("Upload = %d, want 1024", got.Upload)
	}
	if got.Download != 2048 {
		t.Errorf("Download = %d, want 2048", got.Download)
	}
	if got.Connections != 3 {
		t.Errorf("Connections = %d, want 3", got.Connections)
	}
}

func TestGetDistributionStats_OrderAndLimit(t *testing.T) {
	st := newTestStore(t)

	path, _ := st.CreateDistributionPath(&DistributionPath{
		Name:             "Test",
		Path:             "/test",
		UpstreamNodeKeys: []string{"1.1.1.1:443"},
		Enabled:          true,
	})

	// 记录多条统计（时间递增）
	for i := 0; i < 5; i++ {
		st.RecordDistributionStat(&DistributionStat{
			PathID:      path.ID,
			Timestamp:   time.Now().Add(time.Duration(i) * time.Minute),
			Upload:      int64(i * 100),
			Download:    int64(i * 200),
			Connections: int64(i),
		})
	}

	// 查询最近 3 条
	stats, err := st.GetDistributionStats(path.ID, 3)
	if err != nil {
		t.Fatalf("GetDistributionStats() error = %v", err)
	}

	if len(stats) != 3 {
		t.Fatalf("len(stats) = %d, want 3", len(stats))
	}

	// 验证时间倒序（最新的在前）
	if stats[0].Upload < stats[1].Upload {
		t.Error("stats not ordered by timestamp DESC")
	}
}

func TestUpdatePathTotalStats(t *testing.T) {
	st := newTestStore(t)

	path, _ := st.CreateDistributionPath(&DistributionPath{
		Name:             "Test",
		Path:             "/test",
		UpstreamNodeKeys: []string{"1.1.1.1:443"},
		Enabled:          true,
	})

	// 第一次增量
	if err := st.UpdatePathTotalStats(path.ID, 1000, 2000, 5); err != nil {
		t.Fatalf("UpdatePathTotalStats() first error = %v", err)
	}

	got, _ := st.GetDistributionPath(path.ID)
	if got.TotalUpload != 1000 {
		t.Errorf("TotalUpload = %d, want 1000", got.TotalUpload)
	}
	if got.TotalDownload != 2000 {
		t.Errorf("TotalDownload = %d, want 2000", got.TotalDownload)
	}
	if got.TotalConnections != 5 {
		t.Errorf("TotalConnections = %d, want 5", got.TotalConnections)
	}
	if got.LastAccess.IsZero() {
		t.Error("LastAccess is zero, want current timestamp")
	}

	// 第二次增量（累加）
	if err := st.UpdatePathTotalStats(path.ID, 500, 1000, 3); err != nil {
		t.Fatalf("UpdatePathTotalStats() second error = %v", err)
	}

	got, _ = st.GetDistributionPath(path.ID)
	if got.TotalUpload != 1500 {
		t.Errorf("TotalUpload = %d, want 1500 (cumulative)", got.TotalUpload)
	}
	if got.TotalDownload != 3000 {
		t.Errorf("TotalDownload = %d, want 3000 (cumulative)", got.TotalDownload)
	}
	if got.TotalConnections != 8 {
		t.Errorf("TotalConnections = %d, want 8 (cumulative)", got.TotalConnections)
	}
}

func TestUpdatePathTotalStats_NotFound(t *testing.T) {
	st := newTestStore(t)

	err := st.UpdatePathTotalStats(999, 100, 200, 1)
	if err != ErrNotFound {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestDistributionPath_EmptyUpstreamNodeKeys(t *testing.T) {
	st := newTestStore(t)

	// 创建空的上游节点列表
	path := &DistributionPath{
		Name:             "Empty Upstream",
		Path:             "/empty",
		UpstreamNodeKeys: []string{},
		Enabled:          true,
	}

	created, err := st.CreateDistributionPath(path)
	if err != nil {
		t.Fatalf("CreateDistributionPath() error = %v", err)
	}

	// 验证空数组正确序列化和反序列化
	got, _ := st.GetDistributionPath(created.ID)
	if got.UpstreamNodeKeys == nil {
		t.Error("UpstreamNodeKeys is nil, want empty slice")
	}
	if len(got.UpstreamNodeKeys) != 0 {
		t.Errorf("len(UpstreamNodeKeys) = %d, want 0", len(got.UpstreamNodeKeys))
	}
}

func TestDistributionPath_JSONRoundtrip(t *testing.T) {
	st := newTestStore(t)

	nodeKeys := []string{
		"192.168.1.1:443",
		"10.0.0.1:8443",
		"example.com:443",
	}

	path := &DistributionPath{
		Name:             "JSON Test",
		Path:             "/json",
		UpstreamNodeKeys: nodeKeys,
		Enabled:          true,
	}

	created, err := st.CreateDistributionPath(path)
	if err != nil {
		t.Fatalf("CreateDistributionPath() error = %v", err)
	}

	// 验证 JSON 往返完整性
	got, _ := st.GetDistributionPath(created.ID)
	if len(got.UpstreamNodeKeys) != len(nodeKeys) {
		t.Fatalf("len(UpstreamNodeKeys) = %d, want %d", len(got.UpstreamNodeKeys), len(nodeKeys))
	}

	for i, key := range nodeKeys {
		if got.UpstreamNodeKeys[i] != key {
			t.Errorf("UpstreamNodeKeys[%d] = %q, want %q", i, got.UpstreamNodeKeys[i], key)
		}
	}
}
