package xraymgr

import (
	"testing"

	"github.com/taliove/proxyhub/internal/subscription"
)

// reality 节点的 xray outbound 生成缝测试(spec #58 / issue #62):
// RealityPublicKey 非空即 reality 节点,outbound 必须带
// streamSettings{security: reality, realitySettings{...}} 与 user 级 flow。
// fixture 全合成:example.com 域名 + 全零 UUID + 合成 pbk。

const (
	testRealityUUID = "00000000-0000-0000-0000-000000000000"
	testRealityPbk  = "0000000000000000000000000000000000000000000"
)

// realityNode 返回一个字段齐全的 reality vless 节点 fixture。
func realityNode() *subscription.Node {
	return &subscription.Node{
		Name:              "reality-01",
		Type:              "vless",
		Server:            "reality.example.com",
		Port:              443,
		UUID:              testRealityUUID,
		Network:           "tcp",
		SNI:               "sni.example.com",
		Flow:              "xtls-rprx-vision",
		RealityPublicKey:  testRealityPbk,
		RealityShortID:    "0123abcd",
		ClientFingerprint: "chrome",
	}
}

// streamSettingsOf 取出 outbound 的 streamSettings,缺失时直接 fail。
func streamSettingsOf(t *testing.T, ob map[string]any) map[string]any {
	t.Helper()
	ss, ok := ob["streamSettings"].(map[string]any)
	if !ok {
		t.Fatalf("streamSettings missing in outbound: %v", ob)
	}
	return ss
}

// firstUserOf 取出 outbound 第一个 vnext user,缺失时直接 fail。
func firstUserOf(t *testing.T, ob map[string]any) map[string]any {
	t.Helper()
	settings := ob["settings"].(map[string]any)
	vnext := settings["vnext"].([]map[string]any)
	users := vnext[0]["users"].([]map[string]any)
	return users[0]
}

// TestNodeToOutbound_RealityVless 验证 reality 节点生成完整 outbound:
// security=reality、realitySettings 四要素、user 级 flow。
func TestNodeToOutbound_RealityVless(t *testing.T) {
	ob := nodeToOutbound(realityNode(), 0)
	if ob == nil {
		t.Fatal("nodeToOutbound returned nil for reality vless node")
	}
	if ob["protocol"] != "vless" {
		t.Errorf("protocol = %v, want vless", ob["protocol"])
	}

	ss := streamSettingsOf(t, ob)
	if ss["network"] != "tcp" {
		t.Errorf("network = %v, want tcp", ss["network"])
	}
	if ss["security"] != "reality" {
		t.Errorf("security = %v, want reality", ss["security"])
	}
	rs, ok := ss["realitySettings"].(map[string]any)
	if !ok {
		t.Fatalf("realitySettings missing: %v", ss)
	}
	if rs["serverName"] != "sni.example.com" {
		t.Errorf("serverName = %v, want sni.example.com", rs["serverName"])
	}
	if rs["publicKey"] != testRealityPbk {
		t.Errorf("publicKey = %v, want %v", rs["publicKey"], testRealityPbk)
	}
	if rs["shortId"] != "0123abcd" {
		t.Errorf("shortId = %v, want 0123abcd", rs["shortId"])
	}
	if rs["fingerprint"] != "chrome" {
		t.Errorf("fingerprint = %v, want chrome", rs["fingerprint"])
	}

	user := firstUserOf(t, ob)
	if user["flow"] != "xtls-rprx-vision" {
		t.Errorf("user flow = %v, want xtls-rprx-vision", user["flow"])
	}
	if user["id"] != testRealityUUID || user["encryption"] != "none" {
		t.Errorf("user = %v, want id + encryption=none preserved", user)
	}
}

// TestNodeToOutbound_RealityDefaults 验证缺省补齐:fp 空时 fingerprint
// 落 chrome,network 空时落 tcp;flow / sid / serverName 空则不输出对应键
// (空值约定与 clash/v2ray 两处对齐)。
func TestNodeToOutbound_RealityDefaults(t *testing.T) {
	n := realityNode()
	n.ClientFingerprint = ""
	n.Network = ""
	n.Flow = ""
	n.RealityShortID = ""
	n.SNI = ""

	ob := nodeToOutbound(n, 1)
	ss := streamSettingsOf(t, ob)
	if ss["network"] != "tcp" {
		t.Errorf("network = %v, want tcp (empty default)", ss["network"])
	}
	rs := ss["realitySettings"].(map[string]any)
	if rs["fingerprint"] != "chrome" {
		t.Errorf("fingerprint = %v, want chrome (empty default)", rs["fingerprint"])
	}
	if _, present := rs["shortId"]; present {
		t.Errorf("shortId present with empty RealityShortID: %v", rs)
	}
	if _, present := rs["serverName"]; present {
		t.Errorf("serverName present with empty SNI: %v", rs)
	}

	user := firstUserOf(t, ob)
	if _, present := user["flow"]; present {
		t.Errorf("flow present with empty Flow: %v", user)
	}
}

// TestNodeToOutbound_PlainVlessUnchanged 零回归:非 reality vless 节点
// 不新增 streamSettings,user 不带 flow。
func TestNodeToOutbound_PlainVlessUnchanged(t *testing.T) {
	n := &subscription.Node{
		Name:   "plain-vless",
		Type:   "vless",
		Server: "plain.example.com",
		Port:   8443,
		UUID:   testRealityUUID,
	}
	ob := nodeToOutbound(n, 2)
	if ob == nil {
		t.Fatal("nodeToOutbound returned nil for plain vless node")
	}
	if _, present := ob["streamSettings"]; present {
		t.Errorf("plain vless must not gain streamSettings: %v", ob)
	}
	user := firstUserOf(t, ob)
	if _, present := user["flow"]; present {
		t.Errorf("plain vless user must not gain flow: %v", user)
	}
	if user["id"] != testRealityUUID || user["encryption"] != "none" {
		t.Errorf("user = %v, want id + encryption=none", user)
	}
}

// TestNodeToOutbound_UnsupportedStillNil 零回归:不支持的节点类型仍返回
// nil 被跳过。
func TestNodeToOutbound_UnsupportedStillNil(t *testing.T) {
	n := &subscription.Node{Type: "hysteria2", Server: "example.com", Port: 443}
	if ob := nodeToOutbound(n, 3); ob != nil {
		t.Errorf("nodeToOutbound(hysteria2) = %v, want nil", ob)
	}
}
