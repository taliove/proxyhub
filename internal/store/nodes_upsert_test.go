package store

import (
	"testing"
	"time"

	"github.com/taliove/proxyhub/internal/subscription"
)

// TestNodePool_UpsertPreservesDetectionState 验证 upsert 能正确保存和读取带检测状态的节点
// （carry-forward 逻辑在 aggregator 层通过 MergePool 完成，store 只负责持久化）
func TestNodePool_UpsertPreservesDetectionState(t *testing.T) {
	st := newTestStore(t)

	detectionTime := time.Now().Add(-1 * time.Hour).Truncate(time.Second)

	// 保存带检测状态的节点
	pool := []*subscription.Node{
		{
			Name: "香港01", Type: "ss", Server: "1.1.1.1", Port: 8388,
			Cipher: "aes-256-gcm", Password: "p1", Region: "HK", Source: "机场A",
			Available: false, Latency: 999, // 真实检测确认不可用
			DetectionLastCheck: detectionTime,
		},
	}
	if err := st.SaveNodePool(pool); err != nil {
		t.Fatalf("SaveNodePool() error = %v", err)
	}

	// 读取回来，检测状态应完整保留
	got, err := st.LoadNodePool()
	if err != nil {
		t.Fatalf("LoadNodePool() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}

	node := got[0]
	if node.Available != false || node.Latency != 999 {
		t.Errorf("检测状态未保留: Available=%v Latency=%d, want false/999", node.Available, node.Latency)
	}
	if !node.DetectionLastCheck.Equal(detectionTime) {
		t.Errorf("DetectionLastCheck = %v, want %v", node.DetectionLastCheck, detectionTime)
	}
}

// TestNodePool_UpsertMarksStaleMissingNodes 验证 stale 标记能正确保存和读取
// （stale 标记由 MergePool 在 aggregator 层设置，store 只负责持久化）
func TestNodePool_UpsertMarksStaleMissingNodes(t *testing.T) {
	st := newTestStore(t)

	lastSeen := time.Now().Add(-24 * time.Hour).Truncate(time.Second)

	// 保存带 stale 标记的节点（模拟 aggregator 调用 MergePool 后的结果）
	pool := []*subscription.Node{
		{Name: "香港01", Type: "ss", Server: "1.1.1.1", Port: 8388, Region: "HK", Source: "机场A", Stale: false},
		{Name: "日本01", Type: "vmess", Server: "2.2.2.2", Port: 443, Region: "JP", Source: "机场A", Stale: true, LastSeen: lastSeen},
		{Name: "自建", Type: "trojan", Server: "3.3.3.3", Port: 443, Region: "SELF", Source: subscription.SourceSelfHosted, Stale: false},
	}
	if err := st.SaveNodePool(pool); err != nil {
		t.Fatalf("SaveNodePool() error = %v", err)
	}

	got, err := st.LoadNodePool()
	if err != nil {
		t.Fatalf("LoadNodePool() error = %v", err)
	}

	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}

	var hk, jp, self *subscription.Node
	for _, n := range got {
		switch n.Server {
		case "1.1.1.1":
			hk = n
		case "2.2.2.2":
			jp = n
		case "3.3.3.3":
			self = n
		}
	}

	if hk == nil || jp == nil || self == nil {
		t.Fatalf("缺失节点: hk=%v jp=%v self=%v", hk, jp, self)
	}

	// 香港节点：active
	if hk.Stale {
		t.Errorf("香港节点 Stale = true, want false")
	}
	// 日本节点：stale
	if !jp.Stale {
		t.Errorf("日本节点 Stale = false, want true")
	}
	if !jp.LastSeen.Equal(lastSeen) {
		t.Errorf("日本节点 LastSeen = %v, want %v", jp.LastSeen, lastSeen)
	}
	// 自建节点：active
	if self.Stale {
		t.Errorf("自建节点 Stale = true, want false")
	}
}

// TestNodePool_UpsertIdempotent 验证同一池连续 upsert 不会炸唯一索引
func TestNodePool_UpsertIdempotent(t *testing.T) {
	st := newTestStore(t)

	pool := []*subscription.Node{
		{Name: "test", Type: "ss", Server: "1.1.1.1", Port: 8388, Region: "HK", Source: "机场A"},
	}
	if err := st.SaveNodePool(pool); err != nil {
		t.Fatalf("SaveNodePool() first error = %v", err)
	}
	// 再次保存同一池应成功（幂等）
	if err := st.SaveNodePool(pool); err != nil {
		t.Fatalf("SaveNodePool() second error = %v (应幂等)", err)
	}

	got, _ := st.LoadNodePool()
	if len(got) != 1 {
		t.Errorf("重复 upsert 后 len = %d, want 1", len(got))
	}
}

// TestNodePool_UpsertOrderStable 验证 upsert 保持顺序稳定（position 列正确维护）
func TestNodePool_UpsertOrderStable(t *testing.T) {
	st := newTestStore(t)

	// 第一轮：A B C
	first := []*subscription.Node{
		{Name: "A", Type: "ss", Server: "1.1.1.1", Port: 1, Source: "机场A"},
		{Name: "B", Type: "ss", Server: "2.2.2.2", Port: 2, Source: "机场A"},
		{Name: "C", Type: "ss", Server: "3.3.3.3", Port: 3, Source: "机场A"},
	}
	if err := st.SaveNodePool(first); err != nil {
		t.Fatalf("SaveNodePool() first error = %v", err)
	}

	// 第二轮：C A（B 消失）—— upsert 后顺序应是 C A B(stale)
	second := []*subscription.Node{
		{Name: "C", Type: "ss", Server: "3.3.3.3", Port: 3, Source: "机场A"},
		{Name: "A", Type: "ss", Server: "1.1.1.1", Port: 1, Source: "机场A"},
	}
	if err := st.SaveNodePool(second); err != nil {
		t.Fatalf("SaveNodePool() second error = %v", err)
	}

	got, _ := st.LoadNodePool()
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}

	// 顺序：active 节点按 position 排列，stale 节点在尾部
	if got[0].Name != "C" || got[1].Name != "A" {
		t.Errorf("active 节点顺序 = [%s %s], want [C A]", got[0].Name, got[1].Name)
	}
	if got[2].Name != "B" || !got[2].Stale {
		t.Errorf("stale 节点 = %s(stale=%v), want B(stale=true)", got[2].Name, got[2].Stale)
	}
}

// TestNodePool_BandwidthRoundtrip 验证带宽测试结果持久化回环
func TestNodePool_BandwidthRoundtrip(t *testing.T) {
	st := newTestStore(t)

	now := time.Now().Truncate(time.Second)
	pool := []*subscription.Node{
		{
			Name: "test", Type: "ss", Server: "1.1.1.1", Port: 8388, Source: "机场A",
			BandwidthDownMbps: 123.45,
			BandwidthUpMbps:   67.89,
			BandwidthCheck:    now,
		},
	}
	if err := st.SaveNodePool(pool); err != nil {
		t.Fatalf("SaveNodePool() error = %v", err)
	}

	got, err := st.LoadNodePool()
	if err != nil {
		t.Fatalf("LoadNodePool() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}

	node := got[0]
	if node.BandwidthDownMbps != 123.45 || node.BandwidthUpMbps != 67.89 {
		t.Errorf("带宽 = down:%.2f up:%.2f, want down:123.45 up:67.89",
			node.BandwidthDownMbps, node.BandwidthUpMbps)
	}
	if !node.BandwidthCheck.Equal(now) {
		t.Errorf("BandwidthCheck = %v, want %v", node.BandwidthCheck, now)
	}
}
