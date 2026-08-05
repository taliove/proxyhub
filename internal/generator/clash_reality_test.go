package generator

import (
	"testing"

	"github.com/taliove/proxyhub/internal/subscription"
)

// vless reality Clash 输出缝测试(ticket 2 / spec #58,issue #60):
// reality 节点(RealityPublicKey 非空)必须输出 mihomo/Clash Meta 直连所需的
// flow、servername、client-fingerprint(缺省 chrome)、reality-opts、udp、tls;
// 非 reality vless 逐字段零回归。fixture 全合成。
func TestClashProxyVLessReality(t *testing.T) {
	const uuid = "00000000-0000-0000-0000-000000000000"
	const pbk = "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8" // 合成公钥,非真实凭证

	t.Run("reality vision 全参数", func(t *testing.T) {
		node := &subscription.Node{
			Name: "Reality01", Type: "vless", Server: "iepl01.example.com", Port: 20014,
			UUID: uuid, Network: "tcp", TLS: true, SNI: "img.example.com",
			Flow: "xtls-rprx-vision", ClientFingerprint: "chrome",
			RealityPublicKey: pbk, RealityShortID: "d28a3d8c",
		}
		got, err := ClashProxy(node, "Reality01")
		if err != nil {
			t.Fatalf("ClashProxy() error = %v", err)
		}
		assertString(t, got, "type", "vless")
		assertString(t, got, "uuid", uuid)
		assertString(t, got, "flow", "xtls-rprx-vision")
		assertString(t, got, "servername", "img.example.com")
		assertString(t, got, "client-fingerprint", "chrome")
		assertBool(t, got, "udp", true)
		assertBool(t, got, "tls", true)
		opts, ok := got["reality-opts"].(map[string]any)
		if !ok {
			t.Fatalf("reality-opts missing or not a map: %v", got["reality-opts"])
		}
		assertString(t, opts, "public-key", pbk)
		assertString(t, opts, "short-id", "d28a3d8c")
	})

	t.Run("缺 fp 补 chrome", func(t *testing.T) {
		node := &subscription.Node{
			Name: "Reality02", Type: "vless", Server: "iepl02.example.com", Port: 20015,
			UUID: uuid, Network: "tcp", TLS: true, SNI: "cdn.example.com",
			Flow: "xtls-rprx-vision", RealityPublicKey: pbk, RealityShortID: "ab12",
		}
		got, err := ClashProxy(node, "Reality02")
		if err != nil {
			t.Fatalf("ClashProxy() error = %v", err)
		}
		assertString(t, got, "client-fingerprint", "chrome")
	})

	t.Run("缺 sid 省略 short-id 键", func(t *testing.T) {
		node := &subscription.Node{
			Name: "Reality03", Type: "vless", Server: "iepl03.example.com", Port: 20016,
			UUID: uuid, Network: "tcp", TLS: true, SNI: "a.example.com",
			RealityPublicKey: pbk,
		}
		got, err := ClashProxy(node, "Reality03")
		if err != nil {
			t.Fatalf("ClashProxy() error = %v", err)
		}
		opts, ok := got["reality-opts"].(map[string]any)
		if !ok {
			t.Fatalf("reality-opts missing or not a map: %v", got["reality-opts"])
		}
		assertString(t, opts, "public-key", pbk)
		if _, exists := opts["short-id"]; exists {
			t.Errorf("short-id should be omitted when empty, got %v", opts["short-id"])
		}
	})

	// 非 reality vless(plain tcp / ws / grpc)逐字段零回归:
	// 不得出现 flow/servername/client-fingerprint/reality-opts/udp。
	t.Run("pbk 非空但 TLS=false 仍强制 tls: true", func(t *testing.T) {
		// 畸形机场链接(security=none&pbk=xxx)产生的节点:clash 侧必须与
		// v2ray/xray 两处强制 security=reality 的口径一致,否则 reality-opts
		// 不生效、静默退化明文,检测链路会误杀该节点(check 评审 M1)。
		node := &subscription.Node{
			Name: "Reality04", Type: "vless", Server: "iepl04.example.com", Port: 20017,
			UUID: uuid, Network: "tcp", TLS: false, SNI: "img.example.com",
			Flow: "xtls-rprx-vision", RealityPublicKey: pbk,
		}
		got, err := ClashProxy(node, "Reality04")
		if err != nil {
			t.Fatalf("ClashProxy() error = %v", err)
		}
		assertBool(t, got, "tls", true)
		if _, ok := got["reality-opts"].(map[string]any); !ok {
			t.Fatalf("reality-opts missing: %v", got["reality-opts"])
		}
	})

	t.Run("非 reality vless 零回归", func(t *testing.T) {
		nodes := []*subscription.Node{
			{Name: "Plain01", Type: "vless", Server: "plain.example.com", Port: 80,
				UUID: uuid, Network: "tcp"},
			{Name: "WS01", Type: "vless", Server: "ws01.example.com", Port: 443,
				UUID: uuid, Network: "ws", TLS: true, SNI: "wss.example.com"},
			{Name: "GRPC01", Type: "vless", Server: "grpc01.example.com", Port: 443,
				UUID: uuid, Network: "grpc", TLS: true, GrpcServiceName: "svc"},
		}
		realityKeys := []string{"flow", "servername", "client-fingerprint", "reality-opts", "udp"}
		for _, node := range nodes {
			got, err := ClashProxy(node, node.Name)
			if err != nil {
				t.Fatalf("ClashProxy(%s) error = %v", node.Name, err)
			}
			for _, k := range realityKeys {
				if _, exists := got[k]; exists {
					t.Errorf("%s: non-reality vless must not emit %q, got %v", node.Name, k, got[k])
				}
			}
		}
	})
}

func assertString(t *testing.T, m map[string]any, key, want string) {
	t.Helper()
	got, ok := m[key].(string)
	if !ok {
		t.Errorf("%s missing or not a string: %v", key, m[key])
		return
	}
	if got != want {
		t.Errorf("%s = %q, want %q", key, got, want)
	}
}

func assertBool(t *testing.T, m map[string]any, key string, want bool) {
	t.Helper()
	got, ok := m[key].(bool)
	if !ok {
		t.Errorf("%s missing or not a bool: %v", key, m[key])
		return
	}
	if got != want {
		t.Errorf("%s = %v, want %v", key, got, want)
	}
}
