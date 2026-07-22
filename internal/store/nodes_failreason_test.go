package store

import (
	"testing"

	"github.com/taliove/proxyhub/internal/subscription"
)

// TestNodePool_FailReasonRoundtrip 失败原因随节点池落库并读回(ticket 0017):
// 分类与截断详情经 SaveNodePool/LoadNodePool 完整保留;无失败节点读出空串。
func TestNodePool_FailReasonRoundtrip(t *testing.T) {
	st := newTestStore(t)

	pool := []*subscription.Node{
		{
			Name: "香港01", Type: "ss", Server: "hk.example.com", Port: 8388,
			Cipher: "aes-256-gcm", Password: "p1", Region: "HK", Source: "机场A",
			Available:           false,
			DetectionFailReason: "timeout",
			DetectionFailDetail: "dial tcp hk.example.com:8388: i/o timeout",
		},
		{
			Name: "日本01", Type: "ss", Server: "jp.example.com", Port: 443,
			Cipher: "aes-256-gcm", Password: "p2", Region: "JP", Source: "机场A",
			Available: true, // 检测成功:原因为空
		},
	}
	if err := st.SaveNodePool(pool); err != nil {
		t.Fatalf("SaveNodePool() error = %v", err)
	}

	got, err := st.LoadNodePool()
	if err != nil {
		t.Fatalf("LoadNodePool() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}

	if got[0].DetectionFailReason != "timeout" {
		t.Errorf("DetectionFailReason = %q, want timeout", got[0].DetectionFailReason)
	}
	if got[0].DetectionFailDetail != "dial tcp hk.example.com:8388: i/o timeout" {
		t.Errorf("DetectionFailDetail = %q", got[0].DetectionFailDetail)
	}
	if got[1].DetectionFailReason != "" || got[1].DetectionFailDetail != "" {
		t.Errorf("成功节点失败原因应为空: reason=%q detail=%q", got[1].DetectionFailReason, got[1].DetectionFailDetail)
	}
}
