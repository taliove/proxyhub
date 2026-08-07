package store

import "testing"

// spec #70 一键转自建的 store 级字段底座:SelfHostedNode 的
// SNI/Flow/RealityPublicKey/RealityShortID/ClientFingerprint 全量字段必须
// 完整落库并读回(create/update roundtrip),ToNode 不丢连接参数。
// fixture 全合成:example.com + 全零 UUID + 合成 pbk。

// syntheticRealitySelfNode 合成一个带完整 reality/grpc 参数的自建节点。
func syntheticRealitySelfNode() *SelfHostedNode {
	return &SelfHostedNode{
		Name:              "转自建",
		Protocol:          "vless",
		Server:            "pool.example.com",
		Port:              443,
		UUID:              "00000000-0000-0000-0000-000000000000",
		Network:           "grpc",
		TLS:               true,
		SNI:               "sni.example.com",
		Flow:              "xtls-rprx-vision",
		RealityPublicKey:  "synthetic-public-key",
		RealityShortID:    "01ab",
		ClientFingerprint: "chrome",
		GrpcServiceName:   "synthetic-svc",
		GrpcAuthority:     "authority.example.com",
		RegionCode:        "HK",
		Enabled:           true,
	}
}

func assertRealityFields(t *testing.T, got *SelfHostedNode) {
	t.Helper()
	checks := map[string][2]string{
		"SNI":               {got.SNI, "sni.example.com"},
		"Flow":              {got.Flow, "xtls-rprx-vision"},
		"RealityPublicKey":  {got.RealityPublicKey, "synthetic-public-key"},
		"RealityShortID":    {got.RealityShortID, "01ab"},
		"ClientFingerprint": {got.ClientFingerprint, "chrome"},
		"GrpcServiceName":   {got.GrpcServiceName, "synthetic-svc"},
		"GrpcAuthority":     {got.GrpcAuthority, "authority.example.com"},
	}
	for field, pair := range checks {
		if pair[0] != pair[1] {
			t.Errorf("%s = %q, want %q", field, pair[0], pair[1])
		}
	}
}

// 创建 → 读回:全量字段 roundtrip 保真。
func TestSelfHostedNode_RealityGrpcCreateRoundtrip(t *testing.T) {
	s := newTestStore(t)

	if err := s.CreateSelfHostedNodeForUser(1, syntheticRealitySelfNode()); err != nil {
		t.Fatalf("create: %v", err)
	}
	rows, err := s.ListAllSelfHostedNodesByUser(1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	assertRealityFields(t, rows[0])

	// ToNode 输出对照:连接参数一个都不能丢
	n := rows[0].ToNode()
	if n.SNI != "sni.example.com" || n.Flow != "xtls-rprx-vision" ||
		n.RealityPublicKey != "synthetic-public-key" || n.RealityShortID != "01ab" ||
		n.ClientFingerprint != "chrome" {
		t.Errorf("ToNode reality 字段不保真: %+v", n)
	}
	if n.GrpcServiceName != "synthetic-svc" || n.GrpcAuthority != "authority.example.com" {
		t.Errorf("ToNode grpc 字段不保真: svc=%q auth=%q", n.GrpcServiceName, n.GrpcAuthority)
	}
	if !n.TLS || n.Network != "grpc" {
		t.Errorf("ToNode tls=%v network=%q, want tls=true network=grpc", n.TLS, n.Network)
	}
}

// 更新 → 读回:编辑路径同样不丢 reality/grpc 字段。
func TestSelfHostedNode_RealityGrpcUpdateRoundtrip(t *testing.T) {
	s := newTestStore(t)

	if err := s.CreateSelfHostedNodeForUser(1, syntheticRealitySelfNode()); err != nil {
		t.Fatalf("create: %v", err)
	}
	rows, err := s.ListAllSelfHostedNodesByUser(1)
	if err != nil || len(rows) != 1 {
		t.Fatalf("list: n=%d err=%v", len(rows), err)
	}
	node := rows[0]
	node.Name = "转自建-改名"
	if err := s.UpdateSelfHostedNodeForUser(1, node); err != nil {
		t.Fatalf("update: %v", err)
	}

	after, err := s.ListAllSelfHostedNodesByUser(1)
	if err != nil || len(after) != 1 {
		t.Fatalf("list after update: n=%d err=%v", len(after), err)
	}
	if after[0].Name != "转自建-改名" {
		t.Errorf("name = %q, want 转自建-改名", after[0].Name)
	}
	assertRealityFields(t, after[0])
}

// 历史行兼容:未填 reality 字段的旧节点(零值)roundtrip 后仍为零值,
// 不污染普通 VLESS/VMess 节点。
func TestSelfHostedNode_RealityFieldsDefaultEmpty(t *testing.T) {
	s := newTestStore(t)

	in := &SelfHostedNode{
		Name: "普通节点", Protocol: "vmess", Server: "plain.example.com", Port: 10086,
		UUID: "00000000-0000-0000-0000-000000000000", Network: "ws", Enabled: true,
	}
	if err := s.CreateSelfHostedNodeForUser(1, in); err != nil {
		t.Fatalf("create: %v", err)
	}
	rows, err := s.ListAllSelfHostedNodesByUser(1)
	if err != nil || len(rows) != 1 {
		t.Fatalf("list: n=%d err=%v", len(rows), err)
	}
	got := rows[0]
	if got.SNI != "" || got.Flow != "" || got.RealityPublicKey != "" ||
		got.RealityShortID != "" || got.ClientFingerprint != "" {
		t.Errorf("普通节点 reality 字段应为空: %+v", got)
	}
}
