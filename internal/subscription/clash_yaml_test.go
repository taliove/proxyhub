package subscription

import (
	"encoding/base64"
	"strings"
	"testing"
)

// 本文件覆盖 spec #64 / issue #65 的 Clash YAML 嗅探与解析缝。
// fixture 全合成:example.com + 全零 UUID + 合成 pbk,真实机场样本不进 testdata。

// clashYAMLFullConfig 合成一份真实形态的 Clash YAML 全文:#!MANAGED-CONFIG 注释头、
// port/dns/proxy-groups/rules 等非 proxies 段,proxies 覆盖 vmess/vless-reality/
// trojan/ss-obfs/anytls 五协议。
const clashYAMLFullConfig = `#!MANAGED-CONFIG https://sub.example.com/api/v1/client/subscribe?token=00000000 interval=86400 strict=false

port: 7890
socks-port: 7891
mixed-port: 7893
allow-lan: false
mode: rule
log-level: info
dns:
  enable: true
  nameserver:
    - 223.5.5.5
    - 119.29.29.29
proxies:
  - name: "VMESS-01"
    type: vmess
    server: vmess.example.com
    port: 443
    uuid: 00000000-0000-0000-0000-000000000000
    alterId: 64
    cipher: auto
    network: ws
    tls: true
    udp: true
  - name: "VLESS-REALITY-01"
    type: vless
    server: vless.example.com
    port: 443
    uuid: 00000000-0000-0000-0000-000000000000
    network: tcp
    tls: true
    udp: true
    flow: xtls-rprx-vision
    servername: reality-sni.example.com
    client-fingerprint: chrome
    reality-opts:
      public-key: AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8
      short-id: 0123456789abcdef
  - name: "TROJAN-01"
    type: trojan
    server: trojan.example.com
    port: 443
    password: trojan-password
    sni: trojan-sni.example.com
    skip-cert-verify: true
    udp: true
  - name: "SS-OBFS-01"
    type: ss
    server: ss.example.com
    port: 8388
    cipher: aes-256-gcm
    password: ss-password
    plugin: obfs
    plugin-opts:
      mode: http
      host: obfs-host.example.com
    udp: true
  - name: "ANYTLS-01"
    type: anytls
    server: anytls.example.com
    port: 8443
    password: 00000000-0000-0000-0000-000000000000
    sni: anytls-sni.example.com
    skip-cert-verify: false
    udp: true
proxy-groups:
  - name: PROXY
    type: select
    proxies:
      - VMESS-01
      - VLESS-REALITY-01
rules:
  - DOMAIN-SUFFIX,example.com,DIRECT
  - MATCH,PROXY
`

func findNodeByName(t *testing.T, nodes []*Node, name string) *Node {
	t.Helper()
	for _, n := range nodes {
		if n.Name == name {
			return n
		}
	}
	t.Fatalf("node %q not found in %d parsed nodes", name, len(nodes))
	return nil
}

// 验收:Clash YAML 全文(含 #!MANAGED-CONFIG 注释头与 port/dns/rules 段)
// 解析出全部 proxies 节点,非 proxies 段安全忽略。
func TestParseWithStats_ClashYAML_FullConfig(t *testing.T) {
	result := ParseWithStats(clashYAMLFullConfig, "clash-source")

	if len(result.Nodes) != 5 {
		t.Fatalf("Nodes count = %d, want 5", len(result.Nodes))
	}
	if result.ParseFailures != 0 {
		t.Errorf("ParseFailures = %d, want 0", result.ParseFailures)
	}
	if result.TotalLines != 5 {
		t.Errorf("TotalLines = %d, want 5 (每项 proxy 计一行)", result.TotalLines)
	}
	for _, n := range result.Nodes {
		if n.Source != "clash-source" {
			t.Errorf("Node %q Source = %q, want clash-source", n.Name, n.Source)
		}
	}
}

