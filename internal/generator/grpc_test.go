package generator

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/taliove/proxyhub/internal/subscription"
)

// gRPC 参数三发射器输出缝测试(ticket 2 / spec #72,issue #78):
// vless 链接 network=grpc 时带 serviceName/authority(空省略);
// vmess JSON net=grpc 时 path=GrpcServiceName、host=GrpcAuthority(v2rayN 约定
// 反向映射);Clash grpc-opts 仅 grpc-service-name(mihomo 无 authority)。
// 非 grpc 节点三发射器输出逐字段零回归。fixture 全合成:example.com + 全零 UUID。

const grpcTestUUID = "00000000-0000-0000-0000-000000000000"

// TestVlessLink_Grpc vless 输出缝:grpc 参数的发射与省略规则。
func TestVlessLink_Grpc(t *testing.T) {
	tests := []struct {
		name string
		node *subscription.Node
		want string
	}{
		{
			name: "grpc 全参数(serviceName+authority)",
			node: &subscription.Node{
				Name: "Grpc01", Type: "vless", Server: "grpc1.example.com", Port: 443,
				UUID: grpcTestUUID, Network: "grpc", TLS: true,
				GrpcServiceName: "grpcsvc", GrpcAuthority: "auth.example.com",
			},
			want: "vless://" + grpcTestUUID + "@grpc1.example.com:443" +
				"?authority=auth.example.com&security=tls&serviceName=grpcsvc&type=grpc#Grpc01",
		},
		{
			name: "仅 serviceName,authority 省略",
			node: &subscription.Node{
				Name: "Grpc02", Type: "vless", Server: "grpc2.example.com", Port: 443,
				UUID: grpcTestUUID, Network: "grpc",
				GrpcServiceName: "grpcsvc",
			},
			want: "vless://" + grpcTestUUID + "@grpc2.example.com:443" +
				"?serviceName=grpcsvc&type=grpc#Grpc02",
		},
		{
			name: "grpc 但两字段皆空:只带 type=grpc",
			node: &subscription.Node{
				Name: "Grpc03", Type: "vless", Server: "grpc3.example.com", Port: 443,
				UUID: grpcTestUUID, Network: "grpc",
			},
			want: "vless://" + grpcTestUUID + "@grpc3.example.com:443?type=grpc#Grpc03",
		},
		{
			// 零回归:非 grpc 节点即使模型残留 grpc 字段也不得发射
			name: "非 grpc(ws)带残留 grpc 字段不发射",
			node: &subscription.Node{
				Name: "WS01", Type: "vless", Server: "ws1.example.com", Port: 443,
				UUID: grpcTestUUID, Network: "ws", TLS: true,
				GrpcServiceName: "grpcsvc", GrpcAuthority: "auth.example.com",
			},
			want: "vless://" + grpcTestUUID + "@ws1.example.com:443?security=tls&type=ws#WS01",
		},
		{
			// grpc+reality 组合(spec #72 与 spec #58 交集):
			// reality 参数与 grpc 参数并存于同一链接
			name: "grpc+reality 组合两类参数并存",
			node: &subscription.Node{
				Name: "GrpcReality", Type: "vless", Server: "gr.example.com", Port: 443,
				UUID: grpcTestUUID, Network: "grpc",
				GrpcServiceName: "grpcsvc", GrpcAuthority: "auth.example.com",
				RealityPublicKey: "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8", // 合成公钥
				ClientFingerprint: "chrome",
			},
			want: "vless://" + grpcTestUUID + "@gr.example.com:443" +
				"?authority=auth.example.com&encryption=none&fp=chrome" +
				"&pbk=AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8" +
				"&security=reality&serviceName=grpcsvc&type=grpc#GrpcReality",
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

// TestVlessLink_GrpcRoundTrip 重造出的 grpc 链接必须能被自家解析器
// 完整读回(验收标准:round-trip 保真)。
func TestVlessLink_GrpcRoundTrip(t *testing.T) {
	node := &subscription.Node{
		Name: "GrpcRT", Type: "vless", Server: "grpc1.example.com", Port: 443,
		UUID: grpcTestUUID, Network: "grpc", TLS: true,
		GrpcServiceName: "grpcsvc", GrpcAuthority: "auth.example.com",
	}

	link := vlessLink(node)
	res := subscription.ParseWithStats(link, "test")
	if res.ParseFailures != 0 {
		t.Fatalf("rebuilt grpc link failed to parse: %+v", res.Failures)
	}
	if len(res.Nodes) != 1 {
		t.Fatalf("len(Nodes) = %d, want 1", len(res.Nodes))
	}
	got := res.Nodes[0]
	if got.Network != "grpc" {
		t.Errorf("round-trip Network = %q, want grpc", got.Network)
	}
	if got.GrpcServiceName != node.GrpcServiceName {
		t.Errorf("round-trip GrpcServiceName = %q, want %q (link: %s)",
			got.GrpcServiceName, node.GrpcServiceName, link)
	}
	if got.GrpcAuthority != node.GrpcAuthority {
		t.Errorf("round-trip GrpcAuthority = %q, want %q (link: %s)",
			got.GrpcAuthority, node.GrpcAuthority, link)
	}
}

// decodeVmessLink 解开 vmess:// 链接的 JSON 载荷。
func decodeVmessLink(t *testing.T, link string) map[string]any {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(link, "vmess://"))
	if err != nil {
		t.Fatalf("vmess link not valid base64: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("vmess payload not valid json: %v", err)
	}
	return cfg
}

// TestVmessLink_Grpc vmess 输出缝:net=grpc 时按 v2rayN 约定反向映射
// path=GrpcServiceName、host=GrpcAuthority;非 grpc 时 host/path 维持空串。
func TestVmessLink_Grpc(t *testing.T) {
	t.Run("net=grpc 反向映射 path/host", func(t *testing.T) {
		node := &subscription.Node{
			Name: "VmessGrpc", Type: "vmess", Server: "vm.example.com", Port: 443,
			UUID: grpcTestUUID, Network: "grpc", TLS: true,
			GrpcServiceName: "grpcsvc", GrpcAuthority: "auth.example.com",
		}
		link, err := vmessLink(node)
		if err != nil {
			t.Fatalf("vmessLink: %v", err)
		}
		cfg := decodeVmessLink(t, link)
		if cfg["net"] != "grpc" {
			t.Errorf("net = %v, want grpc", cfg["net"])
		}
		if cfg["path"] != "grpcsvc" {
			t.Errorf("path = %v, want grpcsvc (GrpcServiceName)", cfg["path"])
		}
		if cfg["host"] != "auth.example.com" {
			t.Errorf("host = %v, want auth.example.com (GrpcAuthority)", cfg["host"])
		}
	})

	t.Run("非 grpc 节点 host/path 维持空串零回归", func(t *testing.T) {
		for _, network := range []string{"", "tcp", "ws"} {
			node := &subscription.Node{
				Name: "VmessPlain", Type: "vmess", Server: "vm.example.com", Port: 443,
				UUID: grpcTestUUID, Network: network,
				// 模型残留 grpc 字段也不得泄漏到非 grpc 输出
				GrpcServiceName: "grpcsvc", GrpcAuthority: "auth.example.com",
			}
			link, err := vmessLink(node)
			if err != nil {
				t.Fatalf("vmessLink(net=%q): %v", network, err)
			}
			cfg := decodeVmessLink(t, link)
			if cfg["host"] != "" || cfg["path"] != "" {
				t.Errorf("net=%q: host/path = %v/%v, want both empty", network, cfg["host"], cfg["path"])
			}
		}
	})
}

// TestClashProxy_Grpc Clash 输出缝:grpc-opts 仅 grpc-service-name
// (mihomo 无 authority 概念);vless/vmess 两协议一致;空 serviceName 省略。
func TestClashProxy_Grpc(t *testing.T) {
	for _, typ := range []string{"vless", "vmess"} {
		t.Run(typ+" grpc 带 grpc-opts", func(t *testing.T) {
			node := &subscription.Node{
				Name: "Grpc", Type: typ, Server: "grpc1.example.com", Port: 443,
				UUID: grpcTestUUID, Network: "grpc",
				GrpcServiceName: "grpcsvc", GrpcAuthority: "auth.example.com",
			}
			p, err := ClashProxy(node, "Grpc")
			if err != nil {
				t.Fatalf("ClashProxy: %v", err)
			}
			if p["network"] != "grpc" {
				t.Errorf("network = %v, want grpc", p["network"])
			}
			opts, ok := p["grpc-opts"].(map[string]any)
			if !ok {
				t.Fatalf("grpc-opts missing or not a map: %v", p)
			}
			if opts["grpc-service-name"] != "grpcsvc" {
				t.Errorf("grpc-service-name = %v, want grpcsvc", opts["grpc-service-name"])
			}
			// mihomo 无 authority:不得泄漏
			if _, exists := opts["authority"]; exists {
				t.Errorf("grpc-opts must not carry authority: %v", opts)
			}
		})

		t.Run(typ+" 非 grpc 零回归", func(t *testing.T) {
			node := &subscription.Node{
				Name: "Plain", Type: typ, Server: "plain.example.com", Port: 443,
				UUID: grpcTestUUID, Network: "ws",
				GrpcServiceName: "grpcsvc",
			}
			p, err := ClashProxy(node, "Plain")
			if err != nil {
				t.Fatalf("ClashProxy: %v", err)
			}
			if _, exists := p["grpc-opts"]; exists {
				t.Errorf("non-grpc %s must not emit grpc-opts: %v", typ, p)
			}
		})
	}
}
