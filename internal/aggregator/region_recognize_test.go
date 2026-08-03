package aggregator

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

// TestRecognizeRegions_ThreeLayersAndStatsEvent 全量刷新的识别步骤:
// 三层各自命中的节点拿到正确地区码,识别统计事件照常落库(issue #37)。
func TestRecognizeRegions_ThreeLayersAndStatsEvent(t *testing.T) {
	agg, st := newTestAggregator(t)

	nodes := []*subscription.Node{
		{Name: "香港 01", Server: "hk1.example.com", Port: 443, Type: "trojan", Source: "机场A"},      // L1 规则表
		{Name: "🇲🇻 马尔代夫 02", Server: "mv1.example.com", Port: 443, Type: "trojan", Source: "机场A"}, // L2 emoji 反解(规则表未覆盖)
		{Name: "节点 03", Server: "node3.example.com", Port: 443, Type: "trojan", Source: "机场A"},    // 三层全空(密闭识别器无 DNS)-> Unknown
	}
	rl, err := agg.newRunLog(0, store.RefreshTriggerManual, 0)
	if err != nil {
		t.Fatalf("newRunLog() error = %v", err)
	}
	agg.recognizeRegions(context.Background(), rl, nodes)

	want := []string{"HK", "MV", "Unknown"}
	for i, w := range want {
		if nodes[i].Region != w {
			t.Errorf("nodes[%d].Region = %q, want %q", i, nodes[i].Region, w)
		}
	}

	events, err := st.ListRefreshEvents(rl.runID)
	if err != nil {
		t.Fatalf("ListRefreshEvents() error = %v", err)
	}
	found := false
	for _, e := range events {
		if !strings.Contains(e.Message, "地区识别完成") {
			continue
		}
		found = true
		var data map[string]any
		if err := json.Unmarshal([]byte(e.Data), &data); err != nil {
			t.Fatalf("event data not json: %v", err)
		}
		stats, ok := data["regions"].(map[string]any)
		if !ok {
			t.Fatalf("event data missing regions stats: %v", data)
		}
		for _, code := range []string{"HK", "MV", "Unknown"} {
			if stats[code] == nil {
				t.Errorf("stats missing %q: %v", code, stats)
			}
		}
	}
	if !found {
		t.Error("missing 地区识别完成 event")
	}
}

// TestRegionRecognition_PoolOpsMatchesFullRefresh 核心一致性义务(issue #37):
// 两条入池路径(全量刷新 recognizeRegions / 单机场 upsert poolops)同输入同结果。
// 两条路径共用同一识别器实例,地区口径不得再分叉。
func TestRegionRecognition_PoolOpsMatchesFullRefresh(t *testing.T) {
	agg, st := newTestAggregator(t)

	makeInput := func() []*subscription.Node {
		return []*subscription.Node{
			{Name: "香港 01", Server: "hk1.example.com", Port: 443, Type: "trojan", Source: "机场A"},    // L1
			{Name: "🇧🇹 不丹 02", Server: "bt1.example.com", Port: 443, Type: "trojan", Source: "机场A"}, // L2
			{Name: "节点 03", Server: "203.0.113.7", Port: 443, Type: "trojan", Source: "机场A"},        // L3 IP 字面量离线查
			{Name: "节点 04", Server: "node4.example.com", Port: 443, Type: "trojan", Source: "机场A"},  // 三层全空 -> Unknown
		}
	}

	// 路径 1:全量刷新的识别步骤
	fullNodes := makeInput()
	agg.recognizeRegions(context.Background(), &runLog{}, fullNodes)
	fullRegions := make(map[string]string, len(fullNodes))
	for _, n := range fullNodes {
		fullRegions[n.NodeKey()] = n.Region
	}

	// 路径 2:单机场 upsert(经 poolOps 装配,与生产同一路径)
	upsertNodes := makeInput()
	if err := agg.poolOps.UpsertAirportNodes(context.Background(), "机场A", upsertNodes); err != nil {
		t.Fatalf("UpsertAirportNodes() error = %v", err)
	}
	pool, err := st.LoadNodePool()
	if err != nil {
		t.Fatalf("LoadNodePool() error = %v", err)
	}
	if len(pool) != len(fullNodes) {
		t.Fatalf("pool has %d nodes, want %d", len(pool), len(fullNodes))
	}
	for _, n := range pool {
		want, ok := fullRegions[n.NodeKey()]
		if !ok {
			t.Fatalf("pool node %s not in full-refresh input", n.NodeKey())
		}
		if n.Region != want {
			t.Errorf("node %s: poolops Region = %q, full-refresh Region = %q (paths diverged)",
				n.NodeKey(), n.Region, want)
		}
	}
}