// 验收:vless reality 全参数映射(servername→SNI、flow、reality-opts、client-fingerprint),
// 与分享链接路径产出一致。
func TestParseWithStats_ClashYAML_VLESSReality(t *testing.T) {
	result := ParseWithStats(clashYAMLFullConfig, "test")
	n := findNodeByName(t, result.Nodes, "VLESS-REALITY-01")

	if n.Type != "vless" {
		t.Errorf("Type = %q, want vless", n.Type)
	}
	if n.UUID != "00000000-0000-0000-0000-000000000000" {
		t.Errorf("UUID = %q, want all-zero uuid", n.UUID)
	}
	if n.Flow != "xtls-rprx-vision" {
		t.Errorf("Flow = %q, want xtls-rprx-vision", n.Flow)
	}
	if n.SNI != "reality-sni.example.com" {
		t.Errorf("SNI = %q, want reality-sni.example.com (servername 映射)", n.SNI)
	}
	if n.RealityPublicKey != "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8" {
		t.Errorf("RealityPublicKey = %q, want synthetic pbk", n.RealityPublicKey)
	}
	if n.RealityShortID != "0123456789abcdef" {
		t.Errorf("RealityShortID = %q, want 0123456789abcdef", n.RealityShortID)
	}
	if n.ClientFingerprint != "chrome" {
		t.Errorf("ClientFingerprint = %q, want chrome", n.ClientFingerprint)
	}
	if !n.TLS {
		t.Errorf("TLS = false, want true (tls: true)")
	}
	if n.Network != "tcp" {
		t.Errorf("Network = %q, want tcp", n.Network)
	}
}

// 验收:vless 的 sni 键优先于 servername(与 parseVLessNode 参数优先级一致)。
func TestParseWithStats_ClashYAML_VLESSSNIPriority(t *testing.T) {
	content := `proxies:
  - name: "VLESS-SNI"
    type: vless
    server: vless.example.com
    port: 443
    uuid: 00000000-0000-0000-0000-000000000000
    tls: true
    sni: sni-key.example.com
    servername: servername-key.example.com
`
	result := ParseWithStats(content, "test")
	n := findNodeByName(t, result.Nodes, "VLESS-SNI")
	if n.SNI != "sni-key.example.com" {
		t.Errorf("SNI = %q, want sni-key.example.com (sni 优先)", n.SNI)
	}
}

// 验收:vmess(alterId/cipher)正确导入。
func TestParseWithStats_ClashYAML_VMess(t *testing.T) {
	result := ParseWithStats(clashYAMLFullConfig, "test")
	n := findNodeByName(t, result.Nodes, "VMESS-01")

	if n.Type != "vmess" {
		t.Errorf("Type = %q, want vmess", n.Type)
	}
	if n.Server != "vmess.example.com" || n.Port != 443 {
		t.Errorf("Server:Port = %s:%d, want vmess.example.com:443", n.Server, n.Port)
	}
	if n.UUID != "00000000-0000-0000-0000-000000000000" {
		t.Errorf("UUID = %q, want all-zero uuid", n.UUID)
	}
	if n.AlterID != 64 {
		t.Errorf("AlterID = %d, want 64", n.AlterID)
	}
	if n.Cipher != "auto" {
		t.Errorf("Cipher = %q, want auto", n.Cipher)
	}
	if n.Network != "ws" {
		t.Errorf("Network = %q, want ws", n.Network)
	}
	if !n.TLS {
		t.Errorf("TLS = false, want true")
	}
}

// 验收:trojan(sni/skip-cert-verify→Insecure)正确导入。
func TestParseWithStats_ClashYAML_Trojan(t *testing.T) {
	result := ParseWithStats(clashYAMLFullConfig, "test")
	n := findNodeByName(t, result.Nodes, "TROJAN-01")

	if n.Type != "trojan" {
		t.Errorf("Type = %q, want trojan", n.Type)
	}
	if n.Password != "trojan-password" {
		t.Errorf("Password = %q, want trojan-password", n.Password)
	}
	if n.SNI != "trojan-sni.example.com" {
		t.Errorf("SNI = %q, want trojan-sni.example.com", n.SNI)
	}
	if !n.Insecure {
		t.Errorf("Insecure = false, want true (skip-cert-verify: true)")
	}
	if !n.TLS {
		t.Errorf("TLS = false, want true (trojan 协议基于 TLS,与行解析一致)")
	}
}

