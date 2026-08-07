package xraymgr

import (
	"testing"

	"github.com/taliove/proxyhub/internal/subscription"
)

// gRPC 传输的 xray outbound 生成缝测试(ticket 2 / spec #72,issue #78):
// vless/vmess outbound 在 network=grpc 时生成
// streamSettings{network: grpc, grpcSettings{serviceName, authority(有则带)}};
// 与 reality 组合时 grpcSettings 与 realitySettings 并存于同一 streamSettings
// (security=reality、network=grpc)。fixture 全合成:example.com + 全零 UUID。

// grpcNode 返回一个字段齐全的 grpc vless 节点 fixture。
func grpcNode() *subscription.Node {
	return &subscription.Node{
		Name:            "grpc-01",
		Type:            "vless",
		Server:          "grpc.example.com",
		Port:            443,
		UUID:            testRealityUUID,
		Network:         "grpc",
		GrpcServiceName: "grpcsvc",
		GrpcAuthority:   "auth.example.com",
	}
}

// grpcSettingsOf 取出 streamSettings 下的 grpcSettings,缺失时直接 fail。
func grpcSettingsOf(t *testing.T, ss map[string]any) map[string]any {
	t.Helper()
	gs, ok := ss["grpcSettings"].(map[string]any)
	if !ok {
		t.Fatalf("grpcSettings missing in streamSettings: %v", ss)
	}
	return gs
}

// TestNodeToOutbound_GrpcVless 验证 grpc vless 节点生成
// streamSettings{network: grpc, grpcSettings{serviceName, authority}}。
func TestNodeToOutbound_GrpcVless(t *testing.T) {
	ob := nodeToOutbound(grpcNode(), 0)
	if ob == nil {
		t.Fatal("nodeToOutbound returned nil for grpc vless node")
	}
	ss := streamSettingsOf(t, ob)
	if ss["network"] != "grpc" {
		t.Errorf("network = %v, want grpc", ss["network"])
	}
	gs := grpcSettingsOf(t, ss)
	if gs["serviceName"] != "grpcsvc" {
		t.Errorf("serviceName = %v, want grpcsvc", gs["serviceName"])
	}
	if gs["authority"] != "auth.example.com" {
		t.Errorf("authority = %v, want auth.example.com", gs["authority"])
	}
	// 非 reality:不得混入 security/realitySettings
	if _, present := ss["security"]; present {
		t.Errorf("plain grpc must not gain security: %v", ss)
	}
	if _, present := ss["realitySettings"]; present {
		t.Errorf("plain grpc must not gain realitySettings: %v", ss)
	}
}

// TestNodeToOutbound_GrpcVlessEmptyAuthority 验证 authority 空时省略,
// serviceName 仍输出。
func TestNodeToOutbound_GrpcVlessEmptyAuthority(t *testing.T) {
	n := grpcNode()
	n.GrpcAuthority = ""

	ob := nodeToOutbound(n, 1)
	ss := streamSettingsOf(t, ob)
	gs := grpcSettingsOf(t, ss)
	if gs["serviceName"] != "grpcsvc" {
		t.Errorf("serviceName = %v, want grpcsvc", gs["serviceName"])
	}
	if _, present := gs["authority"]; present {
		t.Errorf("authority present with empty GrpcAuthority: %v", gs)
	}
}

