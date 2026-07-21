package generator

import (
	"strings"
	"testing"

	"github.com/taliove/proxyhub/internal/subscription"
)

// TestClashProxy_SSWithObfsPlugin 验证带 simple-obfs 插件的 SS 节点导出为
// Clash 配置时保留 plugin/plugin-opts/udp,否则节点在客户端不可用。
func TestClashProxy_SSWithObfsPlugin(t *testing.T) {
	node := &subscription.Node{
		Name: "香港-01", Type: "ss", Server: "hk1.example.com", Port: 12022,
		Cipher: "aes-128-gcm", Password: "pass-1",
		Plugin: "simple-obfs", PluginOpts: "obfs=http;obfs-host=obfs.example.com",
	}

	proxy, err := ClashProxy(node, node.Name)
	if err != nil {
		t.Fatalf("ClashProxy() error = %v", err)
	}

	if got := proxy["plugin"]; got != "obfs" {
		t.Errorf("plugin = %v, want obfs", got)
	}
	opts, ok := proxy["plugin-opts"].(map[string]any)
	if !ok {
		t.Fatalf("plugin-opts missing or wrong type: %v", proxy["plugin-opts"])
	}
	if opts["mode"] != "http" {
		t.Errorf("plugin-opts.mode = %v, want http", opts["mode"])
	}
	if opts["host"] != "obfs.example.com" {
		t.Errorf("plugin-opts.host = %v, want obfs.example.com", opts["host"])
	}
	if proxy["udp"] != true {
		t.Errorf("udp = %v, want true", proxy["udp"])
	}
}

// TestClashProxy_SSV2rayPlugin 验证 v2ray-plugin 的 opts 键原样透传,裸 tls 标记转 true。
func TestClashProxy_SSV2rayPlugin(t *testing.T) {
	node := &subscription.Node{
		Name: "日本-01", Type: "ss", Server: "jp1.example.com", Port: 443,
		Cipher: "aes-128-gcm", Password: "pass-1",
		Plugin: "v2ray-plugin", PluginOpts: "mode=websocket;host=cdn.example.com;path=/ws;tls",
	}

	proxy, err := ClashProxy(node, node.Name)
	if err != nil {
		t.Fatalf("ClashProxy() error = %v", err)
	}

	if got := proxy["plugin"]; got != "v2ray-plugin" {
		t.Errorf("plugin = %v, want v2ray-plugin", got)
	}
	opts, ok := proxy["plugin-opts"].(map[string]any)
	if !ok {
		t.Fatalf("plugin-opts missing or wrong type: %v", proxy["plugin-opts"])
	}
	if opts["mode"] != "websocket" || opts["host"] != "cdn.example.com" || opts["path"] != "/ws" {
		t.Errorf("plugin-opts = %v, want mode/host/path passthrough", opts)
	}
	if opts["tls"] != true {
		t.Errorf("plugin-opts.tls = %v, want true", opts["tls"])
	}
}

// TestClashProxy_SSWithoutPlugin 验证无插件节点不带 plugin 键,但保留 udp: true。
func TestClashProxy_SSWithoutPlugin(t *testing.T) {
	node := &subscription.Node{
		Name: "新加坡-01", Type: "ss", Server: "sg1.example.com", Port: 8388,
		Cipher: "aes-256-gcm", Password: "pass-2",
	}

	proxy, err := ClashProxy(node, node.Name)
	if err != nil {
		t.Fatalf("ClashProxy() error = %v", err)
	}

	if _, exists := proxy["plugin"]; exists {
		t.Errorf("plugin should be absent, got %v", proxy["plugin"])
	}
	if _, exists := proxy["plugin-opts"]; exists {
		t.Errorf("plugin-opts should be absent, got %v", proxy["plugin-opts"])
	}
	if proxy["udp"] != true {
		t.Errorf("udp = %v, want true", proxy["udp"])
	}
}

// TestSSLink_PluginRoundTrip 验证 ss:// 分享链接回写 SIP002 plugin 参数,
// 格式与机场原始链接一致(plugin=simple-obfs%3Bobfs%3Dhttp%3B...)。
func TestSSLink_PluginRoundTrip(t *testing.T) {
	node := &subscription.Node{
		Name: "香港-01", Type: "ss", Server: "hk1.example.com", Port: 12022,
		Cipher: "aes-128-gcm", Password: "pass-1",
		Plugin: "simple-obfs", PluginOpts: "obfs=http;obfs-host=obfs.example.com",
	}

	link, err := GenerateShareURI(node)
	if err != nil {
		t.Fatalf("GenerateShareURI() error = %v", err)
	}

	wantPlugin := "/?plugin=simple-obfs%3Bobfs%3Dhttp%3Bobfs-host%3Dobfs.example.com#"
	if !strings.Contains(link, wantPlugin) {
		t.Errorf("link = %s, want plugin segment %s", link, wantPlugin)
	}
}

// TestSSLink_NoPlugin 验证无插件节点链接保持原样(不带 ? 查询串)。
func TestSSLink_NoPlugin(t *testing.T) {
	node := &subscription.Node{
		Name: "新加坡-01", Type: "ss", Server: "sg1.example.com", Port: 8388,
		Cipher: "aes-256-gcm", Password: "pass-2",
	}

	link, err := GenerateShareURI(node)
	if err != nil {
		t.Fatalf("GenerateShareURI() error = %v", err)
	}
	if strings.Contains(link, "?") {
		t.Errorf("link = %s, want no query string", link)
	}
}
