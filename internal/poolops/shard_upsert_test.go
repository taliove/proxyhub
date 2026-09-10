package poolops

import (
	"context"
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
	_ "modernc.org/sqlite"
)

// loadBySource 按键索引池中指定来源的节点快照(含 stale)。
func loadBySource(t *testing.T, st *store.Store, source string) map[string]subscription.Node {
	t.Helper()
	pool, err := st.LoadNodePool()
	if err != nil {
		t.Fatalf("LoadNodePool() error = %v", err)
	}
	out := make(map[string]subscription.Node)
	for _, n := range pool {
		if n.Source == source {
			out[n.NodeKey()] = *n
		}
	}
	return out
}

// 两机场入池后刷新机场 A,机场 B 的节点必须逐字段不变(issue #143 Bug 3:
// 分片局部 upsert,不再读全池-写全池)。
func TestUpsertAirportNodes_OtherAirportUntouchedFieldByField(t *testing.T) {
	adapter, st := newTestAdapter(t)

	checked := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	lastSeen := time.Now().Add(-24 * time.Hour).UTC().Truncate(time.Second)
	seed := append(makeNodes("airport-a", "1.1", 2),
		&subscription.Node{
			Name: "b1", Type: "vless", Server: "10.2.2.1", Port: 443, Source: "airport-b",
			Region: "HK", Available: true, Latency: 88, DetectionLastCheck: checked,
			DetectionKind: "real", BandwidthDownMbps: 12.5, BandwidthUpMbps: 3.25,
			Plugin: "simple-obfs", PluginOpts: "obfs=http",
		},
		&subscription.Node{
			Name: "b2", Type: "trojan", Server: "10.2.2.2", Port: 443, Source: "airport-b",
			Stale: true, LastSeen: lastSeen, Password: "p2",
		},
	)
	if err := st.SaveNodePool(seed); err != nil {
		t.Fatalf("SaveNodePool() seed error = %v", err)
	}

	before := loadBySource(t, st, "airport-b")
	if len(before) != 2 {
		t.Fatalf("seed: airport-b has %d nodes, want 2", len(before))
	}

	// 刷新机场 A:改一批全新节点(旧 A 节点全部消失)
	if err := adapter.UpsertAirportNodes(context.Background(), "airport-a", 0, makeNodes("airport-a", "9.9", 3)); err != nil {
		t.Fatalf("UpsertAirportNodes() error = %v", err)
	}

	after := loadBySource(t, st, "airport-b")
	if len(after) != len(before) {
		t.Fatalf("airport-b node count changed: %d -> %d", len(before), len(after))
	}
	for key, want := range before {
		got, ok := after[key]
		if !ok {
			t.Errorf("airport-b node %s disappeared", key)
			continue
		}
		if !reflect.DeepEqual(want, got) {
			t.Errorf("airport-b node %s rewritten:\n before = %+v\n after  = %+v", key, want, got)
		}
	}
}

// 机场 B 存在"异常数据"(任何重写 B 行的写路径都会失败,用毒触发器模拟)时,
// 刷新机场 A 仍必须成功(issue #143 Bug 3:旧实现先 UPDATE 全表 stale,
// B 的异常行会让 A 的刷新连坐回滚)。
func TestUpsertAirportNodes_CorruptOtherAirportRowsDoNotBlock(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := store.OpenForTesting(dbPath)
	if err != nil {
		t.Fatalf("store.OpenForTesting() error = %v", err)
	}
	t.Cleanup(func() { st.Close() })
	adapter := NewStoreAdapter(st, nil)

	seed := append(makeNodes("airport-a", "1.1", 2), makeNodes("airport-b", "2.2", 2)...)
	if err := st.SaveNodePool(seed); err != nil {
		t.Fatalf("SaveNodePool() seed error = %v", err)
	}

	// 独立连接安装毒触发器:任何对 airport-b 行的 UPDATE/DELETE 直接失败。
	poison, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	defer poison.Close()
	for _, ddl := range []string{
		`CREATE TRIGGER poison_b_update BEFORE UPDATE ON nodes
			WHEN OLD.source = 'airport-b'
			BEGIN SELECT RAISE(ABORT, 'corrupt airport-b row'); END;`,
		`CREATE TRIGGER poison_b_delete BEFORE DELETE ON nodes
			WHEN OLD.source = 'airport-b'
			BEGIN SELECT RAISE(ABORT, 'corrupt airport-b row'); END;`,
	} {
		if _, err := poison.Exec(ddl); err != nil {
			t.Fatalf("install poison trigger error = %v", err)
		}
	}

	if err := adapter.UpsertAirportNodes(context.Background(), "airport-a", 0, makeNodes("airport-a", "3.3", 2)); err != nil {
		t.Fatalf("airport-a refresh blocked by corrupt airport-b rows: %v", err)
	}

	after := loadBySource(t, st, "airport-b")
	if len(after) != 2 {
		t.Fatalf("airport-b node count = %d, want 2 (untouched)", len(after))
	}
	for key, n := range after {
		if n.Stale {
			t.Errorf("airport-b node %s touched (stale flipped)", key)
		}
	}
	if got := loadBySource(t, st, "airport-a"); len(got) != 4 { // 2 新 + 2 消失标 stale
		t.Fatalf("airport-a node count = %d, want 4 (2 active + 2 stale)", len(got))
	}
}

