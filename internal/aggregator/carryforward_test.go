package aggregator

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/taliove/proxyhub/internal/subscription"
)

// TestAggregator_CarryForwardDetectionState 端到端验证检测状态 carry-forward（修复刷新抹掉真实检测的 bug）
func TestAggregator_CarryForwardDetectionState(t *testing.T) {
	a, st := newTestAggregator(t)

	// 创建测试机场服务器，返回固定节点
	node1 := "trojan://pw@1.1.1.1:8388#HK01"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(base64.StdEncoding.EncodeToString([]byte(node1))))
	}))
	t.Cleanup(srv.Close)

	if _, err := st.CreateAirport("机场A", srv.URL); err != nil {
		t.Fatalf("CreateAirport() error = %v", err)
	}

	detectionTime := time.Now().Add(-1 * time.Hour)

	// 第一轮刷新
	if err := a.RunOnce(context.Background(), "test"); err != nil {
		t.Fatalf("RunOnce() first error = %v", err)
	}

	// 模拟真实检测：直接修改内存池的检测状态
	if len(a.NodesForUser(0)) != 1 {
		t.Fatalf("first round: nodes len = %d, want 1", len(a.NodesForUser(0)))
	}
	a.NodesForUser(0)[0].Available = false
	a.NodesForUser(0)[0].Latency = 999
	a.NodesForUser(0)[0].DetectionLastCheck = detectionTime
	// 持久化修改后的状态（模拟检测完成后的写回）
	if err := st.SaveNodePool(a.NodesForUser(0)); err != nil {
		t.Fatalf("save detection state error = %v", err)
	}

	// 第二轮刷新：同一节点（NodeKey 相同）
	if err := a.RunOnce(context.Background(), "test"); err != nil {
		t.Fatalf("RunOnce() second error = %v", err)
	}

	// 验证：检测状态应保留（Available=false, Latency=999, DetectionLastCheck 非零）
	nodes := a.NodesForUser(0)

	if len(nodes) != 1 {
		t.Fatalf("second round: nodes len = %d, want 1", len(nodes))
	}

	node := nodes[0]
	if node.Available != false || node.Latency != 999 {
		t.Errorf("检测状态未保留: Available=%v Latency=%d, want false/999", node.Available, node.Latency)
	}
	if !node.DetectionLastCheck.Equal(detectionTime) {
		t.Errorf("DetectionLastCheck 未保留: got %v, want %v", node.DetectionLastCheck, detectionTime)
	}
}

// TestAggregator_StaleMissingNodes 验证消失的机场节点标记为 stale
func TestAggregator_StaleMissingNodes(t *testing.T) {
	a, st := newTestAggregator(t)

	// 第一轮：返回两个节点
	nodes2 := "trojan://pw1@1.1.1.1:8388#HK01\ntrojan://pw2@2.2.2.2:443#JP01"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(base64.StdEncoding.EncodeToString([]byte(nodes2))))
	}))
	t.Cleanup(srv.Close)

	if _, err := st.CreateAirport("机场A", srv.URL); err != nil {
		t.Fatalf("CreateAirport() error = %v", err)
	}

	if err := a.RunOnce(context.Background(), "test"); err != nil {
		t.Fatalf("RunOnce() first error = %v", err)
	}

	// 第二轮：修改服务器只返回一个节点（HK01）
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		node1 := "trojan://pw1@1.1.1.1:8388#HK01"
		w.Write([]byte(base64.StdEncoding.EncodeToString([]byte(node1))))
	})

	if err := a.RunOnce(context.Background(), "test"); err != nil {
		t.Fatalf("RunOnce() second error = %v", err)
	}

	// 验证：应有2个节点（香港 active + 日本 stale）
	nodes := a.NodesForUser(0)

	if len(nodes) != 2 {
		t.Fatalf("nodes len = %d, want 2 (1 active + 1 stale)", len(nodes))
	}

	var hk, jp *subscription.Node
	for _, n := range nodes {
		if n.Server == "1.1.1.1" {
			hk = n
		} else if n.Server == "2.2.2.2" {
			jp = n
		}
	}

	if hk == nil || jp == nil {
		t.Fatalf("missing nodes: hk=%v jp=%v", hk, jp)
	}

	// 香港：active
	if hk.Stale {
		t.Errorf("香港节点 Stale = true, want false")
	}
	// 日本：stale
	if !jp.Stale {
		t.Errorf("日本节点 Stale = false, want true")
	}
	if jp.LastSeen.IsZero() {
		t.Errorf("日本节点 LastSeen 为零值，应非零")
	}
}
