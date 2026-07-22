package aggregator

import (
	"errors"
	"testing"

	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

// purgePoolFixture 两个机场节点 + 一个自建节点(fixture 一律 example.com)。
func purgePoolFixture() []*subscription.Node {
	return []*subscription.Node{
		{Name: "香港01", Type: "ss", Server: "hk01.example.com", Port: 8388,
			Cipher: "aes-256-gcm", Password: "p1", Region: "HK", Source: "机场A", Available: true},
		{Name: "日本01", Type: "ss", Server: "jp01.example.com", Port: 8389,
			Cipher: "aes-256-gcm", Password: "p2", Region: "JP", Source: "机场B", Available: true},
		{Name: "自建", Type: "trojan", Server: "self.example.com", Port: 443,
			Password: "p3", TLS: true, Region: "US",
			Source: subscription.SourceSelfHosted, Available: true},
	}
}

// TestPurgeAirportNodes_DoubleClear 双清语义:内存池与 DB 快照同时无任何机场节点,
// 自建节点两侧都豁免。
func TestPurgeAirportNodes_DoubleClear(t *testing.T) {
	agg, st := newTestAggregator(t)

	if err := st.SaveNodePool(purgePoolFixture()); err != nil {
		t.Fatalf("SaveNodePool() error = %v", err)
	}
	// 模拟进程重启回填:内存池从 DB 快照恢复
	agg = newTestAggregatorWithStore(t, st)
	if got := len(agg.Nodes()); got != 3 {
		t.Fatalf("pool size before purge = %d, want 3", got)
	}

	removed, err := agg.PurgeAirportNodes()
	if err != nil {
		t.Fatalf("PurgeAirportNodes() error = %v", err)
	}
	if removed != 2 {
		t.Errorf("removed = %d, want 2", removed)
	}

	// 内存池:只剩自建
	mem := agg.Nodes()
	if len(mem) != 1 || mem[0].Source != subscription.SourceSelfHosted {
		t.Fatalf("memory pool after purge = %+v, want only the self-hosted node", mem)
	}

	// DB 快照:只剩自建(否则重启后旧节点复活,等于没清)
	db, err := st.LoadNodePool()
	if err != nil {
		t.Fatalf("LoadNodePool() error = %v", err)
	}
	if len(db) != 1 || db[0].Source != subscription.SourceSelfHosted {
		t.Fatalf("db pool after purge = %+v, want only the self-hosted node", db)
	}
}

// TestPurgeAirportNodes_Conflict 并发语义:有刷新任务进行中时拒绝清空(ErrPurgeConflict),
// 刷新结束后放行。
func TestPurgeAirportNodes_Conflict(t *testing.T) {
	agg, st := newTestAggregator(t)
	release := make(chan struct{})
	srv := gatedSubscriptionServer(t, release)
	if _, err := st.CreateAirport("慢机场", srv.URL); err != nil {
		t.Fatalf("CreateAirport() error = %v", err)
	}

	jobID, _, started, err := agg.StartRefreshJob(store.RefreshTriggerManual)
	if err != nil || !started {
		t.Fatalf("StartRefreshJob() = started=%v err=%v, want running", started, err)
	}

	// 刷新进行中:清空必须被拒绝
	if _, err := agg.PurgeAirportNodes(); !errors.Is(err, ErrPurgeConflict) {
		t.Fatalf("PurgeAirportNodes() during refresh error = %v, want ErrPurgeConflict", err)
	}

	// 放行刷新,等终态后清空应成功
	close(release)
	waitJobStatus(t, st, jobID)

	if _, err := agg.PurgeAirportNodes(); err != nil {
		t.Fatalf("PurgeAirportNodes() after refresh error = %v", err)
	}
}

// TestPurgeAirportNodes_EmptyPool 空池清空是 no-op。
func TestPurgeAirportNodes_EmptyPool(t *testing.T) {
	agg, _ := newTestAggregator(t)
	removed, err := agg.PurgeAirportNodes()
	if err != nil {
		t.Fatalf("PurgeAirportNodes() error = %v", err)
	}
	if removed != 0 {
		t.Errorf("removed = %d, want 0", removed)
	}
	if got := len(agg.Nodes()); got != 0 {
		t.Errorf("pool size = %d, want 0", got)
	}
}