// 验收:ss obfs 插件映射为 SIP002 形态,与 parseShadowsocksNode 产出对齐。
func TestParseWithStats_ClashYAML_SSObfsPlugin(t *testing.T) {
	result := ParseWithStats(clashYAMLFullConfig, "test")
	n := findNodeByName(t, result.Nodes, "SS-OBFS-01")

	if n.Type != "ss" {
		t.Errorf("Type = %q, want ss", n.Type)
	}
	if n.Cipher != "aes-256-gcm" {
		t.Errorf("Cipher = %q, want aes-256-gcm", n.Cipher)
	}
	if n.Password != "ss-password" {
		t.Errorf("Password = %q, want ss-password", n.Password)
	}
	if n.Plugin != "simple-obfs" {
		t.Errorf("Plugin = %q, want simple-obfs (clash obfs 的 SIP002 形态)", n.Plugin)
	}
	if n.PluginOpts != "obfs=http;obfs-host=obfs-host.example.com" {
		t.Errorf("PluginOpts = %q, want obfs=http;obfs-host=obfs-host.example.com", n.PluginOpts)
	}
}

// 验收:anytls(password/sni/skip-cert-verify)正确导入。
func TestParseWithStats_ClashYAML_AnyTLS(t *testing.T) {
	result := ParseWithStats(clashYAMLFullConfig, "test")
	n := findNodeByName(t, result.Nodes, "ANYTLS-01")

	if n.Type != "anytls" {
		t.Errorf("Type = %q, want anytls", n.Type)
	}
	if n.Password != "00000000-0000-0000-0000-000000000000" {
		t.Errorf("Password = %q, want all-zero uuid", n.Password)
	}
	if n.SNI != "anytls-sni.example.com" {
		t.Errorf("SNI = %q, want anytls-sni.example.com", n.SNI)
	}
	if n.Insecure {
		t.Errorf("Insecure = true, want false (skip-cert-verify: false)")
	}
	if !n.TLS {
		t.Errorf("TLS = false, want true (anytls 始终基于 TLS,与行解析一致)")
	}
}

// 验收:network=grpc 与 grpc-opts.grpc-service-name 落模型。
func TestParseWithStats_ClashYAML_Grpc(t *testing.T) {
	content := `proxies:
  - name: "VLESS-GRPC-01"
    type: vless
    server: grpc.example.com
    port: 443
    uuid: 00000000-0000-0000-0000-000000000000
    network: grpc
    tls: true
    grpc-opts:
      grpc-service-name: TestService
`
	result := ParseWithStats(content, "test")
	n := findNodeByName(t, result.Nodes, "VLESS-GRPC-01")

	if n.Network != "grpc" {
		t.Errorf("Network = %q, want grpc", n.Network)
	}
	if n.GrpcServiceName != "TestService" {
		t.Errorf("GrpcServiceName = %q, want TestService", n.GrpcServiceName)
	}
}

// 验收:未知 type(hysteria2)跳过并计入解析失败,其余节点正常导入。
func TestParseWithStats_ClashYAML_UnknownTypeSkipped(t *testing.T) {
	content := `proxies:
  - name: "HY2-01"
    type: hysteria2
    server: hy2.example.com
    port: 443
    password: hy2-password
  - name: "VLESS-01"
    type: vless
    server: vless.example.com
    port: 443
    uuid: 00000000-0000-0000-0000-000000000000
`
	result := ParseWithStats(content, "test")

	if len(result.Nodes) != 1 {
		t.Fatalf("Nodes count = %d, want 1 (hysteria2 跳过)", len(result.Nodes))
	}
	if result.Nodes[0].Name != "VLESS-01" {
		t.Errorf("Nodes[0].Name = %q, want VLESS-01", result.Nodes[0].Name)
	}
	if result.ParseFailures != 1 {
		t.Errorf("ParseFailures = %d, want 1", result.ParseFailures)
	}
	if result.TotalLines != 2 {
		t.Errorf("TotalLines = %d, want 2", result.TotalLines)
	}
	if len(result.Failures) != 1 || !strings.Contains(result.Failures[0].Reason, "hysteria2") {
		t.Errorf("Failures = %+v, want 1 entry mentioning hysteria2", result.Failures)
	}
}

