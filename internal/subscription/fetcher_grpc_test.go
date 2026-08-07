package subscription

import (
	"encoding/base64"
	"testing"
)

// grpc 传输参数解析缝测试(ticket 1 / spec #72):
// vless 分享链接的 serviceName/authority、vmess JSON 在 net=grpc 时的
// path/host(v2rayN 约定)必须完整落入节点模型;非 grpc 节点两字段为空
// (零回归)。fixture 全合成。
func TestParseVLessGrpcNode(t *testing.T) {
	const uuid = "00000000-0000-0000-0000-000000000000"

	tests := []struct {
		name string
		line string
		want Node
	}{
		{
			name: "grpc 全参数(serviceName + authority)",
			line: "vless://" + uuid + "@grpc01.example.com:443?type=grpc&security=tls&sni=sni.example.com&serviceName=grpcsvc01&authority=auth01.example.com#Grpc01",
			want: Node{
				Name: "Grpc01", Type: "vless", Server: "grpc01.example.com", Port: 443,
				UUID: uuid, Network: "grpc", TLS: true, SNI: "sni.example.com",
				GrpcServiceName: "grpcsvc01", GrpcAuthority: "auth01.example.com",
			},
		},
		{
			name: "grpc 无 authority 容错(仅 serviceName)",
			line: "vless://" + uuid + "@grpc02.example.com:443?type=grpc&security=tls&serviceName=grpcsvc02#Grpc02",
			want: Node{
				Name: "Grpc02", Type: "vless", Server: "grpc02.example.com", Port: 443,
				UUID: uuid, Network: "grpc", TLS: true,
				GrpcServiceName: "grpcsvc02",
			},
		},
		{
			name: "grpc + reality 叠加(spec #58 与 #72 并存,两类参数同时保真)",
			line: "vless://" + uuid + "@grpc03.example.com:443?type=grpc&security=reality&sni=img.example.com&pbk=AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8&sid=d28a3d8c&fp=chrome&flow=xtls-rprx-vision&serviceName=grpcsvc03&authority=auth03.example.com#GrpcReality03",
			want: Node{
				Name: "GrpcReality03", Type: "vless", Server: "grpc03.example.com", Port: 443,
				UUID: uuid, Network: "grpc", TLS: true, SNI: "img.example.com",
				GrpcServiceName: "grpcsvc03", GrpcAuthority: "auth03.example.com",
				Flow:              "xtls-rprx-vision",
				RealityPublicKey:  "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8",
				RealityShortID:    "d28a3d8c",
				ClientFingerprint: "chrome",
			},
		},
		{
			name: "非 grpc(ws)零回归:两字段为空",
			line: "vless://" + uuid + "@ws01.example.com:443?type=ws&security=tls&sni=wss.example.com&path=%2Fws&host=cdn.example.com#WS01",
			want: Node{
				Name: "WS01", Type: "vless", Server: "ws01.example.com", Port: 443,
				UUID: uuid, Network: "ws", TLS: true, SNI: "wss.example.com",
			},
		},
		{
			name: "明文 tcp 零回归",
			line: "vless://" + uuid + "@plain.example.com:80?type=tcp#Plain01",
			want: Node{
				Name: "Plain01", Type: "vless", Server: "plain.example.com", Port: 80,
				UUID: uuid, Network: "tcp", TLS: false,
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
			if n.Network != tt.want.Network {
				t.Errorf("Network = %q, want %q", n.Network, tt.want.Network)
			}
			if n.TLS != tt.want.TLS {
				t.Errorf("TLS = %v, want %v", n.TLS, tt.want.TLS)
			}
			if n.SNI != tt.want.SNI {
				t.Errorf("SNI = %q, want %q", n.SNI, tt.want.SNI)
			}
			if n.GrpcServiceName != tt.want.GrpcServiceName {
				t.Errorf("GrpcServiceName = %q, want %q", n.GrpcServiceName, tt.want.GrpcServiceName)
			}
			if n.GrpcAuthority != tt.want.GrpcAuthority {
				t.Errorf("GrpcAuthority = %q, want %q", n.GrpcAuthority, tt.want.GrpcAuthority)
			}
			if n.RealityPublicKey != tt.want.RealityPublicKey {
				t.Errorf("RealityPublicKey = %q, want %q", n.RealityPublicKey, tt.want.RealityPublicKey)
			}
			if n.Flow != tt.want.Flow {
				t.Errorf("Flow = %q, want %q", n.Flow, tt.want.Flow)
			}
		})
	}
}