// 两用户同名机场(机场名是用户自选字符串,可撞名):用户 A 的单机场 upsert
// 不得触碰用户 B 的行(issue #143 跨用户隔离)——carry-forward 集、stale 标记、
// position、标签/prune/purge 全部限定属主;同时新拉取节点被打上属主 user_id。
func TestUpsertAirportNodes_SameNameAirportUserIsolation(t *testing.T) {
	adapter, st := newTestAdapter(t)

	checked := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	seed := []*subscription.Node{
		{
			Name: "a-old", Type: "vless", Server: "10.1.1.1", Port: 443, Source: "same-airport",
			UserID: 1, Available: true, Latency: 66, DetectionLastCheck: checked,
		},
		{Name: "a-gone", Type: "vless", Server: "10.1.1.2", Port: 443, Source: "same-airport", UserID: 1},
		{
			Name: "b-keep", Type: "vless", Server: "10.2.2.1", Port: 443, Source: "same-airport",
			UserID: 2, Region: "HK", Available: true, Latency: 88, DetectionLastCheck: checked,
			DetectionKind: "real", BandwidthDownMbps: 12.5,
		},
	}
	if err := st.SaveNodePool(seed); err != nil {
		t.Fatalf("SaveNodePool() seed error = %v", err)
	}
	beforeB := loadBySource(t, st, "same-airport")["10.2.2.1:443"]

	// 用户 A 刷新同名机场:改名留一个(carry-forward)+ 新增一个;a-gone 消失。
	fetched := []*subscription.Node{
		{Name: "a-new-name", Type: "vless", Server: "10.1.1.1", Port: 443, Source: "same-airport"},
		{Name: "a-added", Type: "vless", Server: "10.1.1.3", Port: 443, Source: "same-airport"},
	}
	if err := adapter.UpsertAirportNodes(context.Background(), "same-airport", 1, fetched); err != nil {
		t.Fatalf("UpsertAirportNodes() error = %v", err)
	}

	after := loadBySource(t, st, "same-airport")

	// 用户 B 的节点逐字段不变(不被误标 stale、检测状态/position 不被重写)。
	afterB, ok := after["10.2.2.1:443"]
	if !ok {
		t.Fatal("user 2 node disappeared after user 1 refresh")
	}
	if !reflect.DeepEqual(beforeB, afterB) {
		t.Errorf("user 2 node rewritten by user 1 refresh:\n before = %+v\n after  = %+v", beforeB, afterB)
	}

	// 用户 A 的分片语义正常:carry-forward、消失标 stale、新节点打上属主。
	carried := after["10.1.1.1:443"]
	if carried.Stale || carried.Name != "a-new-name" {
		t.Errorf("carried node: stale=%v name=%q, want active a-new-name", carried.Stale, carried.Name)
	}
	if !carried.Available || carried.Latency != 66 || !carried.DetectionLastCheck.Equal(checked) {
		t.Errorf("carry-forward lost on user 1 node: %+v", carried)
	}
	if carried.UserID != 1 {
		t.Errorf("carried node UserID = %d, want 1 (owner tag)", carried.UserID)
	}
	if gone := after["10.1.1.2:443"]; !gone.Stale {
		t.Errorf("disappeared user 1 node Stale = false, want true")
	}
	if added := after["10.1.1.3:443"]; added.UserID != 1 || added.Stale {
		t.Errorf("added node: UserID = %d stale = %v, want owner 1 active", added.UserID, added.Stale)
	}
}

// 语义等价回归:本机场消失的节点仍正确标 stale(检测状态与 LastSeen 保留)。
func TestUpsertAirportNodes_DisappearedNodesStillMarkedStale(t *testing.T) {
	adapter, st := newTestAdapter(t)

	lastSeen := time.Now().Add(-2 * time.Hour).UTC().Truncate(time.Second)
	checked := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	stayer := &subscription.Node{
		Name: "stay", Type: "vless", Server: "10.5.5.1", Port: 443, Source: "airport-a",
	}
	goner := &subscription.Node{
		Name: "gone", Type: "vless", Server: "10.5.5.2", Port: 443, Source: "airport-a",
		Available: true, Latency: 66, DetectionLastCheck: checked, LastSeen: lastSeen,
	}
	if err := st.SaveNodePool([]*subscription.Node{stayer, goner}); err != nil {
		t.Fatalf("SaveNodePool() seed error = %v", err)
	}

	// 刷新只剩 stayer(改名,同 NodeKey)
	fresh := &subscription.Node{
		Name: "stay-v2", Type: "vless", Server: "10.5.5.1", Port: 443, Source: "airport-a",
	}
	if err := adapter.UpsertAirportNodes(context.Background(), "airport-a", 0, []*subscription.Node{fresh}); err != nil {
		t.Fatalf("UpsertAirportNodes() error = %v", err)
	}

	got := loadBySource(t, st, "airport-a")
	if len(got) != 2 {
		t.Fatalf("airport-a has %d nodes, want 2 (1 active + 1 stale)", len(got))
	}

	stay := got[stayer.NodeKey()]
	if stay.Stale {
		t.Errorf("stayer Stale = true, want false")
	}
	if stay.Name != "stay-v2" {
		t.Errorf("stayer Name = %q, want stay-v2 (identity columns refreshed)", stay.Name)
	}
	if stay.LastSeen.IsZero() {
		t.Errorf("stayer LastSeen not refreshed")
	}

	gone := got[goner.NodeKey()]
	if !gone.Stale {
		t.Errorf("disappeared node Stale = false, want true")
	}
	if !gone.LastSeen.Equal(lastSeen) {
		t.Errorf("disappeared node LastSeen = %v, want preserved %v", gone.LastSeen, lastSeen)
	}
	if !gone.Available || gone.Latency != 66 || !gone.DetectionLastCheck.Equal(checked) {
		t.Errorf("disappeared node detection state lost: %+v", gone)
	}
}
