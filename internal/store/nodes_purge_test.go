package store

import (
	"testing"

	"github.com/taliove/proxyhub/internal/subscription"
)

// purgePoolSample 机场节点 + 自建节点混合池(fixture 一律 example.com + 全零 UUID)。
func purgePoolSample() []*subscription.Node {
	return []*subscription.Node{
		{Name: "香港01", Type: "ss", Server: "hk01.example.com", Port: 8388,
			Cipher: "aes-256-gcm", Password: "p1", Region: "HK", Source: "机场A",
			Available: true, Latency: 50},
		{Name: "日本01", Type: "vmess", Server: "jp01.example.com", Port: 443,
			UUID: "00000000-0000-0000-0000-000000000000", AlterID: 0, Cipher: "auto",
			Network: "ws", TLS: true, Region: "JP", Source: "机场B",
			Available: true, Latency: 80},
		{Name: "自建", Type: "trojan", Server: "self.example.com", Port: 443,
			Password: "p3", TLS: true, Region: "US",
			Source: subscription.SourceSelfHosted, Available: true, Latency: 120},
	}
}

func airportKeysOf(t *testing.T, pool []*subscription.Node) (airport []string, self []string) {
	t.Helper()
	for _, n := range pool {
		if n.Source == subscription.SourceSelfHosted {
			self = append(self, n.NodeKey())
		} else {
			airport = append(airport, n.NodeKey())
		}
	}
	return airport, self
}

// TestDeleteAirportNodes 验证双清中的 DB 半区:机场节点删除、自建豁免,
// 屏蔽名单/名称覆盖保留,机场节点自动标签级联删除。
func TestDeleteAirportNodes(t *testing.T) {
	st := newTestStore(t)

	pool := purgePoolSample()
	if err := st.SaveNodePool(pool); err != nil {
		t.Fatalf("SaveNodePool() error = %v", err)
	}
	airportKeys, selfKeys := airportKeysOf(t, pool)
	if len(airportKeys) != 2 || len(selfKeys) != 1 {
		t.Fatalf("sample split = %d airport / %d self, want 2/1", len(airportKeys), len(selfKeys))
	}

	// 关联数据:屏蔽名单、名称覆盖、自动标签(机场 + 自建各一)
	if err := st.BlockNode(airportKeys[0]); err != nil {
		t.Fatalf("BlockNode() error = %v", err)
	}
	if err := st.SetNodeOverride(airportKeys[1], "自定义名", "HK"); err != nil {
		t.Fatalf("SetNodeOverride() error = %v", err)
	}
	for _, key := range append(airportKeys, selfKeys...) {
		if err := st.ReplaceNodeTags(key, []string{"fast"}); err != nil {
			t.Fatalf("ReplaceNodeTags(%s) error = %v", key, err)
		}
	}

	removed, err := st.DeleteAirportNodes()
	if err != nil {
		t.Fatalf("DeleteAirportNodes() error = %v", err)
	}
	if removed != 2 {
		t.Errorf("removed = %d, want 2", removed)
	}

	// DB 只剩自建节点
	got, err := st.LoadNodePool()
	if err != nil {
		t.Fatalf("LoadNodePool() error = %v", err)
	}
	if len(got) != 1 || got[0].Source != subscription.SourceSelfHosted {
		t.Fatalf("LoadNodePool() = %+v, want only the self-hosted node", got)
	}

	// 屏蔽名单保留
	blocked, err := st.ListBlockedNodes()
	if err != nil {
		t.Fatalf("ListBlockedNodes() error = %v", err)
	}
	if !blocked[airportKeys[0]] {
		t.Error("node_blocks entry should survive purge")
	}

	// 名称/地区覆盖保留
	overrides, err := st.ListNodeOverrides()
	if err != nil {
		t.Fatalf("ListNodeOverrides() error = %v", err)
	}
	if _, ok := overrides[airportKeys[1]]; !ok {
		t.Error("node_overrides entry should survive purge")
	}

	// 机场节点标签级联删除;自建节点标签保留
	tags, err := st.ListNodeTags(append(airportKeys, selfKeys...))
	if err != nil {
		t.Fatalf("ListNodeTags() error = %v", err)
	}
	for _, key := range airportKeys {
		if len(tags[key]) != 0 {
			t.Errorf("airport node tags should be purged, got %v for %s", tags[key], key)
		}
	}
	if len(tags[selfKeys[0]]) != 1 {
		t.Errorf("self-hosted node tags should survive, got %v", tags[selfKeys[0]])
	}
}

// TestDeleteAirportNodes_EmptyPool 空池调用是 no-op,不报错。
func TestDeleteAirportNodes_EmptyPool(t *testing.T) {
	st := newTestStore(t)
	removed, err := st.DeleteAirportNodes()
	if err != nil {
		t.Fatalf("DeleteAirportNodes() error = %v", err)
	}
	if removed != 0 {
		t.Errorf("removed = %d, want 0", removed)
	}
}