// vmess grpc JSON 解析缝(spec #72):net=grpc 时 path→GrpcServiceName、
// host→GrpcAuthority(v2rayN 约定);net!=grpc 时 path/host 不入模型
// (ws 的 path/host 是已知缺口,不在本票)。
func TestParseVMessGrpcNode(t *testing.T) {
	const uuid = "00000000-0000-0000-0000-000000000000"

	vmessLink := func(jsonBody string) string {
		return "vmess://" + base64.StdEncoding.EncodeToString([]byte(jsonBody))
	}

	tests := []struct {
		name string
		line string
		want Node
	}{
		{
			name: "net=grpc 全参数(path→serviceName, host→authority)",
			line: vmessLink(`{"add":"grpc01.example.com","port":443,"id":"` + uuid + `","aid":0,"net":"grpc","type":"none","host":"auth01.example.com","path":"grpcsvc01","tls":"tls","ps":"GrpcVmess01"}`),
			want: Node{
				Name: "GrpcVmess01", Type: "vmess", Server: "grpc01.example.com", Port: 443,
				UUID: uuid, Network: "grpc", TLS: true,
				GrpcServiceName: "grpcsvc01", GrpcAuthority: "auth01.example.com",
			},
		},
		{
			name: "net=grpc 无 host 容错(authority 为空)",
			line: vmessLink(`{"add":"grpc02.example.com","port":443,"id":"` + uuid + `","aid":0,"net":"grpc","type":"none","host":"","path":"grpcsvc02","tls":"","ps":"GrpcVmess02"}`),
			want: Node{
				Name: "GrpcVmess02", Type: "vmess", Server: "grpc02.example.com", Port: 443,
				UUID: uuid, Network: "grpc", TLS: false,
				GrpcServiceName: "grpcsvc02",
			},
		},
		{
			name: "net=ws 零回归:path/host 不落入 grpc 字段",
			line: vmessLink(`{"add":"ws01.example.com","port":443,"id":"` + uuid + `","aid":0,"net":"ws","type":"none","host":"cdn.example.com","path":"/ws","tls":"tls","ps":"WSVmess01"}`),
			want: Node{
				Name: "WSVmess01", Type: "vmess", Server: "ws01.example.com", Port: 443,
				UUID: uuid, Network: "ws", TLS: true,
			},
		},
		{
			name: "net=tcp 零回归",
			line: vmessLink(`{"add":"tcp01.example.com","port":80,"id":"` + uuid + `","aid":0,"net":"tcp","type":"none","host":"","path":"","tls":"","ps":"TCPVmess01"}`),
			want: Node{
				Name: "TCPVmess01", Type: "vmess", Server: "tcp01.example.com", Port: 80,
				UUID: uuid, Network: "tcp", TLS: false,
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
			if n.Network != tt.want.Network {
				t.Errorf("Network = %q, want %q", n.Network, tt.want.Network)
			}
			if n.TLS != tt.want.TLS {
				t.Errorf("TLS = %v, want %v", n.TLS, tt.want.TLS)
			}
			if n.GrpcServiceName != tt.want.GrpcServiceName {
				t.Errorf("GrpcServiceName = %q, want %q", n.GrpcServiceName, tt.want.GrpcServiceName)
			}
			if n.GrpcAuthority != tt.want.GrpcAuthority {
				t.Errorf("GrpcAuthority = %q, want %q", n.GrpcAuthority, tt.want.GrpcAuthority)
			}
		})
	}
}
