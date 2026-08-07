package server

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/taliove/proxyhub/internal/subscription"
	"gopkg.in/yaml.v3"
)

// grpc 订阅渲染缝测试(ticket 2 / spec #72,issue #78):
// 订阅地址 v2ray 格式对 grpc 节点输出带 serviceName/authority 的链接,
// 且重造链接经 ParseWithStats 回读保真(round-trip);
// Clash 格式输出 grpc-opts.grpc-service-name;
// 同批非 grpc 节点输出保持旧格式(零回归)。
// fixture 全合成:example.com + 全零 UUID。
func TestRenderSubscriptionGrpc(t *testing.T) {
	srv, st := newEndpointTestServer(t, endpointTestPool())
	userID := int64(993)

	const uuid = "00000000-0000-0000-0000-000000000000"

	nodes := []*subscription.Node{
		{
			Name: "GrpcVless", DisplayName: "🇭🇰 香港 GRPC-01",
			Type: "vless", Server: "grpc1.example.com", Port: 443,
			UUID: uuid, Network: "grpc", TLS: true,
			GrpcServiceName: "grpcsvc", GrpcAuthority: "auth.example.com",
			Available: true, Region: "HK", Source: "机场甲",
		},
		{
			Name: "GrpcVmess", Type: "vmess", Server: "grpc2.example.com", Port: 443,
			UUID: uuid, Network: "grpc", TLS: true,
			GrpcServiceName: "vmsvc", GrpcAuthority: "vmauth.example.com",
			Available: true, Region: "JP", Source: "机场甲",
		},
		{
			// 非 grpc vless:输出保持旧格式,不得混入 grpc 参数
			Name: "Plain01", Type: "vless", Server: "plain.example.com", Port: 80,
			UUID: uuid, Available: true, Region: "US", Source: "机场甲",
		},
	}

	ep, err := st.CreateEndpointForUser(userID, "grpc-ep")
	if err != nil {
		t.Fatalf("create endpoint: %v", err)
	}

	t.Run("v2ray 格式", func(t *testing.T) {
		data, _, err := srv.renderSubscriptionForEndpoint(nodes, "v2ray", ep)
		if err != nil {
			t.Fatalf("render v2ray: %v", err)
		}

		decoded, err := base64.StdEncoding.DecodeString(string(data))
		if err != nil {
			t.Fatalf("output is not valid base64: %v", err)
		}
		links := strings.Split(strings.TrimSpace(string(decoded)), "\n")
		if len(links) != 3 {
			t.Fatalf("len(links) = %d, want 3, got:\n%s", len(links), string(decoded))
		}

		// 链接 1:grpc vless 带 serviceName/authority
		grpcLink := links[0]
		for _, want := range []string{
			"vless://" + uuid + "@grpc1.example.com:443?",
			"type=grpc", "serviceName=grpcsvc", "authority=auth.example.com",
		} {
			if !strings.Contains(grpcLink, want) {
				t.Errorf("grpc vless link missing %q, got: %s", want, grpcLink)
			}
		}

		// round-trip:渲染输出的链接经 ParseWithStats 回读保真
		res := subscription.ParseWithStats(grpcLink, "rendered")
		if res.ParseFailures != 0 || len(res.Nodes) != 1 {
			t.Fatalf("rendered grpc link round-trip failed: %+v", res.Failures)
		}
		got := res.Nodes[0]
		if got.GrpcServiceName != "grpcsvc" || got.GrpcAuthority != "auth.example.com" {
			t.Errorf("round-trip grpc params = %q/%q, want grpcsvc/auth.example.com",
				got.GrpcServiceName, got.GrpcAuthority)
		}

		// 链接 2:grpc vmess JSON 含 path/host(v2rayN 约定)
		vmessRaw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(links[1], "vmess://"))
		if err != nil {
			t.Fatalf("vmess link not valid base64: %v", err)
		}
		for _, want := range []string{`"net":"grpc"`, `"path":"vmsvc"`, `"host":"vmauth.example.com"`} {
			if !strings.Contains(string(vmessRaw), want) {
				t.Errorf("grpc vmess JSON missing %s, got: %s", want, string(vmessRaw))
			}
		}

		// 链接 3:非 grpc 零回归(旧格式:type 唯一参数)
		plain := links[2]
		if !strings.Contains(plain, "vless://"+uuid+"@plain.example.com:80?type=tcp#Plain01") {
			t.Errorf("non-grpc vless output regressed, got: %s", plain)
		}
		for _, banned := range []string{"serviceName=", "authority=", "type=grpc"} {
			if strings.Contains(plain, banned) {
				t.Errorf("non-grpc vless link must not contain %q, got: %s", banned, plain)
			}
		}
	})

	t.Run("clash 格式 grpc-opts", func(t *testing.T) {
		data, _, err := srv.renderSubscriptionForEndpoint(nodes, "clash", ep)
		if err != nil {
			t.Fatalf("render clash: %v", err)
		}
		var cfg map[string]any
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			t.Fatalf("rendered output is not valid yaml: %v\n%s", err, string(data))
		}
		proxiesRaw, ok := cfg["proxies"].([]any)
		if !ok {
			t.Fatalf("proxies missing or not a list:\n%s", string(data))
		}
		grpcSeen := 0
		for _, raw := range proxiesRaw {
			p, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			name, _ := p["name"].(string)
			switch name {
			case "🇭🇰 香港 GRPC-01", "GrpcVmess":
				opts, ok := p["grpc-opts"].(map[string]any)
				if !ok {
					t.Errorf("%s: grpc-opts missing: %v", name, p)
					continue
				}
				wantSvc := map[string]string{"🇭🇰 香港 GRPC-01": "grpcsvc", "GrpcVmess": "vmsvc"}[name]
				if opts["grpc-service-name"] != wantSvc {
					t.Errorf("%s: grpc-service-name = %v, want %s", name, opts["grpc-service-name"], wantSvc)
				}
				grpcSeen++
			case "Plain01":
				if _, exists := p["grpc-opts"]; exists {
					t.Errorf("non-grpc vless must not emit grpc-opts: %v", p)
				}
			}
		}
		if grpcSeen != 2 {
			t.Errorf("grpc proxies with grpc-opts = %d, want 2", grpcSeen)
		}
	})
}
