package server

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/taliove/proxyhub/internal/subscription"
)

// v2ray 订阅渲染缝测试(ticket 3 / spec #58):订阅地址 v2ray 格式对 reality 节点
// 输出完整 vless 链接(security=reality/encryption=none/flow/sni/pbk/sid/fp),
// fragment 为标准化展示名;同批非 reality vless 节点输出保持旧格式(零回归)。
// fixture 全合成:example.com + 全零 UUID + 合成 pbk。
func TestRenderSubscriptionV2RayReality(t *testing.T) {
	srv, st := newEndpointTestServer(t, endpointTestPool())
	userID := int64(992)

	const uuid = "00000000-0000-0000-0000-000000000000"
	const pbk = "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8" // 合成公钥,非真实凭证

	nodes := []*subscription.Node{
		{
			Name: "机场原名", DisplayName: "🇭🇰 香港 JS-01",
			Type: "vless", Server: "us1.example.com", Port: 443,
			UUID: uuid, TLS: true, SNI: "img.example.com",
			Flow: "xtls-rprx-vision", ClientFingerprint: "chrome",
			RealityPublicKey: pbk, RealityShortID: "d28a3d8c",
			Available: true, Region: "HK", Source: "机场甲",
		},
		{
			// 缺 fp:渲染输出必须兜底 fp=chrome
			Name: "Reality02", Type: "vless", Server: "hk1.example.com", Port: 8443,
			UUID: uuid, TLS: true, RealityPublicKey: pbk,
			Available: true, Region: "HK", Source: "机场甲",
		},
		{
			// 非 reality vless:输出保持旧格式,不得混入 reality 参数
			Name: "Plain01", Type: "vless", Server: "plain.example.com", Port: 80,
			UUID: uuid, Available: true, Region: "US", Source: "机场甲",
		},
	}

	ep, err := st.CreateEndpointForUser(userID, "reality-ep")
	if err != nil {
		t.Fatalf("create endpoint: %v", err)
	}

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

	// 链接 1:reality 全参数
	full := links[0]
	for _, want := range []string{
		"vless://" + uuid + "@us1.example.com:443?",
		"type=tcp", "security=reality", "encryption=none",
		"flow=xtls-rprx-vision", "sni=img.example.com",
		"pbk=" + pbk, "sid=d28a3d8c", "fp=chrome",
	} {
		if !strings.Contains(full, want) {
			t.Errorf("reality link missing %q, got: %s", want, full)
		}
	}
	// fragment 为标准化展示名(DisplayName 优先于机场原名,空格 %20)
	if !strings.Contains(full, "#%F0%9F%87%AD%F0%9F%87%B0%20%E9%A6%99%E6%B8%AF%20JS-01") {
		t.Errorf("reality link fragment should be standardized display name, got: %s", full)
	}

	// 链接 2:缺 fp 兜底 chrome
	if !strings.Contains(links[1], "fp=chrome") {
		t.Errorf("link without fp should default to fp=chrome, got: %s", links[1])
	}
	if !strings.Contains(links[1], "security=reality") || !strings.Contains(links[1], "pbk="+pbk) {
		t.Errorf("link 2 should still be a full reality link, got: %s", links[1])
	}

	// 链接 3:非 reality 零回归(旧格式:type 唯一参数,无 security/encryption/pbk/fp)
	plain := links[2]
	if !strings.Contains(plain, "vless://"+uuid+"@plain.example.com:80?type=tcp#Plain01") {
		t.Errorf("non-reality vless output regressed, got: %s", plain)
	}
	for _, banned := range []string{"security=", "encryption=", "pbk=", "fp=", "sid="} {
		if strings.Contains(plain, banned) {
			t.Errorf("non-reality vless link must not contain %q, got: %s", banned, plain)
		}
	}
}
