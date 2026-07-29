package aggregator

import (
	"testing"

	"github.com/taliove/proxyhub/internal/subscription"
)

// TestMergePartialOnCancel_SelfHostedSurvives 回归(Check H1R):
// 取消路径 includeSelfHosted=false,自建节点不得被路由进 MergePool
// (其自建豁免会把它直接丢弃)——取消一次全量刷新,自建 FailBack 节点
// 必须在内存池与 DB 快照双双原样保留且不 stale。
func TestMergePartialOnCancel_SelfHostedSurvives(t *testing.T) {
	agg, st := newTestAggregator(t)

	selfNode := &subscription.Node{
		Name: "自建 01", Type: "trojan", Server: "self1.example.com", Port: 443,
		Password: "pw", Source: subscription.SourceSelfHosted, Region: "SELF", Available: true,
	}
	oldX := &subscription.Node{
		Name: "X 01", Type: "trojan", Server: "node1.example.com", Port: 443,
		Password: "pw", Region: "HK", Source: "机场X",
	}
	if err := st.SaveNodePool([]*subscription.Node{selfNode, oldX}); err != nil {
		t.Fatalf("SaveNodePool: %v", err)
	}
	agg.restoreNodePool()

	// 取消时刻:X 拉成功,无自建注入(取消路径语义)
	newX := &subscription.Node{
		Name: "X 01", Type: "trojan", Server: "node1.example.com", Port: 443,
		Password: "pw", Region: "HK", Source: "机场X",
	}
	fetched := &fetchResult{
		airportNodes: map[string][]*subscription.Node{"机场X": {newX}},
		allNodes:     []*subscription.Node{newX},
		enabled:      1,
		preserve:     map[string]bool{},
	}

	agg.mergePartialOnCancel(&runLog{}, fetched)

	var mem *subscription.Node
	for _, n := range agg.Nodes() {
		if n.Source == subscription.SourceSelfHosted {
			mem = n
		}
	}
	if mem == nil {
		t.Fatal("self-hosted node vanished from memory pool after cancel (H1R)")
	}
	if mem.Stale {
		t.Error("self-hosted node marked stale after cancel, want active")
	}

	persisted, err := st.LoadNodePool()
	if err != nil {
		t.Fatalf("LoadNodePool: %v", err)
	}
	var dbNode *subscription.Node
	for _, n := range persisted {
		if n.Source == subscription.SourceSelfHosted {
			dbNode = n
		}
	}
	if dbNode == nil {
		t.Fatal("self-hosted node vanished from DB snapshot after cancel (H1R)")
	}
	if dbNode.Stale {
		t.Error("self-hosted node stale in DB after cancel, want active")
	}
}

// TestMergePerSource_SuccessPathSelfHostedStillMerged 反向护栏:
// 成功路径(includeSelfHosted=true)自建节点仍进 MergePool 随注入 carry-forward,
// H1R 修复不得把成功路径的自建也改走保留侧(否则会与注入节点双份)。
func TestMergePerSource_SuccessPathSelfHostedStillMerged(t *testing.T) {
	oldSelf := &subscription.Node{
		Name: "自建 01", Type: "trojan", Server: "self1.example.com", Port: 443,
		Password: "old", Source: subscription.SourceSelfHosted, Available: true,
	}
	injected := &subscription.Node{
		Name: "自建 01", Type: "trojan", Server: "self1.example.com", Port: 443,
		Password: "new", Source: subscription.SourceSelfHosted,
	}
	fetched := &fetchResult{
		airportNodes: map[string][]*subscription.Node{},
		preserve:     map[string]bool{},
	}
	out := mergePerSource([]*subscription.Node{oldSelf}, []*subscription.Node{injected}, fetched, true)
	if len(out) != 1 {
		t.Fatalf("success path: merged = %d nodes, want 1 (注入与旧自建不双份)", len(out))
	}
	if out[0].Password != "new" {
		t.Errorf("success path: Password = %q, want new (注入覆盖,carry-forward 语义)", out[0].Password)
	}
}
