package server

import (
	"strings"
	"testing"
	"time"

	"github.com/taliove/proxyhub/internal/subscription"
)

func TestBuildStatusNode(t *testing.T) {
	up := &subscription.Node{Name: "节点A", Server: "a.example.com", Port: 443, Available: true,
		DetectionKind: subscription.DetectionKindHealth, DetectionLastCheck: time.Now()}
	unchecked := &subscription.Node{Name: "节点B", Server: "b.example.com", Port: 443}
	down := &subscription.Node{Name: "原名C", DisplayName: "🇭🇰 香港01", Server: "c.example.com", Port: 443,
		Available: false, DetectionKind: subscription.DetectionKindHealth}

	// 全在线(未检测按在线计)
	n := buildStatusNode([]*subscription.Node{up, unchecked})
	if !strings.HasPrefix(n.Name, "📡 节点状态: 2/2 在线") {
		t.Errorf("all-up name = %q", n.Name)
	}
	if n.Type != "ss" || n.Server != "127.0.0.1" {
		t.Errorf("dummy config = %+v, want ss/127.0.0.1", n)
	}

	// 有故障:列生效名(槽位/标准名优先,用 EffectiveName 口径)
	n = buildStatusNode([]*subscription.Node{up, unchecked, down})
	if !strings.Contains(n.Name, "故障 1/3") || !strings.Contains(n.Name, "🇭🇰 香港01") {
		t.Errorf("down name = %q, want 故障 1/3 with effective name", n.Name)
	}
}

func TestPrependStatusNode(t *testing.T) {
	srv, st := newTestServer(t, nil)
	node := &subscription.Node{Name: "A", Server: "a.example.com", Port: 443, Available: true}
	ep, err := st.CreateEndpointForUser(0, "s")
	if err != nil {
		t.Fatal(err)
	}

	// 默认关:原样返回(零回归)
	out := srv.prependStatusNode([]*subscription.Node{node}, ep)
	if len(out) != 1 || out[0] != node {
		t.Fatal("disabled should passthrough")
	}

	// 开:第一位是状态节点
	if err := st.SetEndpointStatusNodeForUser(0, ep.ID, true); err != nil {
		t.Fatal(err)
	}
	ep, _ = st.GetEndpointByID(ep.ID)
	out = srv.prependStatusNode([]*subscription.Node{node}, ep)
	if len(out) != 2 || !strings.Contains(out[0].Name, "节点状态") || out[1] != node {
		t.Fatalf("enabled output = %v", out)
	}
}

// TestSubscription_StatusNodeEndToEnd /sub 真实链路:开关开启时输出含状态节点,
// 空池仍 503(状态节点不伪装空订阅)
func TestSubscription_StatusNodeEndToEnd(t *testing.T) {
	up := &subscription.Node{Name: "好节点", Type: "ss", Server: "a.example.com", Port: 443,
		Cipher: "aes-128-gcm", Password: "test-pw", Available: true}
	srv, st := newTestServer(t, []*subscription.Node{up})
	h := srv.Handler()
	ep, err := st.CreateEndpointForUser(0, "e2e")
	if err != nil {
		t.Fatal(err)
	}

	// 关:输出无状态节点
	out := fetchSub(t, h, ep)
	if strings.Contains(out, "节点状态") {
		t.Fatal("disabled: status node must not appear")
	}

	// 开:输出含状态节点且在首位节点
	if err := st.SetEndpointStatusNodeForUser(0, ep.ID, true); err != nil {
		t.Fatal(err)
	}
	out = fetchSub(t, h, ep)
	// 注:emoji 在 YAML 输出中被转义(\U0001F4E1),断言匹配未转义的中文段
	if !strings.Contains(out, "节点状态: 1/1 在线") {
		t.Errorf("enabled: output should contain status node, got:\n%s", out)
	}

	// 空池 + 开关开:仍 503
	srv2, st2 := newTestServer(t, nil)
	h2 := srv2.Handler()
	ep2, _ := st2.CreateEndpointForUser(0, "empty")
	if err := st2.SetEndpointStatusNodeForUser(0, ep2.ID, true); err != nil {
		t.Fatal(err)
	}
	status, _ := fetchSubStatus(t, h2, ep2)
	if status != 503 {
		t.Errorf("empty pool with status node = %d, want 503", status)
	}
}
