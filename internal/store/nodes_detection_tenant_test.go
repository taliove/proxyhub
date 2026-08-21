package store

import (
	"testing"
	"time"

	"github.com/taliove/proxyhub/internal/subscription"
)

// detectionTenantNode 归属用户 2 的池内节点(fixture 纪律:example.com 合成值)。
// SaveNodePoolForUser 落库写的是节点自身 UserID 字段,故 fixture 必须显式带归属。
func detectionTenantNode() *subscription.Node {
	return &subscription.Node{
		Name: "香港A", Type: "ss", Server: "a.example.com", Port: 8388,
		Cipher: "aes-256-gcm", Password: "p", UserID: 2,
	}
}

// quickResult 一次 quick 检测写回(可用,延迟 123ms)。
func quickResult(n *subscription.Node) *subscription.Node {
	return &subscription.Node{
		Server: n.Server, Port: n.Port,
		Available: true, Latency: 123,
		DetectionLastCheck: time.Now(),
		DetectionKind:      subscription.DetectionKindHealth,
	}
}

// bandwidthResult 一次带宽检测写回(下行 123.4 / 上行 45.6)。
func bandwidthResult(n *subscription.Node) *subscription.Node {
	return &subscription.Node{
		Server: n.Server, Port: n.Port,
		BandwidthDownMbps: 123.4, BandwidthUpMbps: 45.6,
		BandwidthCheck: time.Now(),
	}
}

func loadTenantNode(t *testing.T, s *Store, userID int64, key string) *subscription.Node {
	t.Helper()
	nodes, err := s.LoadNodePoolByUser(userID)
	if err != nil {
		t.Fatalf("LoadNodePoolByUser(%d): %v", userID, err)
	}
	for _, n := range nodes {
		if n.NodeKey() == key {
			return n
		}
	}
	t.Fatalf("node %s not found in user %d pool", key, userID)
	return nil
}

// 跨租户静默跳过、本租户正常落库(issue #131):nodes.user_id 是
// last-writer-wins 归属列,行归其他租户时检测写回不得越租户落库。
func TestUpdateNodeDetectionResult_TenantScoped(t *testing.T) {
	s := newTestStore(t)
	node := detectionTenantNode()
	if err := s.SaveNodePoolForUser(2, []*subscription.Node{node}); err != nil {
		t.Fatalf("SaveNodePoolForUser: %v", err)
	}
	key := node.NodeKey()

	// quick/real 分支:用户 1 写用户 2 的行——不报错、不写
	if err := s.UpdateNodeDetectionResult(1, quickResult(node), "quick"); err != nil {
		t.Fatalf("cross-tenant quick write must not error: %v", err)
	}
	got := loadTenantNode(t, s, 2, key)
	if got.Available || got.Latency != 0 || got.DetectionKind != "" {
		t.Errorf("cross-tenant quick write leaked: available=%v latency=%d kind=%q, want untouched",
			got.Available, got.Latency, got.DetectionKind)
	}

	// 本租户写:正常落库
	if err := s.UpdateNodeDetectionResult(2, quickResult(node), "quick"); err != nil {
		t.Fatalf("own-tenant quick write: %v", err)
	}
	got = loadTenantNode(t, s, 2, key)
	if !got.Available || got.Latency != 123 || got.DetectionKind != subscription.DetectionKindHealth {
		t.Errorf("own-tenant quick write = available %v latency %d kind %q, want true/123/health",
			got.Available, got.Latency, got.DetectionKind)
	}

	// bandwidth 分支:跨租户跳过、本租户落库,口径与 quick/real 一致
	if err := s.UpdateNodeDetectionResult(1, bandwidthResult(node), "bandwidth"); err != nil {
		t.Fatalf("cross-tenant bandwidth write must not error: %v", err)
	}
	got = loadTenantNode(t, s, 2, key)
	if got.BandwidthDownMbps != 0 || got.BandwidthUpMbps != 0 {
		t.Errorf("cross-tenant bandwidth write leaked: down=%v up=%v, want untouched",
			got.BandwidthDownMbps, got.BandwidthUpMbps)
	}
	if err := s.UpdateNodeDetectionResult(2, bandwidthResult(node), "bandwidth"); err != nil {
		t.Fatalf("own-tenant bandwidth write: %v", err)
	}
	got = loadTenantNode(t, s, 2, key)
	if got.BandwidthDownMbps != 123.4 || got.BandwidthUpMbps != 45.6 {
		t.Errorf("own-tenant bandwidth write = down %v up %v, want 123.4/45.6",
			got.BandwidthDownMbps, got.BandwidthUpMbps)
	}
}

// userID=0 是内部跨分片回退路径(机场测试探测等),不带租户谓词——
// 钉死该回退是有意语义,防止后续误加谓词把内部路径写死。
func TestUpdateNodeDetectionResult_ZeroUserFallback(t *testing.T) {
	s := newTestStore(t)
	node := detectionTenantNode()
	if err := s.SaveNodePoolForUser(2, []*subscription.Node{node}); err != nil {
		t.Fatalf("SaveNodePoolForUser: %v", err)
	}

	if err := s.UpdateNodeDetectionResult(0, quickResult(node), "quick"); err != nil {
		t.Fatalf("zero-user fallback write: %v", err)
	}
	got := loadTenantNode(t, s, 2, node.NodeKey())
	if !got.Available || got.Latency != 123 {
		t.Errorf("zero-user fallback = available %v latency %d, want true/123 (legacy cross-shard path)",
			got.Available, got.Latency)
	}
}
