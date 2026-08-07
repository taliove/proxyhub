package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

// 订阅地址精选 node_picks 的 server 缝(spec #70 / issue #79):
// 过滤链最前插入精选候选集替换——非空时 池∩精选 先行,再流经既有
// 关键词/屏蔽/stale/可用性过滤;空精选零回归。fixture 全合成(example.com)。

// picksPool 三个可用机场节点(NodeKey = server:port)。
func picksPool() []*subscription.Node {
	return []*subscription.Node{
		{Name: "香港A", Type: "ss", Server: "a.example.com", Port: 8388, Cipher: "aes-256-gcm", Password: "p", Available: true, Region: "HK", Source: "机场甲"},
		{Name: "日本B", Type: "ss", Server: "b.example.com", Port: 8388, Cipher: "aes-256-gcm", Password: "p", Available: true, Region: "JP", Source: "机场甲"},
		{Name: "美国C", Type: "ss", Server: "c.example.com", Port: 8388, Cipher: "aes-256-gcm", Password: "p", Available: true, Region: "US", Source: "机场乙"},
	}
}

// setNodePicks 通过 store 直接写入端点精选(绕过 API,测试订阅链)。
func setNodePicks(t *testing.T, srv *Server, id int64, picks ...string) {
	t.Helper()
	raw, err := json.Marshal(picks)
	if err != nil {
		t.Fatalf("marshal picks: %v", err)
	}
	if err := srv.st.UpdateEndpointNodePicks(id, string(raw)); err != nil {
		t.Fatalf("UpdateEndpointNodePicks: %v", err)
	}
}

