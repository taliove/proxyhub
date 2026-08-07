package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

// 机场节点一键转自建(spec #70 / issue #81)的 server 级测试缝:
// POST /api/self-nodes/from-pool {node_key} 从请求者合并池查节点,
// 全量映射(含 reality/grpc)走既有创建路径。fixture 全合成
// (example.com + 全零 UUID + 合成 pbk)。

// fromPoolRealityNode 合成一个带完整 VLESS Reality 参数的池节点。
func fromPoolRealityNode(userID int64) *subscription.Node {
	return &subscription.Node{
		Name:              "机场Reality节点",
		Type:              "vless",
		Server:            "reality.example.com",
		Port:              443,
		UUID:              "00000000-0000-0000-0000-000000000000",
		Network:           "tcp",
		TLS:               true,
		SNI:               "sni.example.com",
		Flow:              "xtls-rprx-vision",
		RealityPublicKey:  "synthetic-public-key",
		RealityShortID:    "01ab",
		ClientFingerprint: "chrome",
		Region:            "HK",
		Source:            "合成机场",
		Available:         true,
		UserID:            userID,
	}
}

// fromPoolGrpcNode 合成一个 vless over grpc 池节点(serviceName/authority 齐全)。
func fromPoolGrpcNode(userID int64) *subscription.Node {
	return &subscription.Node{
		Name:            "机场Grpc节点",
		Type:            "vless",
		Server:          "grpc.example.com",
		Port:            8443,
		UUID:            "00000000-0000-0000-0000-000000000000",
		Network:         "grpc",
		TLS:             true,
		SNI:             "grpc-sni.example.com",
		GrpcServiceName: "synthetic-svc",
		GrpcAuthority:   "authority.example.com",
		Region:          "JP",
		Source:          "合成机场",
		Available:       true,
		UserID:          userID,
	}
}

// callFromPool 以指定用户身份调用 from-pool handler(绕过路由中间件,同
// TestHandleNodeShareURI_ScopedToUserPool 的 prior art)。
func callFromPool(t *testing.T, srv *Server, scope UserScope, nodeKey string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"node_key": nodeKey})
	req := httptest.NewRequest(http.MethodPost, "/api/self-nodes/from-pool", bytes.NewReader(body))
	req = req.WithContext(ContextWithUserScope(req.Context(), scope))
	w := httptest.NewRecorder()
	srv.handleCreateSelfNodeFromPool(w, req)
	return w
}

