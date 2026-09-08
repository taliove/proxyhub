package store

import (
	"testing"
	"time"

	"github.com/taliove/proxyhub/internal/subscription"
)

// TestUpsertNodePoolShard_RewritesOnlySourceShard 验证分片局部 upsert:
// 目标来源分片被重写(在架更新/新增入池/消失标 stale),其他来源的行不动。
func TestUpsertNodePoolShard_RewritesOnlySourceShard(t *testing.T) {
	st := newTestStore(t)

	lastSeen := time.Now().Add(-24 * time.Hour).Truncate(time.Second)
	checked := time.Now().Add(-time.Hour).Truncate(time.Second)

	seed := []*subscription.Node{
		{Name: "a1", Type: "ss", Server: "10.1.1.1", Port: 8388, Source: "airport-a"},
		{Name: "a2", Type: "ss", Server: "10.1.1.2", Port: 8388, Source: "airport-a"},
		{
			Name: "b1", Type: "vless", Server: "10.2.2.1", Port: 443, Source: "airport-b",
			Region: "HK", Available: true, Latency: 88, DetectionLastCheck: checked,
			BandwidthDownMbps: 12.5, Plugin: "simple-obfs", PluginOpts: "obfs=http",
		},
		{Name: "b2", Type: "vless", Server: "10.2.2.2", Port: 443, Source: "airport-b", Stale: true, LastSeen: lastSeen},
	}
	if err := st.SaveNodePool(seed); err != nil {
		t.Fatalf("SaveNodePool() seed error = %v", err)
	}

	// 本机场新状态:a1 改名(同 NodeKey)、a3 新增、a2 不在列表(应留 stale)
	shard := []*subscription.Node{
		{Name: "a1-renamed", Type: "ss", Server: "10.1.1.1", Port: 8388, Source: "airport-a"},
		{Name: "a3", Type: "ss", Server: "10.1.1.3", Port: 8388, Source: "airport-a"},
	}
	if err := st.UpsertNodePoolShard("airport-a", shard); err != nil {
		t.Fatalf("UpsertNodePoolShard() error = %v", err)
	}

	got, err := st.LoadNodePool()
	if err != nil {
		t.Fatalf("LoadNodePool() error = %v", err)
	}
	byName := make(map[string]*subscription.Node, len(got))
	for _, n := range got {
		byName[n.Name] = n
	}
	// a2 改名后旧名查不到,按 server 补查
	var a2 *subscription.Node
	for _, n := range got {
		if n.Server == "10.1.1.2" {
			a2 = n
		}
	}

	if n := byName["a1-renamed"]; n == nil || n.Stale {
		t.Errorf("a1-renamed 应在架, got %+v", n)
	}
	if n := byName["a3"]; n == nil || n.Stale {
		t.Errorf("a3 应在架, got %+v", n)
	}
	if a2 == nil || !a2.Stale {
		t.Errorf("a2 应被标记 stale, got %+v", a2)
	}

	// 机场 B 两行逐字段不动
	b1 := byName["b1"]
	if b1 == nil {
		t.Fatalf("b1 丢失")
	}
	if b1.Stale || !b1.Available || b1.Latency != 88 || !b1.DetectionLastCheck.Equal(checked) ||
		b1.BandwidthDownMbps != 12.5 || b1.Plugin != "simple-obfs" || b1.PluginOpts != "obfs=http" || b1.Region != "HK" {
		t.Errorf("b1 被改写: %+v", b1)
	}
	b2 := byName["b2"]
	if b2 == nil {
		t.Fatalf("b2 丢失")
	}
	if !b2.Stale || !b2.LastSeen.Equal(lastSeen) {
		t.Errorf("b2 被改写: stale=%v lastSeen=%v", b2.Stale, b2.LastSeen)
	}
}

// TestUpsertNodePoolShard_OtherShardRowsNotTouched 用毒触发器模拟"其他机场
// 异常数据导致该行无法重写"(issue #152 Bug 3):任何 UPDATE/DELETE airport-b
// 行的写路径都会失败。分片 upsert 只写本机场,必须成功;旧的全池重写路径
// (SaveNodePool 先 UPDATE 全表 stale)会在这里连坐失败。
func TestUpsertNodePoolShard_OtherShardRowsNotTouched(t *testing.T) {
	st := newTestStore(t)

	seed := []*subscription.Node{
		{Name: "a1", Type: "ss", Server: "10.1.1.1", Port: 8388, Source: "airport-a"},
		{Name: "b1", Type: "vless", Server: "10.2.2.1", Port: 443, Source: "airport-b"},
	}
	if err := st.SaveNodePool(seed); err != nil {
		t.Fatalf("SaveNodePool() seed error = %v", err)
	}

	for _, ddl := range []string{
		`CREATE TRIGGER poison_b_update BEFORE UPDATE ON nodes
			WHEN OLD.source = 'airport-b'
			BEGIN SELECT RAISE(ABORT, 'corrupt airport-b row'); END;`,
		`CREATE TRIGGER poison_b_delete BEFORE DELETE ON nodes
			WHEN OLD.source = 'airport-b'
			BEGIN SELECT RAISE(ABORT, 'corrupt airport-b row'); END;`,
	} {
		if _, err := st.db.Exec(ddl); err != nil {
			t.Fatalf("install poison trigger error = %v", err)
		}
	}

	shard := []*subscription.Node{
		{Name: "a1-new", Type: "ss", Server: "10.1.1.1", Port: 8388, Source: "airport-a"},
		{Name: "a2", Type: "ss", Server: "10.1.1.2", Port: 8388, Source: "airport-a"},
	}
	if err := st.UpsertNodePoolShard("airport-a", shard); err != nil {
		t.Fatalf("UpsertNodePoolShard() 被 airport-b 异常行连坐: %v", err)
	}

	got, err := st.LoadNodePool()
	if err != nil {
		t.Fatalf("LoadNodePool() error = %v", err)
	}
	var b1 *subscription.Node
	aCount := 0
	for _, n := range got {
		switch n.Source {
		case "airport-a":
			aCount++
		case "airport-b":
			b1 = n
		}
	}
	if aCount != 2 {
		t.Errorf("airport-a 应有 2 个节点, got %d", aCount)
	}
	if b1 == nil || b1.Name != "b1" || b1.Stale {
		t.Errorf("airport-b 行被触碰: %+v", b1)
	}
}

