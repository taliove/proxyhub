package aggregator

import (
	"testing"

	"github.com/taliove/proxyhub/internal/subscription"
)

// TestUpdateNodeTestResultPersistsToStore issue #33:手动/批量检测写回内存池的
// 同时单行落库——重启(从 DB 快照恢复)后可用性不归零,与机场 URL 可达性解耦。
func TestUpdateNodeTestResultPersistsToStore(t *testing.T) {
	agg, st := newTestAggregator(t)

	node := &subscription.Node{
		Name: "香港A", Type: "ss", Server: "a.example.com", Port: 8388,
		Cipher: "aes-256-gcm", Password: "p", Source: "机场甲",
	}
	// 池与库同在上轮全量刷新落位(未检测:available=0, detection_kind 空)
	agg.SetNodesForUser(0, []*subscription.Node{node})
	if err := st.SaveNodePool([]*subscription.Node{node}); err != nil {
		t.Fatalf("SaveNodePool: %v", err)
	}

	// 手动检测写回(quick 判可用,延迟 88ms)
	if !agg.UpdateNodeTestResultForUser(0, node.NodeKey(), "quick", true, 88, 0, 0, "", "") {
		t.Fatal("pool writeback should hit")
	}

	// 模拟重启:从 DB 快照重建池,检测状态必须还在
	restored, err := st.LoadNodePool()
	if err != nil {
		t.Fatalf("LoadNodePool: %v", err)
	}
	if len(restored) != 1 {
		t.Fatalf("restored %d nodes, want 1", len(restored))
	}
	got := restored[0]
	if !got.Available || got.Latency != 88 {
		t.Errorf("restored availability = %v/%dms, want true/88ms", got.Available, got.Latency)
	}
	if got.DetectionKind != subscription.DetectionKindHealth {
		t.Errorf("restored detection kind = %q, want health", got.DetectionKind)
	}
	if got.DetectionLastCheck.IsZero() {
		t.Error("restored detection_last_check should be set")
	}

	// 失败写回:分类与详情同样落库
	if !agg.UpdateNodeTestResultForUser(0, node.NodeKey(), "real", false, 0, 0, 0, "timeout", "dial tcp: i/o timeout") {
		t.Fatal("failure writeback should hit")
	}
	restored, _ = st.LoadNodePool()
	got = restored[0]
	if got.Available || got.DetectionKind != subscription.DetectionKindReal {
		t.Errorf("restored after failure = available %v kind %q, want false/real", got.Available, got.DetectionKind)
	}
	if got.DetectionFailReason != "timeout" {
		t.Errorf("restored fail reason = %q, want timeout", got.DetectionFailReason)
	}

	// 带宽模式:只动带宽列,不碰检测字段
	if !agg.UpdateNodeTestResultForUser(0, node.NodeKey(), "bandwidth", false, 0, 123.4, 45.6, "", "") {
		t.Fatal("bandwidth writeback should hit")
	}
	restored, _ = st.LoadNodePool()
	got = restored[0]
	if got.BandwidthDownMbps != 123.4 || got.BandwidthUpMbps != 45.6 {
		t.Errorf("restored bandwidth = %.1f/%.1f, want 123.4/45.6", got.BandwidthDownMbps, got.BandwidthUpMbps)
	}
	if got.DetectionKind != subscription.DetectionKindReal {
		t.Errorf("bandwidth mode must not touch detection kind, got %q", got.DetectionKind)
	}

	// 未入库的节点(从未成功刷新):写回内存即可,落库静默跳过不报错
	ghost := &subscription.Node{
		Name: "幽灵", Type: "ss", Server: "ghost.example.com", Port: 443,
		Cipher: "aes-256-gcm", Password: "p", Source: "机场甲",
	}
	agg.SetNodesForUser(0, []*subscription.Node{node, ghost})
	if !agg.UpdateNodeTestResultForUser(0, ghost.NodeKey(), "quick", true, 10, 0, 0, "", "") {
		t.Fatal("ghost pool writeback should hit memory pool")
	}
}

// TestUpdateNodeTestResultMissNoPersist 未命中(节点不在池)时返回 false 且不写库。
func TestUpdateNodeTestResultMissNoPersist(t *testing.T) {
	agg, st := newTestAggregator(t)
	if agg.UpdateNodeTestResultForUser(0, "none.example.com:443", "quick", true, 1, 0, 0, "", "") {
		t.Fatal("miss should return false")
	}
	restored, err := st.LoadNodePool()
	if err != nil || len(restored) != 0 {
		t.Errorf("store should stay empty, got %d nodes, err %v", len(restored), err)
	}
}