// TestNodeToOutbound_GrpcRealityCoexist 验证 grpc+reality 组合:
// realitySettings 与 grpcSettings 并存于同一 streamSettings,
// security=reality、network=grpc;reality 分支已建 streamSettings 时
// 必须合并而非覆盖(ADR 0043 applyVlessReality 的四要素不丢)。
func TestNodeToOutbound_GrpcRealityCoexist(t *testing.T) {
	n := grpcNode()
	n.RealityPublicKey = testRealityPbk
	n.RealityShortID = "0123abcd"
	n.ClientFingerprint = "chrome"
	n.SNI = "sni.example.com"
	n.Flow = "xtls-rprx-vision"

	ob := nodeToOutbound(n, 2)
	ss := streamSettingsOf(t, ob)
	if ss["network"] != "grpc" {
		t.Errorf("network = %v, want grpc", ss["network"])
	}
	if ss["security"] != "reality" {
		t.Errorf("security = %v, want reality", ss["security"])
	}

	// reality 四要素不被 grpc 合并覆盖
	rs, ok := ss["realitySettings"].(map[string]any)
	if !ok {
		t.Fatalf("realitySettings missing in grpc+reality combo: %v", ss)
	}
	if rs["publicKey"] != testRealityPbk {
		t.Errorf("publicKey = %v, want %v", rs["publicKey"], testRealityPbk)
	}
	if rs["shortId"] != "0123abcd" {
		t.Errorf("shortId = %v, want 0123abcd", rs["shortId"])
	}
	if rs["serverName"] != "sni.example.com" {
		t.Errorf("serverName = %v, want sni.example.com", rs["serverName"])
	}
	if rs["fingerprint"] != "chrome" {
		t.Errorf("fingerprint = %v, want chrome", rs["fingerprint"])
	}

	// grpcSettings 与 realitySettings 并存
	gs := grpcSettingsOf(t, ss)
	if gs["serviceName"] != "grpcsvc" {
		t.Errorf("serviceName = %v, want grpcsvc", gs["serviceName"])
	}
	if gs["authority"] != "auth.example.com" {
		t.Errorf("authority = %v, want auth.example.com", gs["authority"])
	}

	// user 级 flow 仍在
	user := firstUserOf(t, ob)
	if user["flow"] != "xtls-rprx-vision" {
		t.Errorf("user flow = %v, want xtls-rprx-vision", user["flow"])
	}
}

// TestNodeToOutbound_GrpcVmess 验证 grpc vmess 节点同样生成
// grpcSettings(vmess 无 reality 分支,streamSettings 由 grpc 独立创建)。
func TestNodeToOutbound_GrpcVmess(t *testing.T) {
	n := &subscription.Node{
		Name:            "grpc-vmess",
		Type:            "vmess",
		Server:          "grpc.example.com",
		Port:            443,
		UUID:            testRealityUUID,
		Network:         "grpc",
		GrpcServiceName: "grpcsvc",
		GrpcAuthority:   "auth.example.com",
	}
	ob := nodeToOutbound(n, 3)
	if ob == nil {
		t.Fatal("nodeToOutbound returned nil for grpc vmess node")
	}
	ss := streamSettingsOf(t, ob)
	if ss["network"] != "grpc" {
		t.Errorf("network = %v, want grpc", ss["network"])
	}
	gs := grpcSettingsOf(t, ss)
	if gs["serviceName"] != "grpcsvc" {
		t.Errorf("serviceName = %v, want grpcsvc", gs["serviceName"])
	}
	if gs["authority"] != "auth.example.com" {
		t.Errorf("authority = %v, want auth.example.com", gs["authority"])
	}
}

// TestNodeToOutbound_NonGrpcNoStreamSettings 零回归:非 grpc 的
// vless/vmess 节点不新增 streamSettings。
func TestNodeToOutbound_NonGrpcNoStreamSettings(t *testing.T) {
	for _, typ := range []string{"vless", "vmess"} {
		n := &subscription.Node{
			Name:   "plain-" + typ,
			Type:   typ,
			Server: "plain.example.com",
			Port:   443,
			UUID:   testRealityUUID,
			// 模型残留 grpc 字段但 network 非 grpc:不得发射
			GrpcServiceName: "grpcsvc",
			GrpcAuthority:   "auth.example.com",
		}
		ob := nodeToOutbound(n, 4)
		if ob == nil {
			t.Fatalf("nodeToOutbound returned nil for %s", typ)
		}
		if _, present := ob["streamSettings"]; present {
			t.Errorf("non-grpc %s must not gain streamSettings: %v", typ, ob)
		}
	}
}
