package aggregator

import (
	"context"
	"testing"

	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

// TestFullRefresh_DisabledAirportNodesGoStale 回归(Check H1):
// 被禁用的机场(fetchAirports 跳过)节点在全量刷新后必须下架(stale),
// 不得永久 active 持续下发订阅。
func TestFullRefresh_DisabledAirportNodesGoStale(t *testing.T) {
	agg, st := newTestAggregator(t)

	srv := subscriptionServer(t)
	ap, err := st.CreateAirport("被禁机场", srv.URL)
	if err != nil {
		t.Fatalf("CreateAirport: %v", err)
	}
	// 第一轮:节点入池
	if err := agg.RunOnce(context.Background(), store.RefreshTriggerManual); err != nil {
		t.Fatalf("RunOnce first: %v", err)
	}
	if len(agg.Nodes()) == 0 {
		t.Fatal("first round: pool empty, want nodes")
	}

	// 禁用机场后第二轮:节点应 stale(合法消失)
	if err := st.SetAirportEnabled(ap.ID, false); err != nil {
		t.Fatalf("SetAirportEnabled: %v", err)
	}
	if err := agg.RunOnce(context.Background(), store.RefreshTriggerManual); err != nil {
		t.Fatalf("RunOnce second: %v", err)
	}
	nodes := agg.Nodes()
	if len(nodes) == 0 {
		t.Fatal("second round: pool empty, want stale-retained nodes")
	}
	for _, n := range nodes {
		if n.Source == "被禁机场" && !n.Stale {
			t.Errorf("disabled airport node %s active after full refresh, want stale", n.Name)
		}
	}
}

// TestFullRefresh_DeletedAirportNodesGoStale 回归(Check H1):
// 被删除的机场(DeleteAirport 不清池)节点在下轮全量刷新后必须下架。
func TestFullRefresh_DeletedAirportNodesGoStale(t *testing.T) {
	agg, st := newTestAggregator(t)

	srv := subscriptionServer(t)
	ap, err := st.CreateAirport("被删机场", srv.URL)
	if err != nil {
		t.Fatalf("CreateAirport: %v", err)
	}
	if err := agg.RunOnce(context.Background(), store.RefreshTriggerManual); err != nil {
		t.Fatalf("RunOnce first: %v", err)
	}
	if len(agg.Nodes()) == 0 {
		t.Fatal("first round: pool empty, want nodes")
	}

	if err := st.DeleteAirport(ap.ID); err != nil {
		t.Fatalf("DeleteAirport: %v", err)
	}
	if err := agg.RunOnce(context.Background(), store.RefreshTriggerManual); err != nil {
		t.Fatalf("RunOnce second: %v", err)
	}
	nodes := agg.Nodes()
	if len(nodes) == 0 {
		t.Fatal("second round: pool empty, want stale-retained nodes")
	}
	for _, n := range nodes {
		if n.Source == "被删机场" && !n.Stale {
			t.Errorf("deleted airport node %s active after full refresh, want stale", n.Name)
		}
	}
}

// TestFullRefresh_RenamedAirportOldNameNodesGoStale 回归(Check H1):
// 机场改名后,Source=旧名的节点属合法消失,下轮全量刷新标 stale。
func TestFullRefresh_RenamedAirportOldNameNodesGoStale(t *testing.T) {
	agg, st := newTestAggregator(t)

	srv := subscriptionServer(t)
	ap, err := st.CreateAirport("旧名机场", srv.URL)
	if err != nil {
		t.Fatalf("CreateAirport: %v", err)
	}
	if err := agg.RunOnce(context.Background(), store.RefreshTriggerManual); err != nil {
		t.Fatalf("RunOnce first: %v", err)
	}

	if err := st.UpdateAirport(ap.ID, "新名机场", srv.URL, ""); err != nil {
		t.Fatalf("UpdateAirport: %v", err)
	}
	if err := agg.RunOnce(context.Background(), store.RefreshTriggerManual); err != nil {
		t.Fatalf("RunOnce second: %v", err)
	}
	for _, n := range agg.Nodes() {
		if n.Source == "旧名机场" && !n.Stale {
			t.Errorf("old-name node %s active after rename+refresh, want stale", n.Name)
		}
	}
}

// TestMergePerSource_RestDedupedByNodeKey 回归(Check M1):
// rest(保留侧)与新拉取集同 NodeKey 的旧节点丢弃,新拉取集优先,不双份。
func TestMergePerSource_RestDedupedByNodeKey(t *testing.T) {
	dup := &subscription.Node{Name: "手动 01", Type: "trojan", Server: "dup.example.com", Port: 443, Password: "old", Source: "手动机场"}
	fresh := &subscription.Node{Name: "拉取 01", Type: "trojan", Server: "dup.example.com", Port: 443, Password: "new", Source: "拉取机场"}
	keep := &subscription.Node{Name: "手动 02", Type: "trojan", Server: "keep.example.com", Port: 443, Password: "pw", Source: "手动机场"}

	fetched := &fetchResult{
		airportNodes: map[string][]*subscription.Node{"拉取机场": {fresh}},
		allNodes:     []*subscription.Node{fresh},
		preserve:     map[string]bool{"手动机场": true},
	}
	out := mergePerSource([]*subscription.Node{dup, keep}, []*subscription.Node{fresh}, fetched, false)

	byKey := make(map[string]*subscription.Node, len(out))
	for _, n := range out {
		if _, exists := byKey[n.NodeKey()]; exists {
			t.Errorf("duplicate NodeKey %s in merged pool", n.NodeKey())
		}
		byKey[n.NodeKey()] = n
	}
	if len(out) != 2 {
		t.Fatalf("merged = %d nodes, want 2 (同 key 去重)", len(out))
	}
	if got := byKey["dup.example.com:443"]; got == nil || got.Source != "拉取机场" || got.Password != "new" {
		t.Errorf("dup key winner = %+v, want 新拉取集节点", got)
	}
	if byKey["keep.example.com:443"] == nil {
		t.Error("preserved manual node dropped, want kept")
	}
}
