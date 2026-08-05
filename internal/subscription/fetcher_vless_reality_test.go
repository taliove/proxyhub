package subscription

import "testing"

// vless reality 解析缝测试(ticket 1 / spec #58):
// 分享链接里的 flow/pbk/sid/sni(servername 别名)/fp 必须完整落入节点模型,
// security=tls 老链接与未知参数容错零回归。fixture 全合成。
func TestParseVLessRealityNode(t *testing.T) {
	const uuid = "00000000-0000-0000-0000-000000000000"
	const pbk = "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8" // 合成公钥,非真实凭证

	tests := []struct {
		name string
		line string
		want Node
	}{
		{
			name: "reality vision 全参数",
			line: "vless://" + uuid + "@iepl01.example.com:20014?type=tcp&security=reality&encryption=none&flow=xtls-rprx-vision&sni=img.example.com&fp=chrome&pbk=" + pbk + "&sid=d28a3d8c#Reality01",
			want: Node{
				Name: "Reality01", Type: "vless", Server: "iepl01.example.com", Port: 20014,
				UUID: uuid, Network: "tcp", TLS: true, SNI: "img.example.com",
				Flow: "xtls-rprx-vision", ClientFingerprint: "chrome",
				RealityPublicKey: pbk, RealityShortID: "d28a3d8c",
			},
		},
		{
			name: "servername 参数名兼容(机场两种写法并存)",
			line: "vless://" + uuid + "@iepl02.example.com:20015?security=reality&servername=cdn.example.com&pbk=" + pbk + "&flow=xtls-rprx-vision#Reality02",
			want: Node{
				Name: "Reality02", Type: "vless", Server: "iepl02.example.com", Port: 20015,
				UUID: uuid, Network: "tcp", TLS: true, SNI: "cdn.example.com",
				Flow: "xtls-rprx-vision", RealityPublicKey: pbk,
			},
		},
		{
			name: "sni 优先于 servername",
			line: "vless://" + uuid + "@iepl03.example.com:20016?security=reality&sni=a.example.com&servername=b.example.com&pbk=" + pbk + "#Reality03",
			want: Node{
				Name: "Reality03", Type: "vless", Server: "iepl03.example.com", Port: 20016,
				UUID: uuid, Network: "tcp", TLS: true, SNI: "a.example.com",
				RealityPublicKey: pbk,
			},
		},
		{
			// spec #58 显式决策"vless 解析补齐 SNI":tls 老链接相对旧行为唯一差异即 SNI 落字段
			name: "security=tls 老链接除补齐 SNI 外零回归(未识别的 ws 参数静默忽略)",
			line: "vless://" + uuid + "@ws01.example.com:443?type=ws&security=tls&sni=wss.example.com&path=%2Fws&host=cdn.example.com#WS01",
			want: Node{
				Name: "WS01", Type: "vless", Server: "ws01.example.com", Port: 443,
				UUID: uuid, Network: "ws", TLS: true, SNI: "wss.example.com",
			},
		},
		{
			name: "明文 tcp 无 security 零回归",
			line: "vless://" + uuid + "@plain.example.com:80?type=tcp#Plain01",
			want: Node{
				Name: "Plain01", Type: "vless", Server: "plain.example.com", Port: 80,
				UUID: uuid, Network: "tcp", TLS: false,
			},
		},
		{
			name: "未知参数静默忽略不整行失败",
			line: "vless://" + uuid + "@x01.example.com:1234?security=reality&pbk=" + pbk + "&foo=bar&alpn=h2#X01",
			want: Node{
				Name: "X01", Type: "vless", Server: "x01.example.com", Port: 1234,
				UUID: uuid, Network: "tcp", TLS: true, RealityPublicKey: pbk,
			},
		},
		{
			name: "reality 缺 pbk 仍容错解析(下游按非 reality 对待)",
			line: "vless://" + uuid + "@deg01.example.com:8443?security=reality&flow=xtls-rprx-vision#Deg01",
			want: Node{
				Name: "Deg01", Type: "vless", Server: "deg01.example.com", Port: 8443,
				UUID: uuid, Network: "tcp", TLS: true, Flow: "xtls-rprx-vision",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := ParseWithStats(tt.line, "test")
			if res.ParseFailures != 0 {
				t.Fatalf("ParseFailures = %d, want 0 (%+v)", res.ParseFailures, res.Failures)
			}
			if len(res.Nodes) != 1 {
				t.Fatalf("len(Nodes) = %d, want 1", len(res.Nodes))
			}
			n := res.Nodes[0]
			if n.Name != tt.want.Name {
				t.Errorf("Name = %q, want %q", n.Name, tt.want.Name)
			}
			if n.Type != tt.want.Type {
				t.Errorf("Type = %q, want %q", n.Type, tt.want.Type)
			}
			if n.Server != tt.want.Server {
				t.Errorf("Server = %q, want %q", n.Server, tt.want.Server)
			}
			if n.Port != tt.want.Port {
				t.Errorf("Port = %d, want %d", n.Port, tt.want.Port)
			}
			if n.UUID != tt.want.UUID {
				t.Errorf("UUID = %q, want %q", n.UUID, tt.want.UUID)
			}
			if n.Network != tt.want.Network {
				t.Errorf("Network = %q, want %q", n.Network, tt.want.Network)
			}
			if n.TLS != tt.want.TLS {
				t.Errorf("TLS = %v, want %v", n.TLS, tt.want.TLS)
			}
			if n.SNI != tt.want.SNI {
				t.Errorf("SNI = %q, want %q", n.SNI, tt.want.SNI)
			}
			if n.Flow != tt.want.Flow {
				t.Errorf("Flow = %q, want %q", n.Flow, tt.want.Flow)
			}
			if n.ClientFingerprint != tt.want.ClientFingerprint {
				t.Errorf("ClientFingerprint = %q, want %q", n.ClientFingerprint, tt.want.ClientFingerprint)
			}
			if n.RealityPublicKey != tt.want.RealityPublicKey {
				t.Errorf("RealityPublicKey = %q, want %q", n.RealityPublicKey, tt.want.RealityPublicKey)
			}
			if n.RealityShortID != tt.want.RealityShortID {
				t.Errorf("RealityShortID = %q, want %q", n.RealityShortID, tt.want.RealityShortID)
			}
		})
	}
}
