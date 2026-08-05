package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 手动粘贴 Clash YAML 验收(spec #64,issue #66):共享解析边界的最高缝——
// 粘贴 YAML 全文(含 #!MANAGED-CONFIG 头与 port/dns/rules 段)导入成功,
// vless reality 字段完整入池,未知协议跳过计失败,失败行号为原文真实行号。
// fixture 全合成。
func TestImportAirport_ClashYAML(t *testing.T) {
	s, _ := newTestServer(t, nil)
	id := createManualAirport(t, s, "YAML机场")

	content := `#!MANAGED-CONFIG https://sub.example.com/link/00000000?clash=1
port: 7890
dns:
  enable: true
rules:
  - MATCH,DIRECT
proxies:
  - name: 'IEPL-01'
    type: vless
    server: iepl01.example.com
    port: 20014
    uuid: 00000000-0000-0000-0000-000000000000
    udp: true
    tls: true
    servername: img.example.com
    flow: xtls-rprx-vision
    client-fingerprint: chrome
    reality-opts:
      public-key: AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8
      short-id: d28a3d8c
  - name: 'HY2-01'
    type: hysteria2
    server: hy2.example.com
    port: 443
    password: hy2-password
  - name: 'SS-01'
    type: ss
    server: ss01.example.com
    port: 8388
    cipher: aes-256-gcm
    password: ss-password
`
	payload, _ := json.Marshal(map[string]any{"content": content})
	req := httptest.NewRequest(http.MethodPost, "/airports/1/import", bytes.NewReader(payload))
	req.SetPathValue("id", formatID(id))
	w := httptest.NewRecorder()
	s.handleImportAirport(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Imported int `json:"imported"`
		Failures []struct {
			Line   int    `json:"line"`
			Reason string `json:"reason"`
		} `json:"failures"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Imported != 2 {
		t.Errorf("imported = %d, want 2(vless + ss;hysteria2 跳过)", resp.Imported)
	}
	// hysteria2 项起始于原文第 21 行:失败行号必须是真实源行号而非 proxies 下标
	if len(resp.Failures) != 1 || resp.Failures[0].Line != 21 ||
		!strings.Contains(resp.Failures[0].Reason, "hysteria2") {
		t.Errorf("failures = %+v, want [{line:21, reason 含 hysteria2}]", resp.Failures)
	}

	// 入池:vless reality 节点字段完整(与分享链接路径待遇一致),ss 节点在
	fakes := s.nodes.(*fakeNodes)
	var reality, ssFound bool
	for _, n := range fakes.nodes {
		if n.Source != "YAML机场" {
			continue
		}
		switch n.Type {
		case "vless":
			reality = true
			if n.SNI != "img.example.com" || n.Flow != "xtls-rprx-vision" ||
				n.RealityPublicKey != "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8" ||
				n.RealityShortID != "d28a3d8c" || n.ClientFingerprint != "chrome" || !n.TLS {
				t.Errorf("reality 节点字段不完整: %+v", n)
			}
			if n.RawLink != "" {
				t.Errorf("YAML 来源节点 RawLink 应为空, got %q", n.RawLink)
			}
		case "ss":
			ssFound = true
		}
	}
	if !reality || !ssFound {
		t.Errorf("pool 缺节点: reality=%v ss=%v", reality, ssFound)
	}
}
