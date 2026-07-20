package store

import (
	"reflect"
	"testing"

	"github.com/taliove/proxyhub/internal/detection"
	"github.com/taliove/proxyhub/internal/subscription"
)

// TestNodeTagsTableMigrated 证明 012 迁移接入迁移链路(裸库 Open 后 node_tags 表即存在)。
func TestNodeTagsTableMigrated(t *testing.T) {
	st := newTestStore(t)
	var name string
	err := st.db.QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name='node_tags'`).Scan(&name)
	if err != nil {
		t.Fatalf("node_tags table missing after migration: %v", err)
	}
}

// TestReplaceNodeTags_FullReplaceIdempotent 全量替换=幂等:重复替换结果一致,旧标签被清掉。
func TestReplaceNodeTags_FullReplaceIdempotent(t *testing.T) {
	st := newTestStore(t)
	key := "example.com:443"

	if err := st.ReplaceNodeTags(key, []string{"nf-full", "openai"}); err != nil {
		t.Fatalf("ReplaceNodeTags: %v", err)
	}
	// 重复相同输入应幂等(不因 PK 冲突报错)。
	if err := st.ReplaceNodeTags(key, []string{"nf-full", "openai"}); err != nil {
		t.Fatalf("ReplaceNodeTags idempotent: %v", err)
	}
	got, err := st.ListNodeTags([]string{key})
	if err != nil {
		t.Fatalf("ListNodeTags: %v", err)
	}
	if !reflect.DeepEqual(got[key], []string{"nf-full", "openai"}) {
		t.Fatalf("tags = %v, want [nf-full openai]", got[key])
	}

	// 新的一组标签整体替换,旧标签消失。
	if err := st.ReplaceNodeTags(key, []string{"residential"}); err != nil {
		t.Fatalf("ReplaceNodeTags replace: %v", err)
	}
	got, _ = st.ListNodeTags([]string{key})
	if !reflect.DeepEqual(got[key], []string{"residential"}) {
		t.Fatalf("after replace tags = %v, want [residential]", got[key])
	}
}

// TestReplaceNodeTags_EmptyClears 空标签集清空该节点标签(幂等,非报错)。
func TestReplaceNodeTags_EmptyClears(t *testing.T) {
	st := newTestStore(t)
	key := "example.com:443"
	if err := st.ReplaceNodeTags(key, []string{"fast"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := st.ReplaceNodeTags(key, nil); err != nil {
		t.Fatalf("ReplaceNodeTags nil: %v", err)
	}
	got, _ := st.ListNodeTags([]string{key})
	if len(got[key]) != 0 {
		t.Fatalf("empty replace should clear, got %v", got[key])
	}
}

// TestListNodeTags_MultiKey 多节点各自标签互不串台;无标签节点缺省不出现在结果中。
func TestListNodeTags_MultiKey(t *testing.T) {
	st := newTestStore(t)
	if err := st.ReplaceNodeTags("a.example.com:443", []string{"nf-full"}); err != nil {
		t.Fatalf("seed a: %v", err)
	}
	if err := st.ReplaceNodeTags("b.example.com:443", []string{"claude", "fast"}); err != nil {
		t.Fatalf("seed b: %v", err)
	}
	got, err := st.ListNodeTags([]string{"a.example.com:443", "b.example.com:443", "c.example.com:443"})
	if err != nil {
		t.Fatalf("ListNodeTags: %v", err)
	}
	if !reflect.DeepEqual(got["a.example.com:443"], []string{"nf-full"}) {
		t.Errorf("a tags = %v", got["a.example.com:443"])
	}
	if !reflect.DeepEqual(got["b.example.com:443"], []string{"claude", "fast"}) {
		t.Errorf("b tags = %v", got["b.example.com:443"])
	}
	if _, ok := got["c.example.com:443"]; ok {
		t.Errorf("c has no tags, should be absent from map")
	}
}

// TestRecomputeNodeTags_FromDetectionAndExam 编排读取:解锁判定 + 体检报告 -> 派生 -> 落库。
func TestRecomputeNodeTags_FromDetectionAndExam(t *testing.T) {
	st := newTestStore(t)
	key := "example.com:443"

	// 名称->kind 需靠检测目标配置解析。
	if err := st.SetDetectionTargets(detection.DefaultUnlockTargets()); err != nil {
		t.Fatalf("SetDetectionTargets: %v", err)
	}

	// 解锁判定落 node_health(target_name 为配置里的展示名)。
	results := []detection.Result{
		{NodeKey: key, TargetName: "Netflix", Available: true, Latency: 88, Level: detection.LevelFull, Region: "US"},
		{NodeKey: key, TargetName: "OpenAI", Available: true, Latency: 120, Level: detection.LevelFull},
		{NodeKey: key, TargetName: "connectivity", Available: true, Latency: 30}, // generic:不成解锁标签
	}
	if err := st.SaveDetectionResults(results, "hk01", "airport"); err != nil {
		t.Fatalf("SaveDetectionResults: %v", err)
	}

	// 体检报告:egress + 稳定性 + 基准测速。
	report := detection.ExamReport{
		Stability:   &detection.StabilityMetrics{Total: 30, Succeeded: 30, Score: 85},
		RegionSpeed: &detection.RegionSpeedMetrics{Regions: []detection.RegionResult{{Code: "baseline", DownMbps: 80}}},
		Egress: &detection.EgressMetrics{
			IPv4: &detection.EgressIPv4{IP: "203.0.113.9", CountryCode: "US", Hosting: true},
			DNS:  &detection.EgressDNS{Leak: true},
		},
	}
	if err := st.SaveExamHistory(key, report); err != nil {
		t.Fatalf("SaveExamHistory: %v", err)
	}

	if err := st.RecomputeNodeTags(key); err != nil {
		t.Fatalf("RecomputeNodeTags: %v", err)
	}
	got, _ := st.ListNodeTags([]string{key})
	want := []string{"dns-leak", "fast", "hosting", "nf-full", "openai", "region:US", "stable-good"}
	if !reflect.DeepEqual(got[key], want) {
		t.Fatalf("recomputed tags = %v, want %v", got[key], want)
	}

	// 幂等:再算一次结果不变。
	if err := st.RecomputeNodeTags(key); err != nil {
		t.Fatalf("RecomputeNodeTags 2: %v", err)
	}
	got2, _ := st.ListNodeTags([]string{key})
	if !reflect.DeepEqual(got2[key], want) {
		t.Fatalf("recompute not idempotent: %v vs %v", got2[key], want)
	}
}

// TestRecomputeNodeTags_NoDataClears 无检测无体检的节点重算 -> 零标签,并清掉历史残留标签。
func TestRecomputeNodeTags_NoDataClears(t *testing.T) {
	st := newTestStore(t)
	key := "example.com:443"
	if err := st.ReplaceNodeTags(key, []string{"stale-tag"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := st.RecomputeNodeTags(key); err != nil {
		t.Fatalf("RecomputeNodeTags: %v", err)
	}
	got, _ := st.ListNodeTags([]string{key})
	if len(got[key]) != 0 {
		t.Fatalf("no-data recompute should clear tags, got %v", got[key])
	}
}

// TestSaveNodePool_PrunesStaleNodeTags 刷新时消失的节点(标记 stale)标签被清理。
func TestSaveNodePool_PrunesStaleNodeTags(t *testing.T) {
	st := newTestStore(t)
	keep := &subscription.Node{Name: "keep", Server: "keep.example.com", Port: 443, Type: "vmess"}
	drop := &subscription.Node{Name: "drop", Server: "drop.example.com", Port: 443, Type: "vmess"}

	// 首轮:两个节点都在池中,各打标签。
	if err := st.SaveNodePool([]*subscription.Node{keep, drop}); err != nil {
		t.Fatalf("SaveNodePool round1: %v", err)
	}
	if err := st.ReplaceNodeTags(keep.NodeKey(), []string{"nf-full"}); err != nil {
		t.Fatalf("tag keep: %v", err)
	}
	if err := st.ReplaceNodeTags(drop.NodeKey(), []string{"openai"}); err != nil {
		t.Fatalf("tag drop: %v", err)
	}

	// 次轮:drop 从订阅消失(标记 stale),其标签应被清理;keep 标签保留。
	if err := st.SaveNodePool([]*subscription.Node{keep}); err != nil {
		t.Fatalf("SaveNodePool round2: %v", err)
	}
	got, _ := st.ListNodeTags([]string{keep.NodeKey(), drop.NodeKey()})
	if !reflect.DeepEqual(got[keep.NodeKey()], []string{"nf-full"}) {
		t.Errorf("keep tags = %v, want [nf-full]", got[keep.NodeKey()])
	}
	if len(got[drop.NodeKey()]) != 0 {
		t.Errorf("stale node tags should be pruned, got %v", got[drop.NodeKey()])
	}
}
