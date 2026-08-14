package poolops

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

func newTestAdapter(t *testing.T) (*StoreAdapter, *store.Store) {
	t.Helper()
	st, err := store.OpenForTesting(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.OpenForTesting() error = %v", err)
	}
	t.Cleanup(func() { st.Close() })
	// nil 识别器:跳过地区识别,这些测试只关心池合并语义(地区填充有专测)。
	return NewStoreAdapter(st, nil), st
}

// makeNodes 构造 n 个属于 source 的节点,NodeKey 以 keyPrefix 区分批次。
// 用保留 IP 段(10/8)避免 geoip 兜底产生地区,测试只关心池合并语义。
func makeNodes(source, keyPrefix string, n int) []*subscription.Node {
	nodes := make([]*subscription.Node, 0, n)
	for i := 0; i < n; i++ {
		nodes = append(nodes, &subscription.Node{
			Name:   fmt.Sprintf("%s-%d", keyPrefix, i),
			Type:   "vless",
			Server: fmt.Sprintf("10.%s.%d", keyPrefix, i),
			Port:   443,
			Source: source,
		})
	}
	return nodes
}

func poolSources(t *testing.T, st *store.Store) map[string][]string {
	t.Helper()
	pool, err := st.LoadNodePool()
	if err != nil {
		t.Fatalf("LoadNodePool() error = %v", err)
	}
	bySource := make(map[string][]string)
	for _, n := range pool {
		bySource[n.Source] = append(bySource[n.Source], n.Name)
	}
	for k := range bySource {
		sort.Strings(bySource[k])
	}
	return bySource
}

// 并发 upsert 不同机场:每个机场的最终节点都必须完整保留。
// UpsertAirportNodes 是"读全池-改本机场-写全池",无串行时并行写会
// lost update(后写覆盖先写,整个机场的节点消失)。-race 下同时验证无数据竞争。
func TestUpsertAirportNodes_ConcurrentDistinctAirports_NoLostUpdate(t *testing.T) {
	adapter, st := newTestAdapter(t)

	const airports = 8
	const nodesPerAirport = 3
	var wg sync.WaitGroup
	for i := 0; i < airports; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := fmt.Sprintf("airport-%d", i)
			if err := adapter.UpsertAirportNodes(context.Background(), name, makeNodes(name, fmt.Sprintf("%d.0", i), nodesPerAirport)); err != nil {
				t.Errorf("UpsertAirportNodes(%s) error = %v", name, err)
			}
		}(i)
	}
	wg.Wait()

	bySource := poolSources(t, st)
	if len(bySource) != airports {
		t.Fatalf("pool has %d sources, want %d (lost update: some airport's upsert was overwritten)", len(bySource), airports)
	}
	for i := 0; i < airports; i++ {
		name := fmt.Sprintf("airport-%d", i)
		if got := len(bySource[name]); got != nodesPerAirport {
			t.Errorf("source %s has %d nodes, want %d", name, got, nodesPerAirport)
		}
	}
}

// 并发 upsert 同一机场:写串行保证结果等价于"按某顺序逐个执行"——
// 全部批次的节点都在场(先写批次被 MergePool 标 stale 保留),且
// active 节点恰好是最后写入批次的完整一套。无串行时两次读-改-写交错,
// 后写直接覆盖先写:先写批次的节点整批消失(连 stale 都留不下)。
func TestUpsertAirportNodes_ConcurrentSameAirport_Serialized(t *testing.T) {
	adapter, st := newTestAdapter(t)

	const writers = 4
	const nodesPerWriter = 3
	want := make(map[string]bool, writers)
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		setKey := fmt.Sprintf("w%d", i)
		want[setKey] = true
		wg.Add(1)
		go func(i int, setKey string) {
			defer wg.Done()
			if err := adapter.UpsertAirportNodes(context.Background(), "airport-a", makeNodes("airport-a", setKey, nodesPerWriter)); err != nil {
				t.Errorf("UpsertAirportNodes() error = %v", err)
			}
		}(i, setKey)
	}
	wg.Wait()

	pool, err := st.LoadNodePool()
	if err != nil {
		t.Fatalf("LoadNodePool() error = %v", err)
	}
	var active []string
	total := 0
	for _, n := range pool {
		if n.Source != "airport-a" {
			continue
		}
		total++
		if !n.Stale {
			active = append(active, n.Name)
		}
	}
	if total != writers*nodesPerWriter {
		t.Fatalf("airport-a has %d nodes, want %d (lost update: an earlier upsert was overwritten entirely)", total, writers*nodesPerWriter)
	}
	if len(active) != nodesPerWriter {
		t.Fatalf("airport-a has %d active nodes, want %d (mixed writes: %v)", len(active), nodesPerWriter, active)
	}
	// active 节点必须来自同一批次(最后写入者)
	sort.Strings(active)
	prefix := active[0][:2]
	if !want[prefix] {
		t.Fatalf("unexpected node set prefix %q in %v", prefix, active)
	}
	for _, name := range active {
		if name[:2] != prefix {
			t.Fatalf("mixed writes: node %s does not belong to winning set %s (%v)", name, prefix, active)
		}
	}
}

// 基本合并语义:本机场旧节点 carry-forward 检测状态,其他机场节点不动。
func TestUpsertAirportNodes_MergeCarryForward(t *testing.T) {
	adapter, st := newTestAdapter(t)

	checked := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	old := &subscription.Node{
		Name:               "old-a",
		Type:               "vless",
		Server:             "10.9.9.1",
		Port:               443,
		Source:             "airport-a",
		Available:          true,
		Latency:            123,
		DetectionLastCheck: checked,
	}
	other := makeNodes("airport-b", "8.8", 2)
	if err := st.SaveNodePool(append([]*subscription.Node{old}, other...)); err != nil {
		t.Fatalf("SaveNodePool() error = %v", err)
	}

	// 同 NodeKey 的新拉取节点(检测字段零值)+ 一个新节点
	fresh := &subscription.Node{
		Name:   "new-a",
		Type:   "vless",
		Server: "10.9.9.1",
		Port:   443,
		Source: "airport-a",
	}
	added := makeNodes("airport-a", "9.9", 1)
	if err := adapter.UpsertAirportNodes(context.Background(), "airport-a", append([]*subscription.Node{fresh}, added...)); err != nil {
		t.Fatalf("UpsertAirportNodes() error = %v", err)
	}

	pool, err := st.LoadNodePool()
	if err != nil {
		t.Fatalf("LoadNodePool() error = %v", err)
	}
	var mergedA, otherCount int
	for _, n := range pool {
		switch n.Source {
		case "airport-a":
			mergedA++
			if n.NodeKey() == old.NodeKey() {
				if !n.Available || n.Latency != 123 || n.DetectionLastCheck.IsZero() {
					t.Errorf("carry-forward lost on %s: available=%v latency=%d lastCheck=%v",
						n.NodeKey(), n.Available, n.Latency, n.DetectionLastCheck)
				}
			}
		case "airport-b":
			otherCount++
		}
	}
	if mergedA != 2 {
		t.Errorf("airport-a has %d nodes, want 2", mergedA)
	}
	if otherCount != 2 {
		t.Errorf("airport-b has %d nodes, want 2 (other airports must stay untouched)", otherCount)
	}
}