// convertedRow 断言转换结果恰好一行并返回之。
func convertedRow(t *testing.T, srv *Server, userID int64) *store.SelfHostedNode {
	t.Helper()
	rows, err := srv.st.ListAllSelfHostedNodesByUser(userID)
	if err != nil {
		t.Fatalf("list self hosted: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("self hosted rows = %d, want 1", len(rows))
	}
	return rows[0]
}

// reality 节点转换保真:ToNode 输出的 reality 参数(flow/pbk/sid/fp/sni)完整。
func TestFromPool_RealityNodeFidelity(t *testing.T) {
	pool := fromPoolRealityNode(1)
	srv, _ := newTestServer(t, []*subscription.Node{pool})
	scope := UserScope{UserID: 1, Role: store.RoleSuperAdmin}

	w := callFromPool(t, srv, scope, pool.NodeKey())
	if w.Code != http.StatusOK {
		t.Fatalf("from-pool status = %d, want 200, body = %s", w.Code, w.Body.String())
	}

	row := convertedRow(t, srv, 1)
	if row.Name != pool.Name || row.Protocol != "vless" || row.Server != pool.Server || row.Port != pool.Port {
		t.Errorf("基本字段不保真: %+v", row)
	}
	if row.UUID != pool.UUID {
		t.Errorf("uuid = %q, want %q", row.UUID, pool.UUID)
	}
	if !row.Enabled {
		t.Errorf("转换产物应默认启用")
	}
	if row.RegionCode != "HK" {
		t.Errorf("region_code = %q, want HK(沿用池节点聚合期解析结果)", row.RegionCode)
	}

	// ToNode 全量对照:reality 参数一个都不能丢(丢失 = 退化成明文 VLESS 必然握手失败)
	n := row.ToNode()
	checks := map[string][2]string{
		"sni":                {n.SNI, "sni.example.com"},
		"flow":               {n.Flow, "xtls-rprx-vision"},
		"reality_public_key": {n.RealityPublicKey, "synthetic-public-key"},
		"reality_short_id":   {n.RealityShortID, "01ab"},
		"client_fingerprint": {n.ClientFingerprint, "chrome"},
		"uuid":               {n.UUID, "00000000-0000-0000-0000-000000000000"},
		"server":             {n.Server, "reality.example.com"},
		"region":             {n.Region, "HK"},
	}
	for field, pair := range checks {
		if pair[0] != pair[1] {
			t.Errorf("ToNode().%s = %q, want %q", field, pair[0], pair[1])
		}
	}
	if !n.TLS || n.Type != "vless" {
		t.Errorf("ToNode() tls=%v type=%q, want tls=true type=vless", n.TLS, n.Type)
	}
	if n.Source != subscription.SourceSelfHosted {
		t.Errorf("ToNode().Source = %q, want self-hosted 标记", n.Source)
	}
}

// grpc 节点转换保真:serviceName/authority 完整带过来。
func TestFromPool_GrpcNodeFidelity(t *testing.T) {
	pool := fromPoolGrpcNode(1)
	srv, _ := newTestServer(t, []*subscription.Node{pool})
	scope := UserScope{UserID: 1, Role: store.RoleSuperAdmin}

	w := callFromPool(t, srv, scope, pool.NodeKey())
	if w.Code != http.StatusOK {
		t.Fatalf("from-pool status = %d, want 200, body = %s", w.Code, w.Body.String())
	}

	n := convertedRow(t, srv, 1).ToNode()
	if n.Network != "grpc" {
		t.Errorf("network = %q, want grpc", n.Network)
	}
	if n.GrpcServiceName != "synthetic-svc" {
		t.Errorf("grpc_service_name = %q, want synthetic-svc", n.GrpcServiceName)
	}
	if n.GrpcAuthority != "authority.example.com" {
		t.Errorf("grpc_authority = %q, want authority.example.com", n.GrpcAuthority)
	}
	if n.SNI != "grpc-sni.example.com" {
		t.Errorf("sni = %q, want grpc-sni.example.com", n.SNI)
	}
}

// 重复转换同一节点 → 409(复用 023 身份唯一约束与既有消息)。
func TestFromPool_DuplicateConversion409(t *testing.T) {
	pool := fromPoolRealityNode(1)
	srv, _ := newTestServer(t, []*subscription.Node{pool})
	scope := UserScope{UserID: 1, Role: store.RoleSuperAdmin}

	if w := callFromPool(t, srv, scope, pool.NodeKey()); w.Code != http.StatusOK {
		t.Fatalf("首次转换 status = %d, want 200, body = %s", w.Code, w.Body.String())
	}
	w := callFromPool(t, srv, scope, pool.NodeKey())
	if w.Code != http.StatusConflict {
		t.Fatalf("重复转换 status = %d, want 409, body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "已存在") {
		t.Errorf("409 响应应说明重复原因, body = %s", w.Body.String())
	}
}

// 多租户:只能转换自己池里的节点;他人池节点与不存在的 key 一律 404,
// 不暴露存在性(凭证红线:连接参数不出池)。
func TestFromPool_NotInOwnPool404(t *testing.T) {
	pool := fromPoolRealityNode(1) // 属 user 1 的池分片
	srv, _ := newTestServer(t, []*subscription.Node{pool})
	other := UserScope{UserID: 2, Role: store.RoleUser}

	if w := callFromPool(t, srv, other, pool.NodeKey()); w.Code != http.StatusNotFound {
		t.Errorf("他人池节点 status = %d, want 404", w.Code)
	}
	// 属主本人可以转换
	if w := callFromPool(t, srv, UserScope{UserID: 1, Role: store.RoleSuperAdmin}, pool.NodeKey()); w.Code != http.StatusOK {
		t.Errorf("属主转换 status = %d, want 200, body = %s", w.Code, w.Body.String())
	}
	// 不存在的 key 同样 404
	if w := callFromPool(t, srv, UserScope{UserID: 1, Role: store.RoleSuperAdmin}, "missing.example.com:443"); w.Code != http.StatusNotFound {
		t.Errorf("未知 node_key status = %d, want 404", w.Code)
	}
}

// 机场下架后,转自建的节点仍在且字段独立存活(FailBack 语义):
// 转换在服务端完成、参数已落库,不依赖池快照生命周期。
func TestFromPool_SurvivesAirportRemoval(t *testing.T) {
	pool := fromPoolRealityNode(1)
	srv, _ := newTestServer(t, []*subscription.Node{pool})
	scope := UserScope{UserID: 1, Role: store.RoleSuperAdmin}

	if w := callFromPool(t, srv, scope, pool.NodeKey()); w.Code != http.StatusOK {
		t.Fatalf("from-pool status = %d, want 200, body = %s", w.Code, w.Body.String())
	}

	// 模拟机场下架:池中不再有任何机场节点
	srv.nodes.(*fakeNodes).nodes = nil

	// 库里的自建行原样存活,ToNode 字段不依赖池
	row := convertedRow(t, srv, 1)
	n := row.ToNode()
	if n.RealityPublicKey != "synthetic-public-key" || n.Flow != "xtls-rprx-vision" ||
		n.RealityShortID != "01ab" || n.ClientFingerprint != "chrome" || n.SNI != "sni.example.com" {
		t.Errorf("下架后 ToNode reality 字段丢失: %+v", n)
	}

	// 且 serve-time 合并依然把该自建节点注入请求者池(常驻安全网)
	merged := srv.mergeSelfHosted(srv.nodes.NodesForUser(1), 1)
	if countSelfHosted(merged) != 1 {
		t.Fatalf("下架后合并池 self-hosted count = %d, want 1", countSelfHosted(merged))
	}
}

// 转换产物出现在请求者自己的合并池里(立即可见于节点管理列表)。
func TestFromPool_AppearsInMergedPool(t *testing.T) {
	pool := fromPoolRealityNode(1)
	srv, _ := newTestServer(t, []*subscription.Node{pool})
	scope := UserScope{UserID: 1, Role: store.RoleSuperAdmin}

	if w := callFromPool(t, srv, scope, pool.NodeKey()); w.Code != http.StatusOK {
		t.Fatalf("from-pool status = %d, want 200", w.Code)
	}
	merged := srv.mergeSelfHosted(srv.nodes.NodesForUser(1), 1)
	if countSelfHosted(merged) != 1 {
		t.Fatalf("merged self-hosted count = %d, want 1", countSelfHosted(merged))
	}
}
