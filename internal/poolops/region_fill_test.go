package poolops

import (
	"context"
	"strings"
	"testing"

	"github.com/taliove/proxyhub/internal/region"
	"github.com/taliove/proxyhub/internal/subscription"
)

// TestUpsertAirportNodes_FillsRegion 单机场 upsert 第一步走统一三层识别器:
// L1 规则表 / L2 emoji 反解 / 三层全空 Unknown,Region 在入池前填好(issue #37)。
// 恒假的 Region=="" 死 guard 已随旧 geoip 兜底一并删除。
func TestUpsertAirportNodes_FillsRegion(t *testing.T) {
	_, st := newTestAdapter(t)

	rec := region.New(region.Deps{
		RecognizeName: func(name string) string {
			if strings.Contains(name, "香港") {
				return "HK"
			}
			return "Unknown"
		},
		// LookupHost nil:域名不进 L3 DNS,测试密闭
		LookupCountry: func(ip string) (string, error) { return "", nil },
	})
	adapter := NewStoreAdapter(st, rec)

	nodes := []*subscription.Node{
		{Name: "香港 01", Type: "trojan", Server: "hk1.example.com", Port: 443, Source: "airport-a"},
		{Name: "🇬🇺 关岛 02", Type: "trojan", Server: "gu1.example.com", Port: 443, Source: "airport-a"},
		{Name: "节点 03", Type: "trojan", Server: "node3.example.com", Port: 443, Source: "airport-a"},
	}
	if err := adapter.UpsertAirportNodes(context.Background(), "airport-a", nodes); err != nil {
		t.Fatalf("UpsertAirportNodes() error = %v", err)
	}

	want := map[string]string{
		"hk1.example.com:443":   "HK",
		"gu1.example.com:443":   "GU",
		"node3.example.com:443": "Unknown",
	}
	pool, err := st.LoadNodePool()
	if err != nil {
		t.Fatalf("LoadNodePool() error = %v", err)
	}
	if len(pool) != len(want) {
		t.Fatalf("pool has %d nodes, want %d", len(pool), len(want))
	}
	for _, n := range pool {
		if n.Region != want[n.NodeKey()] {
			t.Errorf("node %s Region = %q, want %q", n.NodeKey(), n.Region, want[n.NodeKey()])
		}
	}
}