// 验收:全部节点无法解析时 0 节点(调用方据此报 "no valid nodes found")。
func TestParseWithStats_ClashYAML_AllUnknown(t *testing.T) {
	content := `proxies:
  - name: "HY2-01"
    type: hysteria2
    server: hy2.example.com
    port: 443
    password: hy2-password
`
	result := ParseWithStats(content, "test")

	if len(result.Nodes) != 0 {
		t.Errorf("Nodes count = %d, want 0", len(result.Nodes))
	}
	if result.ParseFailures != 1 || result.TotalLines != 1 {
		t.Errorf("ParseFailures=%d TotalLines=%d, want 1/1", result.ParseFailures, result.TotalLines)
	}
	// Line 必须是原文真实行号(hysteria2 项在第 2 行),不是 proxies 数组下标
	// (check 评审 M1:手动粘贴入口前端按编辑器行号展示失败明细)。
	if len(result.Failures) != 1 || result.Failures[0].Line != 2 {
		t.Errorf("Failures = %+v, want 1 条且 Line=2(原文行号)", result.Failures)
	}
}

// 验收:base64 分享链接列表嗅探天然落空走老路,行解析行为逐字节零回归。
// 走完整共享边界:DecodeSubscription(整体 base64) -> ParseWithStats。
func TestParseWithStats_LinkList_SniffFallbackZeroRegression(t *testing.T) {
	links := `vmess://eyJhZGQiOiJleGFtcGxlLmNvbSIsInBvcnQiOjQ0MywiaWQiOiIwMDAwMDAwMC0wMDAwLTAwMDAtMDAwMC0wMDAwMDAwMDAwMDAiLCJhaWQiOjAsIm5ldCI6InRjcCIsInRscyI6InRscyIsInBzIjoiVGVzdCBOb2RlIn0=
vless://00000000-0000-0000-0000-000000000000@example.com:443?security=tls#Test%20VLess
invalid-line-no-protocol`

	// 明文多行形态
	plain := ParseWithStats(links, "test")
	if len(plain.Nodes) != 2 || plain.ParseFailures != 1 || plain.TotalLines != 3 {
		t.Fatalf("plain: nodes=%d failures=%d total=%d, want 2/1/3",
			len(plain.Nodes), plain.ParseFailures, plain.TotalLines)
	}
	for _, n := range plain.Nodes {
		if n.RawLink == "" {
			t.Errorf("plain: node %q RawLink empty, want 行解析保留原始链接", n.Name)
		}
	}

	// 整体 base64 形态(机场面板标准导出)
	encoded := base64.StdEncoding.EncodeToString([]byte(links))
	decoded := DecodeSubscription([]byte(encoded))
	b64 := ParseWithStats(decoded, "test")
	if len(b64.Nodes) != 2 || b64.ParseFailures != 1 || b64.TotalLines != 3 {
		t.Fatalf("base64: nodes=%d failures=%d total=%d, want 2/1/3",
			len(b64.Nodes), b64.ParseFailures, b64.TotalLines)
	}
	for _, n := range b64.Nodes {
		if n.RawLink == "" {
			t.Errorf("base64: node %q RawLink empty, want 行解析保留原始链接", n.Name)
		}
	}
}

// 嗅探误判面钉死(check 评审 M2):含冒号元数据行、YAML 列表形态、注释里的
// proxies 字样等混合内容不得误命中 YAML 模式,必须回落行解析。
func TestParseWithStats_SniffMisjudgeFallback(t *testing.T) {
	link := "vless://00000000-0000-0000-0000-000000000000@example.com:443?security=tls#Test%20VLess"
	tests := []struct {
		name      string
		content   string
		wantNodes int
	}{
		{"ASCII 冒号元数据行 + 链接", "Traffic: 100GB\n" + link, 1},
		{"全角冒号中文元数据行 + 链接", "剩余流量:100GB\n" + link, 1},
		{"proxies 字样出现在注释里", "# proxies: not real\n" + link, 1},
		{"YAML 列表形态但非 proxies map", "- just a string\n- another string", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := ParseWithStats(tt.content, "test")
			if len(res.Nodes) != tt.wantNodes {
				t.Fatalf("Nodes = %d, want %d(嗅探误判或回落失败)", len(res.Nodes), tt.wantNodes)
			}
			for _, n := range res.Nodes {
				if n.RawLink == "" {
					t.Errorf("node %q RawLink empty, want 回落行解析保留原始链接", n.Name)
				}
			}
		})
	}
}

