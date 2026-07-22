package generator

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/taliove/proxyhub/internal/subscription"
)

func sampleNodes() []*subscription.Node {
	return []*subscription.Node{
		{Name: "香港-01", Type: "vmess", Server: "hk1.example.com", Port: 443,
			UUID: "uuid-1", AlterID: 0, Network: "ws", TLS: true, Region: "HK", Latency: 50},
		{Name: "日本-01", Type: "trojan", Server: "jp1.example.com", Port: 443,
			Password: "pass-1", TLS: true, Region: "JP", Latency: 80},
		{Name: "新加坡-01", Type: "ss", Server: "sg1.example.com", Port: 8388,
			Cipher: "aes-256-gcm", Password: "pass-2", Region: "SG", Latency: 100},
		{Name: "美国-01", Type: "vless", Server: "us1.example.com", Port: 443,
			UUID: "uuid-2", TLS: true, Region: "US", Latency: 200},
	}
}

func TestGenerateV2Ray(t *testing.T) {
	data, err := GenerateV2Ray(sampleNodes())
	if err != nil {
		t.Fatalf("GenerateV2Ray() error = %v", err)
	}

	decoded, err := base64.StdEncoding.DecodeString(string(data))
	if err != nil {
		t.Fatalf("output is not valid base64: %v", err)
	}

	links := strings.Split(string(decoded), "\n")
	if len(links) != 4 {
		t.Fatalf("len(links) = %d, want 4", len(links))
	}

	prefixes := []string{"vmess://", "trojan://", "ss://", "vless://"}
	for _, prefix := range prefixes {
		found := false
		for _, link := range links {
			if strings.HasPrefix(link, prefix) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("no link with prefix %s", prefix)
		}
	}
}

func TestGenerateV2Ray_RoundTrip(t *testing.T) {
	// 生成的 vmess 链接应该能被我们自己的解析器解析回来
	nodes := []*subscription.Node{
		{Name: "香港-01", Type: "vmess", Server: "hk1.example.com", Port: 443,
			UUID: "uuid-1", AlterID: 0, Network: "ws", TLS: true},
	}

	data, err := GenerateV2Ray(nodes)
	if err != nil {
		t.Fatalf("GenerateV2Ray() error = %v", err)
	}

	decoded, _ := base64.StdEncoding.DecodeString(string(data))
	link := strings.TrimSpace(string(decoded))

	if !strings.HasPrefix(link, "vmess://") {
		t.Fatalf("link = %s, want vmess:// prefix", link)
	}

	payload, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(link, "vmess://"))
	if err != nil {
		t.Fatalf("vmess payload is not valid base64: %v", err)
	}

	for _, want := range []string{"hk1.example.com", "uuid-1", "香港-01"} {
		if !strings.Contains(string(payload), want) {
			t.Errorf("payload missing %s", want)
		}
	}
}

func TestGenerateV2Ray_Empty(t *testing.T) {
	if _, err := GenerateV2Ray(nil); err == nil {
		t.Error("GenerateV2Ray(nil) expected error, got nil")
	}
}

// 分享链接 fragment(备注)中的空格必须编码为 %20 而不是 +:
// + 是 query 表单编码约定,fragment 只做 percent-decode,Shadowrocket 等客户端
// 会把 + 原样显示在备注里。
func TestShareLink_FragmentSpaceEncoding(t *testing.T) {
	name := "HK 香港 01"
	nodes := []*subscription.Node{
		{Name: name, Type: "vless", Server: "us1.example.com", Port: 443,
			UUID: "00000000-0000-0000-0000-000000000000", TLS: true},
		{Name: name, Type: "trojan", Server: "jp1.example.com", Port: 443,
			Password: "pass-1", TLS: true},
		{Name: name, Type: "ss", Server: "sg1.example.com", Port: 8388,
			Cipher: "aes-256-gcm", Password: "pass-2"},
		{Name: name, Type: "anytls", Server: "hk1.example.com", Port: 28537,
			Password: "pass-3", SNI: "sni.example.com"},
	}

	for _, node := range nodes {
		link, err := shareLink(node)
		if err != nil {
			t.Fatalf("shareLink(%s) error = %v", node.Type, err)
		}
		frag := link[strings.LastIndex(link, "#")+1:]
		if strings.Contains(frag, "+") {
			t.Errorf("%s fragment %q contains +, space must be %%20", node.Type, frag)
		}
		if !strings.Contains(frag, "HK%20%E9%A6%99%E6%B8%AF%2001") {
			t.Errorf("%s fragment %q, want HK%%20...%%2001", node.Type, frag)
		}
	}
}

func TestNormalizeShareURIFragment(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "no fragment passthrough",
			in:   "vmess://eyJ2IjoiMiJ9",
			want: "vmess://eyJ2IjoiMiJ9",
		},
		{
			name: "literal plus becomes %20",
			in:   "trojan://pass@example.com:443#HK+01",
			want: "trojan://pass@example.com:443#HK%2001",
		},
		{
			name: "encoded plus round-trips as %2B",
			in:   "trojan://pass@example.com:443#A%2BB",
			want: "trojan://pass@example.com:443#A%2BB",
		},
		{
			name: "invalid escape preserved verbatim",
			in:   "ss://e30@example.com:8388#100%zz",
			want: "ss://e30@example.com:8388#100%zz",
		},
		{
			name: "body untouched before fragment",
			in:   "ss://e30@example.com:8388/?plugin=a%3Bb%3Dc#x+y",
			want: "ss://e30@example.com:8388/?plugin=a%3Bb%3Dc#x%20y",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeShareURIFragment(tt.in); got != tt.want {
				t.Errorf("NormalizeShareURIFragment(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
