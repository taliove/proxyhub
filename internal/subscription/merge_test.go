package subscription

import (
	"testing"
	"time"
)

func TestMergePool_CarryForwardDetectionState(t *testing.T) {
	detectionTime := time.Now().Add(-1 * time.Hour)

	oldPool := []*Node{
		{
			Name: "香港01", Type: "ss", Server: "1.1.1.1", Port: 8388,
			Region: "HK", Source: "机场A",
			Available: false, Latency: 999, // 真实检测确认不可用
			DetectionLastCheck: detectionTime,
		},
	}

	// 新 fetch：同 NodeKey 但连接参数可能变化
	newPool := []*Node{
		{
			Name: "香港01-新名", Type: "ss", Server: "1.1.1.1", Port: 8388,
			Cipher: "new-cipher", Password: "new-pass", // 连接参数更新
			Region: "HK", Source: "机场A",
			Available: true, Latency: 50, // fetch 占位值（应被旧检测状态覆盖）
			DetectionLastCheck: time.Time{}, // 零值（未检测）
		},
	}

	result := MergePool(oldPool, newPool)

	if len(result) != 1 {
		t.Fatalf("len = %d, want 1", len(result))
	}

	node := result[0]
	// 连接参数应更新（新 fetch 值）
	if node.Cipher != "new-cipher" || node.Password != "new-pass" {
		t.Errorf("连接参数未更新: cipher=%s password=%s", node.Cipher, node.Password)
	}
	// 检测状态应保留（旧池值）
	if node.Available != false || node.Latency != 999 {
		t.Errorf("检测状态未保留: Available=%v Latency=%d, want false/999", node.Available, node.Latency)
	}
	if !node.DetectionLastCheck.Equal(detectionTime) {
		t.Errorf("DetectionLastCheck 未保留")
	}
	// Stale 应为 false（active 节点）
	if node.Stale {
		t.Errorf("Stale = true, want false")
	}
}

func TestMergePool_MarksStaleMissingAirportNodes(t *testing.T) {
	lastSeen := time.Now().Add(-24 * time.Hour)

	oldPool := []*Node{
		{Name: "香港01", Server: "1.1.1.1", Port: 8388, Source: "机场A", LastSeen: lastSeen},
		{Name: "日本01", Server: "2.2.2.2", Port: 443, Source: "机场A", LastSeen: lastSeen},
		{Name: "自建", Server: "3.3.3.3", Port: 443, Source: SourceSelfHosted},
	}

	// 新池：只有香港节点（日本消失，自建由注入保证在场）
	newPool := []*Node{
		{Name: "香港01", Server: "1.1.1.1", Port: 8388, Source: "机场A"},
		{Name: "自建", Server: "3.3.3.3", Port: 443, Source: SourceSelfHosted},
	}

	result := MergePool(oldPool, newPool)

	// 应有 3 个节点：香港(active) + 自建(active) + 日本(stale)
	if len(result) != 3 {
		t.Fatalf("len = %d, want 3", len(result))
	}

	// 前两个是 active（香港 + 自建），最后一个是 stale（日本）
	for i, n := range result[:2] {
		if n.Stale {
			t.Errorf("result[%d] (%s) Stale = true, want false (active)", i, n.Name)
		}
	}

	staleNode := result[2]
	if staleNode.Server != "2.2.2.2" {
		t.Fatalf("stale 节点 server = %s, want 2.2.2.2", staleNode.Server)
	}
	if !staleNode.Stale {
		t.Errorf("日本节点 Stale = false, want true")
	}
	if !staleNode.LastSeen.Equal(lastSeen) {
		t.Errorf("日本节点 LastSeen 未保留旧值")
	}
}

func TestMergePool_SelfHostedNeverStale(t *testing.T) {
	oldPool := []*Node{
		{Name: "自建", Server: "1.1.1.1", Port: 443, Source: SourceSelfHosted},
	}

	// 新池：空（自建节点由聚合注入，这里模拟"fetch 后未注入前"的状态）
	newPool := []*Node{}

	result := MergePool(oldPool, newPool)

	// 自建节点不应出现在结果中（因为它不参与 stale 逻辑，由聚合注入保证在场）
	if len(result) != 0 {
		t.Errorf("len = %d, want 0 (自建节点不参与 stale)", len(result))
	}
}

func TestMergePool_BandwidthCarryForward(t *testing.T) {
	bandwidthTime := time.Now().Add(-30 * time.Minute)

	oldPool := []*Node{
		{
			Name: "test", Server: "1.1.1.1", Port: 8388, Source: "机场A",
			BandwidthDownMbps: 123.45,
			BandwidthUpMbps:   67.89,
			BandwidthCheck:    bandwidthTime,
		},
	}

	newPool := []*Node{
		{Name: "test", Server: "1.1.1.1", Port: 8388, Source: "机场A"},
	}

	result := MergePool(oldPool, newPool)

	if len(result) != 1 {
		t.Fatalf("len = %d, want 1", len(result))
	}

	node := result[0]
	if node.BandwidthDownMbps != 123.45 || node.BandwidthUpMbps != 67.89 {
		t.Errorf("带宽未 carry forward: down=%.2f up=%.2f", node.BandwidthDownMbps, node.BandwidthUpMbps)
	}
	if !node.BandwidthCheck.Equal(bandwidthTime) {
		t.Errorf("BandwidthCheck 未保留")
	}
}

func TestMergePool_EmptyOldPool(t *testing.T) {
	newPool := []*Node{
		{Name: "新节点", Server: "1.1.1.1", Port: 8388, Source: "机场A"},
	}

	result := MergePool(nil, newPool)

	if len(result) != 1 {
		t.Fatalf("len = %d, want 1", len(result))
	}
	if result[0].Stale {
		t.Errorf("新节点 Stale = true, want false")
	}
}

func TestMergePool_EmptyNewPool(t *testing.T) {
	oldPool := []*Node{
		{Name: "旧节点", Server: "1.1.1.1", Port: 8388, Source: "机场A"},
	}

	result := MergePool(oldPool, nil)

	// 旧池全部机场节点标记 stale
	if len(result) != 1 {
		t.Fatalf("len = %d, want 1", len(result))
	}
	if !result[0].Stale {
		t.Errorf("旧节点 Stale = false, want true")
	}
}

func TestMergePool_OrderStable(t *testing.T) {
	oldPool := []*Node{
		{Name: "A", Server: "1.1.1.1", Port: 1, Source: "机场A"},
		{Name: "B", Server: "2.2.2.2", Port: 2, Source: "机场A"},
	}

	// 新池顺序：B A（反过来）
	newPool := []*Node{
		{Name: "B", Server: "2.2.2.2", Port: 2, Source: "机场A"},
		{Name: "A", Server: "1.1.1.1", Port: 1, Source: "机场A"},
	}

	result := MergePool(oldPool, newPool)

	// 结果顺序应跟新池一致：B A
	if len(result) != 2 {
		t.Fatalf("len = %d, want 2", len(result))
	}
	if result[0].Name != "B" || result[1].Name != "A" {
		t.Errorf("顺序 = [%s %s], want [B A]", result[0].Name, result[1].Name)
	}
}