// 验收:元数据伪节点过滤在 YAML 路径同样生效(复用 isMetadataName 管道)。
func TestParseWithStats_ClashYAML_MetadataFiltered(t *testing.T) {
	content := `proxies:
  - name: "剩余流量:1024GB"
    type: vless
    server: meta.example.com
    port: 443
    uuid: 00000000-0000-0000-0000-000000000000
  - name: "REAL-01"
    type: vless
    server: real.example.com
    port: 443
    uuid: 00000000-0000-0000-0000-000000000000
`
	result := ParseWithStats(content, "test")

	if len(result.Nodes) != 1 {
		t.Fatalf("Nodes count = %d, want 1 (元数据伪节点被过滤)", len(result.Nodes))
	}
	if result.Nodes[0].Name != "REAL-01" {
		t.Errorf("Nodes[0].Name = %q, want REAL-01", result.Nodes[0].Name)
	}
	if result.ParseFailures != 0 {
		t.Errorf("ParseFailures = %d, want 0 (过滤不算失败,与行解析一致)", result.ParseFailures)
	}
}

// 验收:NodeKey 去重在 YAML 路径同样生效(复用 DedupeByNodeKey 管道,后条覆盖前条)。
func TestParseWithStats_ClashYAML_DedupeByNodeKey(t *testing.T) {
	content := `proxies:
  - name: "DUP-OLD"
    type: vless
    server: dup.example.com
    port: 443
    uuid: 00000000-0000-0000-0000-000000000000
  - name: "DUP-NEW"
    type: vless
    server: dup.example.com
    port: 443
    uuid: 00000000-0000-0000-0000-000000000000
`
	result := ParseWithStats(content, "test")
	if len(result.Nodes) != 2 {
		t.Fatalf("Nodes count = %d, want 2 (解析层不去重)", len(result.Nodes))
	}

	nodes := DedupeByNodeKey(result.Nodes)
	if len(nodes) != 1 {
		t.Fatalf("DedupeByNodeKey count = %d, want 1", len(nodes))
	}
	if nodes[0].Name != "DUP-NEW" {
		t.Errorf("Nodes[0].Name = %q, want DUP-NEW (后条覆盖前条)", nodes[0].Name)
	}
}

// 验收:YAML 来源节点 RawLink 为空。
func TestParseWithStats_ClashYAML_RawLinkEmpty(t *testing.T) {
	result := ParseWithStats(clashYAMLFullConfig, "test")
	if len(result.Nodes) == 0 {
		t.Fatal("no nodes parsed")
	}
	for _, n := range result.Nodes {
		if n.RawLink != "" {
			t.Errorf("Node %q RawLink = %q, want empty (YAML 来源无原始分享链接)", n.Name, n.RawLink)
		}
	}
}

// 验收:空 proxies 列表嗅探不命中(非空列表才命中),回落行解析后 0 节点。
func TestParseWithStats_ClashYAML_EmptyProxies(t *testing.T) {
	result := ParseWithStats("proxies: []\n", "test")

	if len(result.Nodes) != 0 {
		t.Errorf("Nodes count = %d, want 0", len(result.Nodes))
	}
	if result.ParseFailures == 0 {
		t.Errorf("ParseFailures = 0, want >0 (回落行解析后各行失败)")
	}
}

// 嗅探边界:YAML 顶层 map 但无 proxies 键时回落行解析。
func TestParseWithStats_YAMLWithoutProxiesKey_Fallback(t *testing.T) {
	content := `port: 7890
mode: rule
`
	result := ParseWithStats(content, "test")

	if len(result.Nodes) != 0 {
		t.Errorf("Nodes count = %d, want 0", len(result.Nodes))
	}
	if result.TotalLines != 2 || result.ParseFailures != 2 {
		t.Errorf("TotalLines=%d ParseFailures=%d, want 2/2 (行解析口径)",
			result.TotalLines, result.ParseFailures)
	}
}
