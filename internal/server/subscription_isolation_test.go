package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

// 多用户订阅隔离(ticket 10):
// 两个用户(超管 owner + 普通用户 alice)各自拥有 endpoint 与自建节点,
// /sub/{path} 按 endpoint 属主反查节点池,只输出该用户可见的节点。

// isolationFixture 搭好双用户 + 双方节点的测试场景,返回 server/handler/两个用户 id。
func isolationFixture(t *testing.T) (*Server, *store.Store, int64, int64) {
	t.Helper()

	ownerNodes := []*subscription.Node{
		{Name: "owner机场节点", Type: "ss", Server: "owner-airport.example.com", Port: 8388,
			Cipher: "aes-256-gcm", Password: "p", Available: true, Source: "机场A", UserID: 1},
		{Name: "alice机场节点", Type: "ss", Server: "alice-airport.example.com", Port: 8388,
			Cipher: "aes-256-gcm", Password: "p", Available: true, Source: "机场A", UserID: 2},
		// 未归属节点(UserID=0):语义上等价"全局共享",任何用户订阅都可见(与真实
		// Aggregator.NodesForUser 的分片口径一致——未归属桶由超管认领,但 fake 池
		// 无法表达"超管分片",按不过滤处理)。
	}
	srv, st := newTestServer(t, ownerNodes)

	// 创建两个用户:owner(超管)+ alice(普通用户)。passHash 填合法 bcrypt 串,
	// 本测试不走登录路径,hash 内容无意义。
	const anyHash = "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"
	owner, err := st.CreateUser("owner", anyHash, store.RoleSuperAdmin, false)
	if err != nil {
		t.Fatalf("CreateUser owner: %v", err)
	}
	alice, err := st.CreateUser("alice", anyHash, store.RoleUser, false)
	if err != nil {
		t.Fatalf("CreateUser alice: %v", err)
	}

	// 节点 UserID 用真实 id 重打标(fixture 里写死的 1/2 只是占位)。
	for _, n := range ownerNodes {
		if n.Name == "owner机场节点" {
			n.UserID = owner.ID
		} else {
			n.UserID = alice.ID
		}
	}

	// 各自一个启用自建节点,名字不同。
	if err := st.CreateSelfHostedNodeForUser(owner.ID, &store.SelfHostedNode{
		Name: "owner自建", Protocol: "ss", Server: "owner-self.example.com", Port: 8388,
		Cipher: "aes-256-gcm", Password: "p", Enabled: true,
	}); err != nil {
		t.Fatalf("CreateSelfHostedNodeForUser owner: %v", err)
	}
	if err := st.CreateSelfHostedNodeForUser(alice.ID, &store.SelfHostedNode{
		Name: "alice自建", Protocol: "ss", Server: "alice-self.example.com", Port: 8388,
		Cipher: "aes-256-gcm", Password: "p", Enabled: true,
	}); err != nil {
		t.Fatalf("CreateSelfHostedNodeForUser alice: %v", err)
	}

	return srv, st, owner.ID, alice.ID
}

// fetchSubStatus 拉一次订阅,返回状态码与响应体(不 assert,调用方按需判定)。
// 与 nodes_test.go 的 fetchSub(直接 fatal 非 200)互补:本文件要验证 404 场景。
func fetchSubStatus(t *testing.T, h http.Handler, ep *store.Endpoint) (int, string) {
	t.Helper()
	req := httptest.NewRequest("GET", "/sub/"+ep.Path+"?token="+ep.Token+"&format=clash", nil)
	req.RemoteAddr = "1.2.3.4:5678"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w.Code, w.Body.String()
}

func TestSubscription_MultiUserIsolation(t *testing.T) {
	srv, st, ownerID, aliceID := isolationFixture(t)
	h := srv.Handler()

	ownerEp, err := st.CreateEndpointForUser(ownerID, "owner-ep")
	if err != nil {
		t.Fatalf("CreateEndpointForUser owner: %v", err)
	}
	aliceEp, err := st.CreateEndpointForUser(aliceID, "alice-ep")
	if err != nil {
		t.Fatalf("CreateEndpointForUser alice: %v", err)
	}

	// 拉 alice 的订阅:应只见 alice 的机场节点 + alice 自建;不见 owner 的任何节点。
	code, body := fetchSubStatus(t, h, aliceEp)
	if code != http.StatusOK {
		t.Fatalf("alice sub status = %d, want 200 (body: %s)", code, body)
	}
	for _, want := range []string{"alice机场节点", "alice自建"} {
		if !strings.Contains(body, want) {
			t.Errorf("alice sub missing %q\nbody: %s", want, body)
		}
	}
	for _, notWant := range []string{"owner机场节点", "owner自建", "owner-airport.example.com", "owner-self.example.com"} {
		if strings.Contains(body, notWant) {
			t.Errorf("alice sub leaked owner resource %q\nbody: %s", notWant, body)
		}
	}

	// 拉 owner 的订阅:同理,只见 owner 的资源。
	code, body = fetchSubStatus(t, h, ownerEp)
	if code != http.StatusOK {
		t.Fatalf("owner sub status = %d, want 200 (body: %s)", code, body)
	}
	for _, want := range []string{"owner机场节点", "owner自建"} {
		if !strings.Contains(body, want) {
			t.Errorf("owner sub missing %q\nbody: %s", want, body)
		}
	}
	for _, notWant := range []string{"alice机场节点", "alice自建", "alice-airport.example.com", "alice-self.example.com"} {
		if strings.Contains(body, notWant) {
			t.Errorf("owner sub leaked alice resource %q\nbody: %s", notWant, body)
		}
	}

	// 禁用 alice 的 endpoint 后,/sub 返回 404(沿用既有"禁用=不存在"语义,
	// 顺带验证禁用路径未被多用户改造破坏)。
	if err := st.SetEndpointEnabledForUser(aliceID, aliceEp.ID, false); err != nil {
		t.Fatalf("SetEndpointEnabledForUser: %v", err)
	}
	code, _ = fetchSubStatus(t, h, aliceEp)
	if code != http.StatusNotFound {
		t.Errorf("disabled endpoint status = %d, want 404", code)
	}
	// owner 的订阅不受影响。
	code, _ = fetchSubStatus(t, h, ownerEp)
	if code != http.StatusOK {
		t.Errorf("owner sub after alice disable status = %d, want 200", code)
	}
}

// TestSubscription_AirportNodeLeakGuard 防回归:即使 fake 池里同时塞了多用户机场节点,
// 订阅输出也不得跨用户泄露(断言 body 里不含他方 server 域名,域名比节点名更难撞车)。
func TestSubscription_AirportNodeLeakGuard(t *testing.T) {
	srv, st, ownerID, aliceID := isolationFixture(t)
	h := srv.Handler()

	aliceEp, err := st.CreateEndpointForUser(aliceID, "alice-ep")
	if err != nil {
		t.Fatalf("CreateEndpointForUser alice: %v", err)
	}
	code, body := fetchSubStatus(t, h, aliceEp)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if bytes.Contains([]byte(body), []byte("owner-airport.example.com")) {
		t.Errorf("alice sub contains owner airport server\nbody: %s", body)
	}
	_ = ownerID // 本用例只关心 alice 侧;owner 侧由上一个测试覆盖
}
