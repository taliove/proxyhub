package detection

import (
	"testing"

	"github.com/taliove/proxyhub/internal/subscription"
)

// 检测锁定测试(ticket 5 / spec #58,issue #63):真实代理检测经由
// buildProxyAdapter -> generator.ClashProxy -> mihomo adapter.ParseProxy
// 消费节点配置。本测试锁定 reality 节点的完整配置(flow/reality-opts/
// servername/client-fingerprint)能一路走到 mihomo 并被其真正接受——
// ParseProxy 对残缺/畸形的 reality 配置会报错,握手之外最强的行为锁定。
// fixture 全合成。
func TestNewProxyAdapterVLessReality(t *testing.T) {
	node := &subscription.Node{
		Name: "Reality01", Type: "vless", Server: "iepl01.example.com", Port: 20014,
		UUID: "00000000-0000-0000-0000-000000000000", Network: "tcp", TLS: true,
		SNI: "img.example.com", Flow: "xtls-rprx-vision", ClientFingerprint: "chrome",
		RealityPublicKey:  "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8",
		RealityShortID:    "d28a3d8c",
	}

	p, err := NewProxyAdapter(node)
	if err != nil {
		t.Fatalf("NewProxyAdapter() error = %v(mihomo 未接受 reality 配置,检测链路断裂)", err)
	}
	if p == nil {
		t.Fatal("NewProxyAdapter() returned nil adapter")
	}

	// 零回归:非 reality vless 照常构造
	plain := &subscription.Node{
		Name: "Plain01", Type: "vless", Server: "plain.example.com", Port: 8443,
		UUID: "00000000-0000-0000-0000-000000000000", Network: "tcp",
	}
	if _, err := NewProxyAdapter(plain); err != nil {
		t.Fatalf("NewProxyAdapter(plain) error = %v", err)
	}
}
