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
