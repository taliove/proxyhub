package store

import (
	"testing"

	"github.com/taliove/proxyhub/internal/subscription"
)

func poolSample() []*subscription.Node {
	return []*subscription.Node{
		{Name: "香港01", Type: "ss", Server: "1.1.1.1", Port: 8388,
			Cipher: "aes-256-gcm", Password: "p1", Region: "HK", Source: "机场A",
			Available: true, Latency: 50},
		{Name: "日本01", Type: "vmess", Server: "2.2.2.2", Port: 443,
			UUID: "uuid-2", AlterID: 0, Cipher: "auto", Network: "ws", TLS: true,
			Region: "JP", Source: "机场A", Available: true, Latency: 80},
		{Name: "自建", Type: "trojan", Server: "3.3.3.3", Port: 443,
			Password: "p3", TLS: true, Region: "US",
			Source: subscription.SourceSelfHosted, Available: true, Latency: 120},
	}
}

func TestNodePool_SaveAndLoad(t *testing.T) {
	st := newTestStore(t)

	want := poolSample()
	if err := st.SaveNodePool(want); err != nil {
		t.Fatalf("SaveNodePool() error = %v", err)
	}

	got, err := st.LoadNodePool()
	if err != nil {
		t.Fatalf("LoadNodePool() error = %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("LoadNodePool() len = %d, want %d", len(got), len(want))
	}

	// 顺序必须保持（订阅生成依赖稳定顺序）
	for i := range want {
		if got[i].Name != want[i].Name {
			t.Errorf("node[%d].Name = %q, want %q", i, got[i].Name, want[i].Name)
		}
		if got[i].Server != want[i].Server || got[i].Port != want[i].Port {
			t.Errorf("node[%d] endpoint = %s:%d, want %s:%d",
				i, got[i].Server, got[i].Port, want[i].Server, want[i].Port)
		}
		if got[i].Type != want[i].Type {
			t.Errorf("node[%d].Type = %q, want %q", i, got[i].Type, want[i].Type)
		}
		if got[i].TLS != want[i].TLS {
			t.Errorf("node[%d].TLS = %v, want %v", i, got[i].TLS, want[i].TLS)
		}
		if got[i].Source != want[i].Source {
			t.Errorf("node[%d].Source = %q, want %q", i, got[i].Source, want[i].Source)
		}
		if got[i].Latency != want[i].Latency || got[i].Available != want[i].Available {
			t.Errorf("node[%d] health = %d/%v, want %d/%v",
				i, got[i].Latency, got[i].Available, want[i].Latency, want[i].Available)
		}
	}
}

func TestNodePool_SaveReplacesPrevious(t *testing.T) {
	st := newTestStore(t)

	if err := st.SaveNodePool(poolSample()); err != nil {
		t.Fatalf("SaveNodePool() first error = %v", err)
	}
	// 第二次保存较小的池：upsert 语义下，旧节点标记为 stale，新节点为 active
	next := []*subscription.Node{
		{Name: "唯一", Type: "ss", Server: "9.9.9.9", Port: 1, Cipher: "aes-256-gcm", Password: "p"},
	}
	if err := st.SaveNodePool(next); err != nil {
		t.Fatalf("SaveNodePool() second error = %v", err)
	}

	got, err := st.LoadNodePool()
	if err != nil {
		t.Fatalf("LoadNodePool() error = %v", err)
	}
	// upsert 后：1 个 active（唯一）+ 3 个 stale（旧池的机场节点）= 4 个
	// 自建节点（poolSample 第3个）不参与 stale，会在第二轮被标记 stale（因为 Source 是自建）
	if len(got) != 4 {
		t.Errorf("pool len = %d, want 4 (1 active + 3 stale)", len(got))
	}
	// 第一个应该是 active 的"唯一"节点
	if got[0].Name != "唯一" || got[0].Stale {
		t.Errorf("pool[0] = %s(stale=%v), want 唯一(stale=false)", got[0].Name, got[0].Stale)
	}
	// 其余应全部 stale
	for i := 1; i < len(got); i++ {
		if !got[i].Stale {
			t.Errorf("pool[%d] (%s) Stale = false, want true", i, got[i].Name)
		}
	}
}

func TestNodePool_LoadEmpty(t *testing.T) {
	st := newTestStore(t)

	got, err := st.LoadNodePool()
	if err != nil {
		t.Fatalf("LoadNodePool() on empty error = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("LoadNodePool() on empty = %d nodes, want 0", len(got))
	}
}

func TestNodePool_SaveEmptyClears(t *testing.T) {
	st := newTestStore(t)

	if err := st.SaveNodePool(poolSample()); err != nil {
		t.Fatalf("SaveNodePool() error = %v", err)
	}
	// 保存空池：upsert 语义下，将所有现有节点标记为 stale（而非删除）
	if err := st.SaveNodePool(nil); err != nil {
		t.Fatalf("SaveNodePool(nil) error = %v", err)
	}
	got, _ := st.LoadNodePool()
	// 应有 3 个节点，全部 stale
	if len(got) != 3 {
		t.Errorf("after SaveNodePool(nil), pool len = %d, want 3 (全部 stale)", len(got))
	}
	for i, n := range got {
		if !n.Stale {
			t.Errorf("pool[%d] (%s) Stale = false, want true", i, n.Name)
		}
	}
}

func TestStore_AllNodeKeys(t *testing.T) {
	st := newTestStore(t)

	// Empty pool returns empty slice
	keys, err := st.AllNodeKeys()
	if err != nil {
		t.Fatalf("AllNodeKeys() on empty error = %v", err)
	}
	if len(keys) != 0 {
		t.Errorf("AllNodeKeys() on empty = %d keys, want 0", len(keys))
	}

	// Save pool and retrieve keys
	pool := poolSample()
	if err := st.SaveNodePool(pool); err != nil {
		t.Fatalf("SaveNodePool() error = %v", err)
	}

	keys, err = st.AllNodeKeys()
	if err != nil {
		t.Fatalf("AllNodeKeys() error = %v", err)
	}
	if len(keys) != len(pool) {
		t.Fatalf("AllNodeKeys() len = %d, want %d", len(keys), len(pool))
	}

	// Verify keys match NodeKey() computation
	for i, n := range pool {
		expected := n.NodeKey()
		found := false
		for _, k := range keys {
			if k == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("pool[%d] key %q not found in AllNodeKeys()", i, expected)
		}
	}
}