// putNodePicks 走完整路由 PUT /api/endpoints/{id}/node-picks。
func putNodePicks(t *testing.T, h http.Handler, cookie *http.Cookie, id int64, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("PUT", "/api/endpoints/"+strconv.FormatInt(id, 10)+"/node-picks", strings.NewReader(body))
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// TestNodePicks_SubOnlyPickedNodes 配精选后 /sub 只含精选节点;
// 后台预览走同一条链(ADR 0005 预览=下发不破坏)。
func TestNodePicks_SubOnlyPickedNodes(t *testing.T) {
	srv, st := newTestServer(t, picksPool())
	h := srv.Handler()
	cookie := authCookie(t, h)
	ep, _ := st.CreateEndpointForUser(1, "picked")
	setNodePicks(t, srv, ep.ID, "a.example.com:8388", "c.example.com:8388")

	out := fetchSub(t, h, ep)
	for _, want := range []string{"香港A", "美国C"} {
		if !strings.Contains(out, want) {
			t.Errorf("picked sub missing %q\nbody: %s", want, out)
		}
	}
	if strings.Contains(out, "日本B") {
		t.Errorf("unpicked 日本B must not be delivered\nbody: %s", out)
	}

	// 后台预览同源:count=2 且节点与 /sub 一致。
	req := httptest.NewRequest("GET", "/api/endpoints/"+strconv.FormatInt(ep.ID, 10)+"/preview?format=clash", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("preview status = %d (body %s)", w.Code, w.Body.String())
	}
	var resp struct {
		Count int `json:"count"`
		Nodes []struct {
			Name string `json:"name"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal preview: %v", err)
	}
	if resp.Count != 2 || len(resp.Nodes) != 2 {
		t.Fatalf("preview count = %d, nodes = %d; want 2,2", resp.Count, len(resp.Nodes))
	}
	got := map[string]bool{resp.Nodes[0].Name: true, resp.Nodes[1].Name: true}
	if !got["香港A"] || !got["美国C"] {
		t.Errorf("preview nodes = %v, want 香港A + 美国C", got)
	}
}

// TestNodePicks_BlockedAndStaleExcluded 精选集中的屏蔽/已下架(stale)节点不下发
// (显式排除与消亡优先于点名,spec #70 user story 4)。
func TestNodePicks_BlockedAndStaleExcluded(t *testing.T) {
	pool := picksPool()
	srv, st := newTestServer(t, pool)
	h := srv.Handler()
	authCookie(t, h)
	ep, _ := st.CreateEndpointForUser(1, "picked")
	setNodePicks(t, srv, ep.ID, "a.example.com:8388", "b.example.com:8388", "c.example.com:8388")

	// 屏蔽 b(按端点属主 userID=1);下架 c(stale)。
	if err := st.BlockNodeForUser(1, "b.example.com:8388"); err != nil {
		t.Fatalf("BlockNodeForUser: %v", err)
	}
	pool[2].Stale = true

	out := fetchSub(t, h, ep)
	if !strings.Contains(out, "香港A") {
		t.Errorf("picked+healthy 香港A must be delivered\nbody: %s", out)
	}
	if strings.Contains(out, "日本B") {
		t.Errorf("blocked 日本B must not be delivered even if picked\nbody: %s", out)
	}
	if strings.Contains(out, "美国C") {
		t.Errorf("stale 美国C must not be delivered even if picked\nbody: %s", out)
	}
}

// TestNodePicks_NodeKeyMemory 精选按 NodeKey 记忆(spec #70 user story 3):
// 机场改名(server:port 不变)仍命中;节点下架后自然失效,复活后自动恢复命中。
func TestNodePicks_NodeKeyMemory(t *testing.T) {
	pool := picksPool()
	srv, st := newTestServer(t, pool)
	h := srv.Handler()
	authCookie(t, h)
	ep, _ := st.CreateEndpointForUser(1, "picked")
	setNodePicks(t, srv, ep.ID, "a.example.com:8388")

	// 改名:NodeKey(server:port)不变,精选仍命中。
	pool[0].Name = "香港A-改名后"
	out := fetchSub(t, h, ep)
	if !strings.Contains(out, "香港A-改名后") {
		t.Errorf("renamed node must still hit its pick\nbody: %s", out)
	}

	// 下架(stale):自然失效;精选只剩它时链空 -> 503(与过滤链清空同语义)。
	pool[0].Stale = true
	code, _ := fetchSubStatus(t, h, ep)
	if code != http.StatusServiceUnavailable {
		t.Errorf("sub with all picks stale status = %d, want 503", code)
	}

	// 复活:自动恢复命中,无需重配精选。
	pool[0].Stale = false
	out = fetchSub(t, h, ep)
	if !strings.Contains(out, "香港A-改名后") {
		t.Errorf("revived node must auto-hit its pick again\nbody: %s", out)
	}
}

// TestNodePicks_EmptyZeroRegression 空精选(未配置/显式清空)行为零回归:全量下发。
func TestNodePicks_EmptyZeroRegression(t *testing.T) {
	srv, st := newTestServer(t, picksPool())
	h := srv.Handler()
	cookie := authCookie(t, h)
	ep, _ := st.CreateEndpointForUser(1, "all")

	// 未配置:全量。
	out := fetchSub(t, h, ep)
	for _, want := range []string{"香港A", "日本B", "美国C"} {
		if !strings.Contains(out, want) {
			t.Errorf("no-picks sub missing %q\nbody: %s", want, out)
		}
	}

	// 配过再清空(API 传空数组 -> 落库空串):回到全量。
	setNodePicks(t, srv, ep.ID, "a.example.com:8388")
	w := putNodePicks(t, h, cookie, ep.ID, `{"node_picks":[]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("clear picks status = %d (body %s)", w.Code, w.Body.String())
	}
	got, _ := st.GetEndpointByID(ep.ID)
	if got.NodePicks != "" {
		t.Errorf("NodePicks after clear = %q, want empty", got.NodePicks)
	}
	out = fetchSub(t, h, ep)
	for _, want := range []string{"香港A", "日本B", "美国C"} {
		if !strings.Contains(out, want) {
			t.Errorf("cleared-picks sub missing %q\nbody: %s", want, out)
		}
	}
}

// TestUpdateEndpointNodePicksAPI 写 API:合法数组落库并被 List 回显;
// 非法请求体 -> 400;不存在的端点 -> 404。
func TestUpdateEndpointNodePicksAPI(t *testing.T) {
	srv, st := newTestServer(t, picksPool())
	h := srv.Handler()
	cookie := authCookie(t, h)
	ep, _ := st.CreateEndpointForUser(1, "cfg")

	w := putNodePicks(t, h, cookie, ep.ID, `{"node_picks":["a.example.com:8388","c.example.com:8388"]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("update picks status = %d (body %s)", w.Code, w.Body.String())
	}
	got, _ := st.GetEndpointByID(ep.ID)
	if !strings.Contains(got.NodePicks, "a.example.com:8388") || !strings.Contains(got.NodePicks, "c.example.com:8388") {
		t.Errorf("stored node_picks = %q, want two keys", got.NodePicks)
	}

	// List 回显(读 API 带 node_picks)。
	req := httptest.NewRequest("GET", "/api/endpoints", nil)
	req.AddCookie(cookie)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list status = %d", w.Code)
	}
	var items []struct {
		ID        int64  `json:"id"`
		NodePicks string `json:"node_picks"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &items); err != nil {
		t.Fatalf("unmarshal list: %v", err)
	}
	if len(items) != 1 || !strings.Contains(items[0].NodePicks, "a.example.com:8388") {
		t.Errorf("list node_picks = %+v, want stored picks echoed", items)
	}

	// 非法请求体 -> 400(坏 JSON / 非数组)。
	for _, bad := range []string{"{bad", `{"node_picks":"not-an-array"}`} {
		if w := putNodePicks(t, h, cookie, ep.ID, bad); w.Code != http.StatusBadRequest {
			t.Errorf("invalid body %q status = %d, want 400", bad, w.Code)
		}
	}

	// 不存在的端点 -> 404。
	if w := putNodePicks(t, h, cookie, 999, `{"node_picks":[]}`); w.Code != http.StatusNotFound {
		t.Errorf("missing endpoint status = %d, want 404", w.Code)
	}
}

// TestNodePicks_MultiTenantIsolation 多租户(issue #79):精选读写按请求者属主隔离——
// alice 的精选只作用于自己的端点;写他人端点 404;他人同名端点行为不受影响。
func TestNodePicks_MultiTenantIsolation(t *testing.T) {
	srv, st := newTestServer(t, picksPool())
	h := srv.Handler()
	ownerCookie := authCookie(t, h) // owner = 超管,用户 id 1
	aliceCookie := loginAs(t, srv, h, "alice", "alice-strong-pass", store.RoleUser)

	owner, err := st.GetUserByUsername("owner")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	alice, err := st.GetUserByUsername("alice")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	ownerEp, err := st.CreateEndpointForUser(owner.ID, "same-name")
	if err != nil {
		t.Fatalf("create owner endpoint: %v", err)
	}
	aliceEp, err := st.CreateEndpointForUser(alice.ID, "same-name")
	if err != nil {
		t.Fatalf("create alice endpoint: %v", err)
	}

	// alice 给自己的端点配精选。
	w := putNodePicks(t, h, aliceCookie, aliceEp.ID, `{"node_picks":["a.example.com:8388"]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("alice update picks status = %d (body %s)", w.Code, w.Body.String())
	}

	// alice 的订阅只含精选节点。
	out := fetchSub(t, h, aliceEp)
	if !strings.Contains(out, "香港A") || strings.Contains(out, "日本B") || strings.Contains(out, "美国C") {
		t.Errorf("alice picked sub wrong\nbody: %s", out)
	}

	// owner 的同名端点不受影响(零回归)。
	out = fetchSub(t, h, ownerEp)
	for _, want := range []string{"香港A", "日本B", "美国C"} {
		if !strings.Contains(out, want) {
			t.Errorf("owner sub missing %q\nbody: %s", want, out)
		}
	}

	// alice 写 owner 的端点 -> 404(不暴露存在性)。
	w = putNodePicks(t, h, aliceCookie, ownerEp.ID, `{"node_picks":["b.example.com:8388"]}`)
	if w.Code != http.StatusNotFound {
		t.Errorf("alice write owner endpoint status = %d, want 404", w.Code)
	}
	ownerGot, _ := st.GetEndpointByID(ownerEp.ID)
	if ownerGot.NodePicks != "" {
		t.Errorf("owner NodePicks after foreign write = %q, want empty", ownerGot.NodePicks)
	}

	// owner 的列表也看不到 alice 的精选(按属主分片)。
	req := httptest.NewRequest("GET", "/api/endpoints", nil)
	req.AddCookie(ownerCookie)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	var items []struct {
		NodePicks string `json:"node_picks"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &items); err != nil {
		t.Fatalf("unmarshal owner list: %v", err)
	}
	for _, it := range items {
		if it.NodePicks != "" {
			t.Errorf("owner list leaked picks: %+v", items)
		}
	}
}

// TestNodePicks_InvalidStoredJSONDegrades 库中精选 JSON 损坏时降级为未配置
// (宁可全量,与过滤链读取失败的降级风格一致),不把订阅打挂。
// 边界校验使脏数据无法经 API 落库,故直接构造端点对象测解析降级。
func TestNodePicks_InvalidStoredJSONDegrades(t *testing.T) {
	srv, _ := newTestServer(t, picksPool())
	broken := &store.Endpoint{ID: 1, NodePicks: "{corrupt"}
	if picks := srv.endpointNodePicks(broken); picks != nil {
		t.Errorf("endpointNodePicks(corrupt) = %v, want nil (degrade to unconfigured)", picks)
	}
	empty := &store.Endpoint{ID: 1, NodePicks: ""}
	if picks := srv.endpointNodePicks(empty); picks != nil {
		t.Errorf("endpointNodePicks(empty) = %v, want nil", picks)
	}
	ok := &store.Endpoint{ID: 1, NodePicks: `["a.example.com:8388"]`}
	picks := srv.endpointNodePicks(ok)
	if len(picks) != 1 || picks[0] != "a.example.com:8388" {
		t.Errorf("endpointNodePicks(valid) = %v, want [a.example.com:8388]", picks)
	}
}