// TestUpsertNodePoolShard_PruneAndPurgeScopedToSource 验证死节点标签修剪与超期
// stale 清理在分片化后语义等价:只作用于目标来源分片,其他来源的 stale 标签
// 与超期 stale 节点都保留(等自己的分片刷新或全量刷新处理)。
func TestUpsertNodePoolShard_PruneAndPurgeScopedToSource(t *testing.T) {
	st := newTestStore(t)

	recent := time.Now().Add(-time.Hour).Truncate(time.Second)
	seed := []*subscription.Node{
		{Name: "a-live", Type: "ss", Server: "10.1.1.1", Port: 8388, Source: "airport-a"},
		{Name: "a-stale", Type: "ss", Server: "10.1.1.2", Port: 8388, Source: "airport-a", Stale: true, LastSeen: recent},
		{Name: "a-expired", Type: "ss", Server: "10.1.1.3", Port: 8388, Source: "airport-a", Stale: true, LastSeen: recent},
		{Name: "b-live", Type: "ss", Server: "10.2.2.1", Port: 8388, Source: "airport-b"},
		{Name: "b-stale", Type: "ss", Server: "10.2.2.2", Port: 8388, Source: "airport-b", Stale: true, LastSeen: recent},
		{Name: "b-expired", Type: "ss", Server: "10.2.2.3", Port: 8388, Source: "airport-b", Stale: true, LastSeen: recent},
	}
	if err := st.SaveNodePool(seed); err != nil {
		t.Fatalf("SaveNodePool() seed error = %v", err)
	}

	// 种入后再把超期行的 last_seen 改到保留期之前(躲过种子保存时的 purge)
	expired := time.Now().AddDate(0, 0, -(StaleRetentionDays + 1))
	if _, err := st.db.Exec(`UPDATE nodes SET last_seen = ? WHERE name IN ('a-expired', 'b-expired')`, expired); err != nil {
		t.Fatalf("backdate last_seen error = %v", err)
	}

	// 两个机场各给一个 stale 节点挂自动标签
	for _, key := range []string{"10.1.1.2:8388", "10.2.2.2:8388"} {
		if err := st.ReplaceNodeTags(key, []string{"fast"}); err != nil {
			t.Fatalf("ReplaceNodeTags(%s) error = %v", key, err)
		}
	}

	// 只重写 airport-a 分片:a-live 仍在架,a-stale/a-expired 不在列表(保持 stale)
	if err := st.UpsertNodePoolShard("airport-a", []*subscription.Node{
		{Name: "a-live", Type: "ss", Server: "10.1.1.1", Port: 8388, Source: "airport-a"},
	}); err != nil {
		t.Fatalf("UpsertNodePoolShard() error = %v", err)
	}

	got, err := st.LoadNodePool()
	if err != nil {
		t.Fatalf("LoadNodePool() error = %v", err)
	}
	names := make(map[string]bool, len(got))
	for _, n := range got {
		names[n.Name] = true
	}

	// 本分片:超期 stale 被物理删除,保留期内 stale 保留
	if names["a-expired"] {
		t.Errorf("a-expired 应被分片 purge 删除")
	}
	if !names["a-stale"] {
		t.Errorf("a-stale 应保留")
	}
	// 本分片:stale 节点标签被修剪
	tags, err := st.ListNodeTags([]string{"10.1.1.2:8388"})
	if err != nil {
		t.Fatalf("ListNodeTags() error = %v", err)
	}
	if len(tags["10.1.1.2:8388"]) != 0 {
		t.Errorf("a-stale 的标签应被分片 prune 清除, got %v", tags["10.1.1.2:8388"])
	}

	// 其他分片:超期 stale 与 stale 标签都不动
	if !names["b-expired"] {
		t.Errorf("b-expired 不应被本分片 purge 删除")
	}
	if !names["b-stale"] || !names["b-live"] {
		t.Errorf("airport-b 节点不应被动, got %v", names)
	}
	bTags, err := st.ListNodeTags([]string{"10.2.2.2:8388"})
	if err != nil {
		t.Fatalf("ListNodeTags() error = %v", err)
	}
	if len(bTags["10.2.2.2:8388"]) != 1 || bTags["10.2.2.2:8388"][0] != "fast" {
		t.Errorf("b-stale 的标签不应被本分片 prune, got %v", bTags["10.2.2.2:8388"])
	}
}
