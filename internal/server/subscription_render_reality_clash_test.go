package server

import (
	"testing"

	"github.com/taliove/proxyhub/internal/subscription"
	"gopkg.in/yaml.v3"
)

// realityClashTestPool 订阅渲染缝 fixture(ticket 2 / spec #58,issue #60):
// 一个全参数 reality 节点 + 一个缺 fp/sid 的 reality 节点 + 一个非 reality vless
// + 一个 vmess(其他协议对照)。fixture 纪律:example.com + 全零 UUID + 合成 pbk。
func realityClashTestPool() []*subscription.Node {
	const uuid = "00000000-0000-0000-0000-000000000000"
	const pbk = "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8" // 合成公钥,非真实凭证
	return []*subscription.Node{
		{Name: "RealityFull", Type: "vless", Server: "iepl01.example.com", Port: 20014,
			UUID: uuid, Network: "tcp", TLS: true, SNI: "img.example.com",
			Flow: "xtls-rprx-vision", ClientFingerprint: "chrome",
			RealityPublicKey: pbk, RealityShortID: "d28a3d8c",
			Available: true, Latency: 100, Region: "HK", Source: "机场甲"},
		{Name: "RealityMinimal", Type: "vless", Server: "iepl02.example.com", Port: 20015,
			UUID: uuid, Network: "tcp", TLS: true, SNI: "cdn.example.com",
			Flow: "xtls-rprx-vision", RealityPublicKey: pbk,
			Available: true, Latency: 110, Region: "HK", Source: "机场甲"},
		{Name: "PlainVless", Type: "vless", Server: "plain.example.com", Port: 80,
			UUID: uuid, Network: "tcp",
			Available: true, Latency: 120, Region: "JP", Source: "机场甲"},
		{Name: "VMess01", Type: "vmess", Server: "vm.example.com", Port: 443,
			UUID: uuid, AlterID: 0, Network: "ws", TLS: true,
			Available: true, Latency: 130, Region: "US", Source: "机场甲"},
	}
}

// TestRenderSubscriptionRealityClash 订阅渲染缝(server 级):
// /sub 与后台预览共用的 renderSubscriptionForEndpoint(ADR 0005 同一转换函数)
// 输出的 Clash YAML 中,reality 节点必须携带完整 reality 配置;
// 非 reality vless 与其他协议逐字段不变。
func TestRenderSubscriptionRealityClash(t *testing.T) {
	srv, st := newEndpointTestServer(t, realityClashTestPool())
	userID := int64(601)

	ep, err := st.CreateEndpointForUser(userID, "reality-ep")
	if err != nil {
		t.Fatalf("create endpoint: %v", err)
	}

	data, contentType, err := srv.renderSubscriptionForEndpoint(realityClashTestPool(), "clash", ep)
	if err != nil {
		t.Fatalf("render clash: %v", err)
	}
	if contentType != "text/yaml; charset=utf-8" {
		t.Errorf("content-type = %s, want text/yaml", contentType)
	}

	var cfg map[string]any
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("rendered output is not valid yaml: %v\n%s", err, string(data))
	}
	proxiesRaw, ok := cfg["proxies"].([]any)
	if !ok {
		t.Fatalf("proxies missing or not a list:\n%s", string(data))
	}
	byName := map[string]map[string]any{}
	for _, raw := range proxiesRaw {
		p, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name, _ := p["name"].(string)
		byName[name] = p
	}

	const pbk = "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8"

	t.Run("reality 全参数节点", func(t *testing.T) {
		p := byName["RealityFull"]
		if p == nil {
			t.Fatalf("RealityFull not in rendered proxies:\n%s", string(data))
		}
		assertYAMLField(t, p, "type", "vless")
		assertYAMLField(t, p, "flow", "xtls-rprx-vision")
		assertYAMLField(t, p, "servername", "img.example.com")
		assertYAMLField(t, p, "client-fingerprint", "chrome")
		assertYAMLField(t, p, "tls", true)
		assertYAMLField(t, p, "udp", true)
		opts, ok := p["reality-opts"].(map[string]any)
		if !ok {
			t.Fatalf("reality-opts missing or not a map: %v", p["reality-opts"])
		}
		assertYAMLField(t, opts, "public-key", pbk)
		assertYAMLField(t, opts, "short-id", "d28a3d8c")
	})

	t.Run("reality 缺 fp 补 chrome、缺 sid 省略 short-id", func(t *testing.T) {
		p := byName["RealityMinimal"]
		if p == nil {
			t.Fatalf("RealityMinimal not in rendered proxies:\n%s", string(data))
		}
		assertYAMLField(t, p, "client-fingerprint", "chrome")
		assertYAMLField(t, p, "udp", true)
		opts, ok := p["reality-opts"].(map[string]any)
		if !ok {
			t.Fatalf("reality-opts missing or not a map: %v", p["reality-opts"])
		}
		assertYAMLField(t, opts, "public-key", pbk)
		if _, exists := opts["short-id"]; exists {
			t.Errorf("short-id should be omitted when empty, got %v", opts["short-id"])
		}
	})

	t.Run("非 reality vless 逐字段不变", func(t *testing.T) {
		p := byName["PlainVless"]
		if p == nil {
			t.Fatalf("PlainVless not in rendered proxies:\n%s", string(data))
		}
		assertYAMLField(t, p, "type", "vless")
		for _, k := range []string{"flow", "servername", "client-fingerprint", "reality-opts", "udp", "tls"} {
			if _, exists := p[k]; exists {
				t.Errorf("non-reality vless must not emit %q, got %v", k, p[k])
			}
		}
	})

	t.Run("其他协议(vmess)逐字段不变", func(t *testing.T) {
		p := byName["VMess01"]
		if p == nil {
			t.Fatalf("VMess01 not in rendered proxies:\n%s", string(data))
		}
		assertYAMLField(t, p, "type", "vmess")
		assertYAMLField(t, p, "network", "ws")
		assertYAMLField(t, p, "tls", true)
		for _, k := range []string{"flow", "servername", "client-fingerprint", "reality-opts", "udp"} {
			if _, exists := p[k]; exists {
				t.Errorf("vmess must not emit %q, got %v", k, p[k])
			}
		}
	})
}

// assertYAMLField 断言 yaml 解码后的 map 字段值(yaml.v3 解出的标量类型与 Go 原生对应)。
func assertYAMLField(t *testing.T, m map[string]any, key string, want any) {
	t.Helper()
	got, exists := m[key]
	if !exists {
		t.Errorf("%s missing in %v", key, m)
		return
	}
	if got != want {
		t.Errorf("%s = %v (%T), want %v (%T)", key, got, got, want, want)
	}
}
