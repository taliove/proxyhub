package subscription

import (
	"encoding/base64"
	"testing"
)

func TestParseShadowsocksNode(t *testing.T) {
	f := &Fetcher{}

	tests := []struct {
		name     string
		line     string
		wantErr  bool
		wantNode *Node
	}{
		{
			name:    "SIP002 format with plugin",
			line:    "ss://YWVzLTEyOC1nY206ZmFrZXB3MTIzNDU2Nzg@00000000-0000-0000-0000-000000000000.example.com:12022/?plugin=simple-obfs%3Bobfs%3Dhttp%3Bobfs-host%3Dobfs.example.com#%F0%9F%87%AD%F0%9F%87%B0%20%E9%A6%99%E6%B8%AF%2001%E4%B8%A81x%20HK",
			wantErr: false,
			wantNode: &Node{
				Type:   "ss",
				Server: "00000000-0000-0000-0000-000000000000.example.com",
				Port:   12022,
				Cipher: "aes-128-gcm",
				Region: "HK",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node, err := f.parseShadowsocksNode(tt.line, "test")
			if (err != nil) != tt.wantErr {
				t.Errorf("parseShadowsocksNode() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err == nil {
				if node.Type != tt.wantNode.Type {
					t.Errorf("Type = %v, want %v", node.Type, tt.wantNode.Type)
				}
				if node.Server != tt.wantNode.Server {
					t.Errorf("Server = %v, want %v", node.Server, tt.wantNode.Server)
				}
				if node.Port != tt.wantNode.Port {
					t.Errorf("Port = %v, want %v", node.Port, tt.wantNode.Port)
				}
				if node.Cipher != tt.wantNode.Cipher {
					t.Errorf("Cipher = %v, want %v", node.Cipher, tt.wantNode.Cipher)
				}
				if node.Region != tt.wantNode.Region {
					t.Errorf("Region = %v, want %v", node.Region, tt.wantNode.Region)
				}
			}
		})
	}
}

func TestParseAnyTLSNode(t *testing.T) {
	f := &Fetcher{}

	tests := []struct {
		name     string
		line     string
		wantErr  bool
		wantNode *Node
	}{
		{
			name:    "anytls with sni and insecure",
			line:    "anytls://00000000-0000-0000-0000-000000000000@node1.example.com:28537?sni=sni.example.com&insecure=1#%F0%9F%87%AD%F0%9F%87%B0%20%E9%A6%99%E6%B8%AF01",
			wantErr: false,
			wantNode: &Node{
				Type:     "anytls",
				Server:   "node1.example.com",
				Port:     28537,
				Password: "00000000-0000-0000-0000-000000000000",
				SNI:      "sni.example.com",
				Insecure: true,
				TLS:      true,
				Region:   "HK",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node, err := f.parseAnyTLSNode(tt.line, "test")
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseAnyTLSNode() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if node.Type != tt.wantNode.Type {
				t.Errorf("Type = %v, want %v", node.Type, tt.wantNode.Type)
			}
			if node.Server != tt.wantNode.Server {
				t.Errorf("Server = %v, want %v", node.Server, tt.wantNode.Server)
			}
			if node.Port != tt.wantNode.Port {
				t.Errorf("Port = %v, want %v", node.Port, tt.wantNode.Port)
			}
			if node.Password != tt.wantNode.Password {
				t.Errorf("Password = %v, want %v", node.Password, tt.wantNode.Password)
			}
			if node.SNI != tt.wantNode.SNI {
				t.Errorf("SNI = %v, want %v", node.SNI, tt.wantNode.SNI)
			}
			if node.Insecure != tt.wantNode.Insecure {
				t.Errorf("Insecure = %v, want %v", node.Insecure, tt.wantNode.Insecure)
			}
			if node.Region != tt.wantNode.Region {
				t.Errorf("Region = %v, want %v", node.Region, tt.wantNode.Region)
			}
		})
	}
}

// TestParseSkipsMetadataLines 验证套餐信息伪节点（剩余流量/套餐到期/距离下次重置）
// 被解析层跳过，不会污染节点池。
func TestParseSkipsMetadataLines(t *testing.T) {
	f := &Fetcher{}
	content := "anytls://uuid@h.example.com:28537?sni=a.example.com&insecure=1#%E5%89%A9%E4%BD%99%E6%B5%81%E9%87%8F%EF%BC%9A188.98%20GB\n" +
		"anytls://uuid@h.example.com:28537?sni=b.example.com&insecure=1#%F0%9F%87%AD%F0%9F%87%B0%20%E9%A6%99%E6%B8%AF01"
	nodes, err := f.parse(content, "test")
	if err != nil {
		t.Fatalf("parse() error = %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("parse() = %d nodes, want 1 (metadata line must be skipped)", len(nodes))
	}
	if nodes[0].Name == "" || nodes[0].Region != "HK" {
		t.Errorf("expected 香港01 node kept, got name=%q region=%q", nodes[0].Name, nodes[0].Region)
	}
}

func TestParseSubscription(t *testing.T) {
	// Test整体订阅解析：base64外壳 + 内部多个SS节点
	// 这是机场返回的原始 base64（需要先解码一层才能得到 ss:// 行）
	rawSub := "c3M6Ly9ZV1Z6TFRFeU9DMW5ZMjA2YUdGTlRFMVlhWEpDZVc0MmNrZFdhQUBhODU2NGZlNi1kNDVhLTQ1ODUtODZlMy04ZTRlOGYxOTEzMWYuc2Fua3VhZWkuY29tOjEyMDIyLz9wbHVnaW49c2ltcGxlLW9iZnMlM0JvYmZzJTNEaHR0cCUzQm9iZnMtaG9zdCUzRGMyZDkzNTAxZTU2Mi5taWNyb3NvZnQuY29tIyVGMCU5RiU4NyVBRCVGMCU5RiU4NyVCMCUyMCVFOSVBNiU5OSVFNiVCOCVBRiUyMDAxJUU0JUI4JUE4MXglMjBISw0Kc3M6Ly9ZV1Z6TFRFeU9DMW5ZMjA2YUdGTlRFMVlhWEpDZVc0MmNrZFdhQUBhODU2NGZlNi1kNDVhLTQ1ODUtODZlMy04ZTRlOGYxOTEzMWYuc2Fua3VhZWkuY29tOjEyMDIzLz9wbHVnaW49c2ltcGxlLW9iZnMlM0JvYmZzJTNEaHR0cCUzQm9iZnMtaG9zdCUzRGMyZDkzNTAxZTU2Mi5taWNyb3NvZnQuY29tIyVGMCU5RiU4NyVBRCVGMCU5RiU4NyVCMCUyMCVFOSVBNiU5OSVFNiVCOCVBRiUyMDAyJUU0JUI4JUE4MXglMjBISw=="

	// 先手动解码第一层 base64 (模拟 Fetch 里的逻辑)
	decoded, err := base64.RawStdEncoding.DecodeString(rawSub)
	if err != nil {
		decoded, err = base64.StdEncoding.DecodeString(rawSub)
		if err != nil {
			t.Fatalf("decode subscription: %v", err)
		}
	}

	f := &Fetcher{}
	nodes, err := f.parse(string(decoded), "test")
	if err != nil {
		t.Fatalf("parse() error = %v", err)
	}
	if len(nodes) < 2 {
		t.Fatalf("parse() returned %d nodes, want >= 2", len(nodes))
	}
	t.Logf("✓ Parsed %d nodes", len(nodes))
	t.Logf("First node: %s | %s:%d", nodes[0].Name, nodes[0].Server, nodes[0].Port)
}

func TestExtractRegion_EmojiFlags(t *testing.T) {
	tests := []struct{ name, want string }{
		{"🇭🇰 香港01", "HK"},
		{"🇧🇩 孟加拉", "BD"},
		{"🇰🇿 哈萨克斯坦", "KZ"},
		{"🇮🇶 伊拉克", "IQ"},
		{"🇰🇭 柬埔寨", "KH"},
		{"🇪🇬 埃及", "EG"},
		{"🇹🇷 土耳其", "TR"},
		{"🇮🇱 以色列", "IL"},
		{"🇵🇰 巴基斯坦", "PK"},
		{"🇬🇷 希腊", "GR"},
		{"🇪🇸 西班牙", "ES"},
		{"🇳🇴 挪威", "NO"},
		{"🇫🇷 法国", "FR"},
		{"🇳🇬 尼日利亚02", "NG"},
		{"🇬🇧 英国家宽", "GB"},
		{"🇨🇳 台湾家宽", "TW"},
		{"🇩🇪 德国", "DE"},
		{"🇰🇷 韩国01", "KR"},
		{"🇦🇺 澳大利亚01", "AU"},
		{"🇷🇺 莫斯科", "RU"},
		{"🇨🇦 加拿大", "CA"},
		{"🇺🇸 美国原生01", "US"},
		{"🇯🇵 日本原生", "JP"},
		{"🇸🇬 新加坡原生", "SG"},
	}
	for _, tt := range tests {
		got := extractRegion(tt.name)
		if got != tt.want {
			t.Errorf("extractRegion(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestExtractRegion_ChineseNames(t *testing.T) {
	tests := []struct{ name, want string }{
		{"孟加拉国节点", "BD"},
		{"哈萨克斯坦-01", "KZ"},
		{"伊拉克专线", "IQ"},
		{"柬埔寨高速", "KH"},
	}
	for _, tt := range tests {
		got := extractRegion(tt.name)
		if got != tt.want {
			t.Errorf("extractRegion(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}
