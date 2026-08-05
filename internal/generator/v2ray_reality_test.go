package generator

import (
	"strings"
	"testing"

	"github.com/taliove/proxyhub/internal/subscription"
)

// vless reality 输出缝测试(ticket 3 / spec #58):
// reality 节点(RealityPublicKey 非空)按结构化字段重造完整分享链接,
// type/security=reality/encryption=none/flow/sni/pbk/sid/fp 全带,缺 fp 兜底 chrome;
// 非 reality vless 输出与旧行为逐字节一致(零回归)。fixture 全合成。
func TestVlessLink_Reality(t *testing.T) {
	const uuid = "00000000-0000-0000-0000-000000000000"
	const pbk = "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8" // 合成公钥,非真实凭证

	tests := []struct {
		name string
		node *subscription.Node
		want string
	}{
		{
			name: "reality 全参数重造完整链接",
			node: &subscription.Node{
				Name: "Reality 01", Type: "vless", Server: "us1.example.com", Port: 443,
				UUID: uuid, TLS: true, SNI: "img.example.com",
				Flow: "xtls-rprx-vision", ClientFingerprint: "chrome",
				RealityPublicKey: pbk, RealityShortID: "d28a3d8c",
			},
			want: "vless://" + uuid + "@us1.example.com:443" +
				"?encryption=none&flow=xtls-rprx-vision&fp=chrome&pbk=" + pbk +
				"&security=reality&sid=d28a3d8c&sni=img.example.com&type=tcp#Reality%2001",
		},
		{
			name: "缺 fp 兜底 chrome,缺 flow/sid/sni 省略",
			node: &subscription.Node{
				Name: "Reality02", Type: "vless", Server: "hk1.example.com", Port: 8443,
				UUID: uuid, TLS: true, RealityPublicKey: pbk,
			},
			want: "vless://" + uuid + "@hk1.example.com:8443" +
				"?encryption=none&fp=chrome&pbk=" + pbk +
				"&security=reality&type=tcp#Reality02",
		},
		{
			name: "fragment 用标准化展示名(DisplayName 优先,名称标准化链路不动)",
			node: &subscription.Node{
				Name: "机场原名", DisplayName: "🇭🇰 香港 JS-01",
				Type: "vless", Server: "sg1.example.com", Port: 443,
				UUID: uuid, TLS: true, RealityPublicKey: pbk,
			},
			want: "vless://" + uuid + "@sg1.example.com:443" +
				"?encryption=none&fp=chrome&pbk=" + pbk +
				"&security=reality&type=tcp" +
				"#%F0%9F%87%AD%F0%9F%87%B0%20%E9%A6%99%E6%B8%AF%20JS-01",
		},
		{
			// 零回归:非 reality 的 tls vless 与旧输出逐字节一致
			name: "非 reality tls vless 零回归",
			node: &subscription.Node{
				Name: "WS01", Type: "vless", Server: "ws1.example.com", Port: 443,
				UUID: uuid, Network: "ws", TLS: true,
			},
			want: "vless://" + uuid + "@ws1.example.com:443?security=tls&type=ws#WS01",
		},
		{
			// 零回归:明文 vless 与旧输出逐字节一致
			name: "非 reality 明文 vless 零回归",
			node: &subscription.Node{
				Name: "Plain01", Type: "vless", Server: "plain.example.com", Port: 80,
				UUID: uuid,
			},
			want: "vless://" + uuid + "@plain.example.com:80?type=tcp#Plain01",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := vlessLink(tt.node); got != tt.want {
				t.Errorf("vlessLink() =\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}

// 重造出的 reality 链接必须能被自家解析器完整读回(客户端可导入性的代理验证)。
func TestVlessLink_RealityRoundTrip(t *testing.T) {
	const uuid = "00000000-0000-0000-0000-000000000000"
	const pbk = "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8"

	node := &subscription.Node{
		Name: "Reality01", Type: "vless", Server: "us1.example.com", Port: 443,
		UUID: uuid, TLS: true, SNI: "img.example.com",
		Flow: "xtls-rprx-vision", ClientFingerprint: "ios",
		RealityPublicKey: pbk, RealityShortID: "d28a3d8c",
	}

	link := vlessLink(node)
	res := subscription.ParseWithStats(link, "test")
	if res.ParseFailures != 0 {
		t.Fatalf("rebuilt link failed to parse: %+v", res.Failures)
	}
	if len(res.Nodes) != 1 {
		t.Fatalf("len(Nodes) = %d, want 1", len(res.Nodes))
	}
	got := res.Nodes[0]
	for field, want := range map[string]string{
		"Flow":              node.Flow,
		"ClientFingerprint": node.ClientFingerprint,
		"RealityPublicKey":  node.RealityPublicKey,
		"RealityShortID":    node.RealityShortID,
		"SNI":               node.SNI,
	} {
		var actual string
		switch field {
		case "Flow":
			actual = got.Flow
		case "ClientFingerprint":
			actual = got.ClientFingerprint
		case "RealityPublicKey":
			actual = got.RealityPublicKey
		case "RealityShortID":
			actual = got.RealityShortID
		case "SNI":
			actual = got.SNI
		}
		if actual != want {
			t.Errorf("round-trip %s = %q, want %q (link: %s)", field, actual, want, link)
		}
	}
	if !strings.HasSuffix(link, "#Reality01") {
		t.Errorf("fragment = %q, want #Reality01", link[strings.LastIndex(link, "#"):])
	}
}
