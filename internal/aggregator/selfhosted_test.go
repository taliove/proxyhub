package aggregator

import (
	"context"
	"net"
	"testing"

	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

// closedPort 返回一个刚被释放、几乎可确定连接会被拒绝的本地端口。
func closedPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}

// 自建节点必须与机场节点一起走健康检查:不可达时 Available 应翻为 false,
// 而不是旧实现里恒为 true。这坐实了「自建节点也要检查」(需求①)。
func TestExecute_HealthChecksSelfHosted(t *testing.T) {
	agg, st := newTestAggregator(t)

	if err := st.CreateSelfHostedNode(&store.SelfHostedNode{
		Name:     "自建不可达",
		Protocol: "vmess",
		Server:   "127.0.0.1",
		Port:     closedPort(t),
		Enabled:  true,
	}); err != nil {
		t.Fatalf("CreateSelfHostedNode: %v", err)
	}

	if err := agg.RunOnce(context.Background(), store.RefreshTriggerManual); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	var self *subscription.Node
	for _, n := range agg.Nodes() {
		if n.Source == subscription.SourceSelfHosted {
			self = n
			break
		}
	}
	if self == nil {
		t.Fatal("self-hosted node missing from pool after run (should be injected)")
	}
	if self.Available {
		t.Error("unreachable self-hosted node should be marked unavailable by health check, not hardcoded available")
	}
	if self.LastCheck.IsZero() {
		t.Error("self-hosted node LastCheck should be set by health check")
	}
}

// 启用的自建节点即使检测不可用,也应留在节点池里(常驻安全网,支撑需求②)。
func TestExecute_RetainsSelfHostedInPool(t *testing.T) {
	agg, st := newTestAggregator(t)
	if err := st.CreateSelfHostedNode(&store.SelfHostedNode{
		Name: "自建常驻", Protocol: "vmess", Server: "127.0.0.1", Port: closedPort(t), Enabled: true,
	}); err != nil {
		t.Fatalf("CreateSelfHostedNode: %v", err)
	}

	if err := agg.RunOnce(context.Background(), store.RefreshTriggerManual); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	found := false
	for _, n := range agg.Nodes() {
		if n.Name == "自建常驻" {
			found = true
		}
	}
	if !found {
		t.Error("self-hosted node must remain in pool even when unavailable")
	}
}
