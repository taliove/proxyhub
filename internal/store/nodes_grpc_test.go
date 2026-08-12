package store

import (
	"testing"

	"github.com/taliove/proxyhub/internal/subscription"
)

// grpc 参数快照 roundtrip(ticket 1 / spec #72):节点池保存/读取后
// GrpcServiceName/GrpcAuthority 不丢,跨刷新保真;非 grpc 节点字段为空
// (零回归);旧行迁移后默认 ”无需 backfill(幂等)。fixture 全合成。
func TestNodePool_GrpcParamsRoundTrip(t *testing.T) {
	st := newTestStore(t)

	pool := []*subscription.Node{
		{Name: "Grpc01", Type: "vless", Server: "grpc01.example.com", Port: 443,
			UUID: "00000000-0000-0000-0000-000000000000", Network: "grpc", TLS: true,
			SNI:             "sni.example.com",
			GrpcServiceName: "grpcsvc01",
			GrpcAuthority:   "auth01.example.com",
			Region:          "HK", Source: "机场A"},
		{Name: "Grpc02", Type: "vmess", Server: "grpc02.example.com", Port: 8443,
			UUID: "00000000-0000-0000-0000-000000000000", Network: "grpc", TLS: false,
			GrpcServiceName: "grpcsvc02", // 无 authority 的形态也要保真
			Region:          "SG", Source: "机场A"},
		{Name: "WS01", Type: "vless", Server: "ws01.example.com", Port: 443,
			UUID: "00000000-0000-0000-0000-000000000000", Network: "ws", TLS: true,
			SNI: "wss.example.com", Region: "HK", Source: "机场A"},
	}
	if err := st.SaveNodePool(pool); err != nil {
		t.Fatalf("SaveNodePool() error = %v", err)
	}

	got, err := st.LoadNodePool()
	if err != nil {
		t.Fatalf("LoadNodePool() error = %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("LoadNodePool() len = %d, want 3", len(got))
	}

	g1 := got[0]
	if g1.GrpcServiceName != "grpcsvc01" {
		t.Errorf("grpc 节点 GrpcServiceName = %q, want grpcsvc01", g1.GrpcServiceName)
	}
	if g1.GrpcAuthority != "auth01.example.com" {
		t.Errorf("grpc 节点 GrpcAuthority = %q, want auth01.example.com", g1.GrpcAuthority)
	}

	g2 := got[1]
	if g2.GrpcServiceName != "grpcsvc02" {
		t.Errorf("grpc 节点(无 authority)GrpcServiceName = %q, want grpcsvc02", g2.GrpcServiceName)
	}
	if g2.GrpcAuthority != "" {
		t.Errorf("grpc 节点(无 authority)GrpcAuthority = %q, want 空", g2.GrpcAuthority)
	}

	w := got[2]
	if w.GrpcServiceName != "" || w.GrpcAuthority != "" {
		t.Errorf("非 grpc 节点 grpc 字段应为空, got serviceName=%q authority=%q",
			w.GrpcServiceName, w.GrpcAuthority)
	}

	// 再次保存(下一轮刷新)后字段仍完整:upsert 路径不丢参数
	if err := st.SaveNodePool(pool); err != nil {
		t.Fatalf("SaveNodePool() 第二次 error = %v", err)
	}
	got2, err := st.LoadNodePool()
	if err != nil {
		t.Fatalf("LoadNodePool() 第二次 error = %v", err)
	}
	if got2[0].GrpcServiceName != g1.GrpcServiceName || got2[0].GrpcAuthority != g1.GrpcAuthority {
		t.Errorf("二次保存后 grpc 参数丢失: serviceName=%q authority=%q",
			got2[0].GrpcServiceName, got2[0].GrpcAuthority)
	}

	// 按用户分片读取(LoadNodePoolByUser)同样带出 grpc 字段
	byUser, err := st.LoadNodePoolByUser(got[0].UserID)
	if err != nil {
		t.Fatalf("LoadNodePoolByUser() error = %v", err)
	}
	if len(byUser) == 0 {
		t.Fatal("LoadNodePoolByUser() 返回空, want 至少含本池节点")
	}
	if byUser[0].GrpcServiceName != "grpcsvc01" || byUser[0].GrpcAuthority != "auth01.example.com" {
		t.Errorf("LoadNodePoolByUser grpc 参数 = %q/%q, want grpcsvc01/auth01.example.com",
			byUser[0].GrpcServiceName, byUser[0].GrpcAuthority)
	}
}

// 自建节点 grpc authority roundtrip(spec #72 实现决策:SelfHostedNode 同步补
// GrpcAuthority,serviceName 已有):创建/更新/读取后两字段不丢,ToNode 注入
// 节点模型;无 authority 的自建节点字段为空(零回归)。
func TestSelfHostedNode_GrpcAuthorityRoundTrip(t *testing.T) {
	st := newTestStore(t)

	n := &SelfHostedNode{
		Name: "自建Grpc", Protocol: "vless", Server: "self01.example.com", Port: 443,
		UUID: "00000000-0000-0000-0000-000000000000", Network: "grpc", TLS: true,
		GrpcServiceName: "selfsvc01", GrpcAuthority: "selfauth.example.com",
		Enabled: true,
	}
	if err := st.CreateSelfHostedNode(n); err != nil {
		t.Fatalf("CreateSelfHostedNode() error = %v", err)
	}

	all, err := st.ListAllSelfHostedNodes()
	if err != nil {
		t.Fatalf("ListAllSelfHostedNodes() error = %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("len = %d, want 1", len(all))
	}
	got := all[0]
	if got.GrpcServiceName != "selfsvc01" || got.GrpcAuthority != "selfauth.example.com" {
		t.Errorf("读取后 grpc 字段 = %q/%q, want selfsvc01/selfauth.example.com",
			got.GrpcServiceName, got.GrpcAuthority)
	}

	// ToNode 注入聚合模型:两字段必须带到 subscription.Node
	sn := got.ToNode()
	if sn.GrpcServiceName != "selfsvc01" || sn.GrpcAuthority != "selfauth.example.com" {
		t.Errorf("ToNode grpc 字段 = %q/%q, want selfsvc01/selfauth.example.com",
			sn.GrpcServiceName, sn.GrpcAuthority)
	}

	// 更新路径:service_name 与 authority 都归编辑面(gRPC Host 已进自建表单,2026-08),
	// 改写即生效;不再沿用"authority 不在编辑面、更新时保留"的旧契约。
	updated := &SelfHostedNode{
		ID: got.ID, Name: "自建Grpc", Protocol: "vless", Server: "self01.example.com", Port: 443,
		UUID: "00000000-0000-0000-0000-000000000000", Network: "grpc", TLS: true,
		GrpcServiceName: "selfsvc02", GrpcAuthority: "selfauth2.example.com",
		Enabled: true,
	}
	if err := st.UpdateSelfHostedNode(updated); err != nil {
		t.Fatalf("UpdateSelfHostedNode() error = %v", err)
	}
	all, _ = st.ListAllSelfHostedNodes()
	if all[0].GrpcServiceName != "selfsvc02" {
		t.Errorf("更新后 grpc service_name = %q, want selfsvc02", all[0].GrpcServiceName)
	}
	if all[0].GrpcAuthority != "selfauth2.example.com" {
		t.Errorf("更新后 grpc authority = %q, want 编辑面改写值 selfauth2.example.com", all[0].GrpcAuthority)
	}

	// 零回归:非 grpc 自建节点两字段为空
	plain := &SelfHostedNode{
		Name: "自建SS", Protocol: "ss", Server: "self02.example.com", Port: 8388,
		Cipher: "aes-256-gcm", Password: "pw", Enabled: true,
	}
	if err := st.CreateSelfHostedNode(plain); err != nil {
		t.Fatalf("CreateSelfHostedNode(ss) error = %v", err)
	}
	all, _ = st.ListAllSelfHostedNodes()
	for _, node := range all {
		if node.Name == "自建SS" && (node.GrpcServiceName != "" || node.GrpcAuthority != "") {
			t.Errorf("非 grpc 自建节点 grpc 字段应为空, got %q/%q",
				node.GrpcServiceName, node.GrpcAuthority)
		}
	}
}
