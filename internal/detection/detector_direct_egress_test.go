package detection

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/taliove/proxyhub/internal/subscription"
)

// TestTCPQuickCheck_DirectEgressEnabled 开关开:快筛经直连拨号器,
// 节点域名由自带 DoH(mock)解析,不经过系统 resolver。
func TestTCPQuickCheck_DirectEgressEnabled(t *testing.T) {
	dohURL := dohFixture(t, nil)
	port := tcpEchoListener(t)

	d := NewDetector(1, 3*time.Second, 3*time.Second)
	d.SetDirectEgressConfigProvider(func() DirectEgressConfig {
		return DirectEgressConfig{Enabled: true, DoHURL: dohURL, Interface: loopbackIface()}
	})

	node := &subscription.Node{Name: "n", Server: "example.com", Port: port}
	if err := d.tcpQuickCheckErr(context.Background(), node); err != nil {
		t.Fatalf("tcpQuickCheckErr() error = %v", err)
	}
}

// TestTCPQuickCheck_DirectEgressDisabled 开关关:快筛恢复系统拨号(现状行为),
// IP 字面量节点照常连通,不构造直连拨号器。
func TestTCPQuickCheck_DirectEgressDisabled(t *testing.T) {
	port := tcpEchoListener(t)

	d := NewDetector(1, 3*time.Second, 3*time.Second)
	d.SetDirectEgressConfigProvider(func() DirectEgressConfig {
		return DirectEgressConfig{Enabled: false, DoHURL: "http://127.0.0.1:1/dns-query", Interface: "noexist0"}
	})

	node := &subscription.Node{Name: "n", Server: "127.0.0.1", Port: port}
	if err := d.tcpQuickCheckErr(context.Background(), node); err != nil {
		t.Fatalf("tcpQuickCheckErr() with disabled direct egress error = %v", err)
	}
}

// TestTCPQuickCheck_NoProvider 未注入 provider:保持系统拨号(测试/装配缺省路径)。
func TestTCPQuickCheck_NoProvider(t *testing.T) {
	port := tcpEchoListener(t)

	d := NewDetector(1, 3*time.Second, 3*time.Second)
	node := &subscription.Node{Name: "n", Server: "127.0.0.1", Port: port}
	if err := d.tcpQuickCheckErr(context.Background(), node); err != nil {
		t.Fatalf("tcpQuickCheckErr() without provider error = %v", err)
	}
}

// TestTCPQuickCheck_DirectEgressStrictError 拨号器装配失败(网卡不存在):
// 快筛报错且经失败原因通道可见,不静默退化为系统拨号。
func TestTCPQuickCheck_DirectEgressStrictError(t *testing.T) {
	dohURL := dohFixture(t, nil)
	port := tcpEchoListener(t)

	d := NewDetector(1, 3*time.Second, 3*time.Second)
	d.SetDirectEgressConfigProvider(func() DirectEgressConfig {
		return DirectEgressConfig{Enabled: true, DoHURL: dohURL, Interface: "noexist0"}
	})

	node := &subscription.Node{Name: "n", Server: "example.com", Port: port}
	err := d.tcpQuickCheckErr(context.Background(), node)
	if err == nil {
		t.Fatal("tcpQuickCheckErr() expected strict error, got nil")
	}
	if !strings.Contains(err.Error(), "direct egress") {
		t.Errorf("error = %v, want direct-egress wrapped", err)
	}
	if reason := ClassifyFailure(err); reason == "" {
		t.Error("ClassifyFailure() = empty, strict error must be classifiable (ticket 0017 channel)")
	}
}

// TestTestNode_RealModeDirectEgressStrict real 模式快筛段同样走直连拨号器:
// 装配失败时 testReal 报 TCP 错误而非悄悄用系统拨号成功。
func TestTestNode_RealModeDirectEgressStrict(t *testing.T) {
	dohURL := dohFixture(t, nil)
	port := tcpEchoListener(t)

	d := NewDetector(1, 3*time.Second, 3*time.Second)
	d.SetDirectEgressConfigProvider(func() DirectEgressConfig {
		return DirectEgressConfig{Enabled: true, DoHURL: dohURL, Interface: "noexist0"}
	})

	node := &subscription.Node{Name: "n", Server: "example.com", Port: port}
	res := d.TestNode(context.Background(), node, "real")
	if res.Available {
		t.Fatal("TestNode(real) Available = true, want false under strict egress failure")
	}
	if !strings.Contains(res.Error, "TCP connection failed") {
		t.Errorf("Error = %q, want TCP connection failed prefix", res.Error)
	}
}

// TestNewProxyAdapter_DirectEgressInjected mihomo 注入点:开关开且装配正常时,
// detectNode 能构出 adapter(注入本身不拨号,仅验证装配路径不报错)。
func TestNewProxyAdapter_DirectEgressInjected(t *testing.T) {
	dohURL := dohFixture(t, nil)

	d := NewDetector(1, 3*time.Second, 3*time.Second)
	d.SetDirectEgressConfigProvider(func() DirectEgressConfig {
		return DirectEgressConfig{Enabled: true, DoHURL: dohURL, Interface: loopbackIface()}
	})

	node := &subscription.Node{
		Name: "n", Type: "ss", Server: "example.com", Port: 8388,
		Cipher: "aes-128-gcm", Password: "00000000-0000-0000-0000-000000000000",
	}
	pa, err := d.newProxyAdapter(node)
	if err != nil {
		t.Fatalf("newProxyAdapter() error = %v", err)
	}
	if pa == nil {
		t.Fatal("newProxyAdapter() = nil")
	}
}

// TestNewProxyAdapter_DirectEgressError 装配失败经 adapter 构造路径透出错误。
func TestNewProxyAdapter_DirectEgressError(t *testing.T) {
	d := NewDetector(1, 3*time.Second, 3*time.Second)
	d.SetDirectEgressConfigProvider(func() DirectEgressConfig {
		return DirectEgressConfig{Enabled: true, DoHURL: "http://127.0.0.1:1", Interface: "noexist0"}
	})

	node := &subscription.Node{
		Name: "n", Type: "ss", Server: "example.com", Port: 8388,
		Cipher: "aes-128-gcm", Password: "00000000-0000-0000-0000-000000000000",
	}
	if _, err := d.newProxyAdapter(node); err == nil {
		t.Fatal("newProxyAdapter() expected strict error, got nil")
	} else if !strings.Contains(fmt.Sprint(err), "direct egress") {
		t.Errorf("error = %v, want direct-egress wrapped", err)
	}
}
