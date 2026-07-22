package store

import (
	"testing"

	"github.com/taliove/proxyhub/internal/subscription"
)

// TestNodePool_DetectionKindRoundtrip 验证 detection_kind 列随节点池快照持久化:
// 重启回填后可用性判定来源不失真(见 ticket 0016;口径推导依赖该字段)。
func TestNodePool_DetectionKindRoundtrip(t *testing.T) {
	st := newTestStore(t)

	pool := []*subscription.Node{
		{
			Name: "实测节点", Type: "ss", Server: "hk01.example.com", Port: 8388,
			Cipher: "aes-256-gcm", Password: "p1", Region: "HK", Source: "机场A",
			Available: true, DetectionKind: subscription.DetectionKindReal,
		},
		{
			Name: "快检节点", Type: "vmess", Server: "jp01.example.com", Port: 443,
			Region: "JP", Source: "机场A",
			Available: true, DetectionKind: subscription.DetectionKindHealth,
		},
		{
			Name: "未检节点", Type: "trojan", Server: "us01.example.com", Port: 443,
			Region: "US", Source: "机场A",
		},
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

	kinds := map[string]string{}
	for _, n := range got {
		kinds[n.Server] = n.DetectionKind
	}
	if kinds["hk01.example.com"] != subscription.DetectionKindReal {
		t.Errorf("实测节点 DetectionKind = %q, want %q", kinds["hk01.example.com"], subscription.DetectionKindReal)
	}
	if kinds["jp01.example.com"] != subscription.DetectionKindHealth {
		t.Errorf("快检节点 DetectionKind = %q, want %q", kinds["jp01.example.com"], subscription.DetectionKindHealth)
	}
	if kinds["us01.example.com"] != "" {
		t.Errorf("未检节点 DetectionKind = %q, want 空(从未检测)", kinds["us01.example.com"])
	}
}
