package store

import "testing"

// 自建节点 reality/grpc 字段的 store 缝:SelfHostedNode 的
// SNI/Flow/RealityPublicKey/RealityShortID/ClientFingerprint/GrpcAuthority 必须
// 完整落库并读回(create roundtrip),ToNode 不丢连接参数;
// UPDATE 只写编辑面持有的列,不持有的列保持原值(pre-push 评审 H1)。
// fixture 全合成:example.com + 全零 UUID + 合成 pbk。

// syntheticRealitySelfNode 合成一个带完整 reality/grpc 参数的自建节点。
func syntheticRealitySelfNode() *SelfHostedNode {
	return &SelfHostedNode{
		Name:              "合成Reality",
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

// 更新 → 读回:编辑面不持有的列(sni/flow/reality_*)保持原值——
// UI 表单不含这些字段,请求体解码零值不得静默擦除既有 reality 参数(H1 回归)。
// (grpc_authority 自 2026-08 起归编辑面:gRPC Host 进自建表单,更新即生效。)
func TestSelfHostedNode_RealityGrpcPreservedOnUpdate(t *testing.T) {
	s := newTestStore(t)

	if err := s.CreateSelfHostedNodeForUser(1, syntheticRealitySelfNode()); err != nil {
		t.Fatalf("create: %v", err)
	}
	rows, err := s.ListAllSelfHostedNodesByUser(1)
	if err != nil || len(rows) != 1 {
		t.Fatalf("list: n=%d err=%v", len(rows), err)
	}
	// 模拟 UI 编辑请求的形状:表单字段全部回带(含 grpc_authority),其余零值。
	ui := &SelfHostedNode{
		ID: rows[0].ID, Name: "改名后", Protocol: "vless",
		Server: "pool.example.com", Port: 443,
		UUID: "00000000-0000-0000-0000-000000000000",
		Network: "grpc", TLS: true, GrpcServiceName: "renamed-svc",
		GrpcAuthority: "auth2.example.com", // 编辑面持有:改写应生效
		Enabled:       true,
	}
	if err := s.UpdateSelfHostedNodeForUser(1, ui); err != nil {
		t.Fatalf("update: %v", err)
	}

	after, err := s.ListAllSelfHostedNodesByUser(1)
	if err != nil || len(after) != 1 {
		t.Fatalf("list after update: n=%d err=%v", len(after), err)
	}
	if after[0].Name != "改名后" || after[0].GrpcServiceName != "renamed-svc" {
		t.Errorf("编辑面字段未生效: name=%q svc=%q", after[0].Name, after[0].GrpcServiceName)
	}
	if after[0].GrpcAuthority != "auth2.example.com" {
		t.Errorf("GrpcAuthority = %q, want auth2.example.com (编辑面持有,改写生效)", after[0].GrpcAuthority)
	}
	// 非编辑面字段全部保留原值(service_name/authority 归编辑面,上方已断言改写生效)
	got := after[0]
	preserved := map[string][2]string{
		"SNI":               {got.SNI, "sni.example.com"},
		"Flow":              {got.Flow, "xtls-rprx-vision"},
		"RealityPublicKey":  {got.RealityPublicKey, "synthetic-public-key"},
		"RealityShortID":    {got.RealityShortID, "01ab"},
		"ClientFingerprint": {got.ClientFingerprint, "chrome"},
	}
	for field, pair := range preserved {
		if pair[0] != pair[1] {
			t.Errorf("%s = %q, want preserved %q", field, pair[0], pair[1])
		}
	}
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
