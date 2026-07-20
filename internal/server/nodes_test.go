package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

// nodeMgmtNodes 一组机场节点 + 一个自建节点，用于过滤链断言
func nodeMgmtNodes() []*subscription.Node {
	return []*subscription.Node{
		{Name: "香港优选", Type: "ss", Server: "1.1.1.1", Port: 8388,
			Cipher: "aes-256-gcm", Password: "p", Available: true, Source: "机场A"},
		{Name: "德国冷门", Type: "ss", Server: "2.2.2.2", Port: 8388,
			Cipher: "aes-256-gcm", Password: "p", Available: true, Source: "机场A"},
		{Name: "自建美国", Type: "trojan", Server: "3.3.3.3", Port: 443,
			Password: "p", TLS: true, Available: true, Source: subscription.SourceSelfHosted},
	}
}

func TestSelfNodeAPI_CRUDAndToggle(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	h := srv.Handler()
	cookie := authCookie(t, h)

	// 新增
	body, _ := json.Marshal(map[string]any{
		"name": "自建HK", "protocol": "ss", "server": "1.1.1.1", "port": 8388,
		"cipher": "aes-256-gcm", "password": "pw",
	})
	req := httptest.NewRequest("POST", "/api/self-nodes", bytes.NewReader(body))
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("create status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}

	// 列表应有 1 个且启用
	id := listSelfNodesFirstID(t, h, cookie, 1)

	// 编辑
	upd, _ := json.Marshal(map[string]any{
		"name": "自建JP", "protocol": "trojan", "server": "2.2.2.2", "port": 443, "password": "np", "tls": true,
	})
	req = httptest.NewRequest("PUT", "/api/self-nodes/"+strconv.FormatInt(id, 10), bytes.NewReader(upd))
	req.AddCookie(cookie)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}

	// 启停：禁用
	tg, _ := json.Marshal(map[string]bool{"enabled": false})
	req = httptest.NewRequest("POST", "/api/self-nodes/"+strconv.FormatInt(id, 10)+"/toggle", bytes.NewReader(tg))
	req.AddCookie(cookie)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("toggle status = %d, want 200", w.Code)
	}

	// 删除
	req = httptest.NewRequest("DELETE", "/api/self-nodes/"+strconv.FormatInt(id, 10), nil)
	req.AddCookie(cookie)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete status = %d, want 200", w.Code)
	}
	listSelfNodesFirstID(t, h, cookie, 0) // 应为空
}

func listSelfNodesFirstID(t *testing.T, h http.Handler, cookie *http.Cookie, wantLen int) int64 {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/self-nodes", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200", w.Code)
	}
	var resp struct {
		Nodes []struct {
			ID int64 `json:"id"`
		} `json:"nodes"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Nodes) != wantLen {
		t.Fatalf("self-nodes len = %d, want %d", len(resp.Nodes), wantLen)
	}
	if wantLen == 0 {
		return 0
	}
	return resp.Nodes[0].ID
}

func TestSelfNodeAPI_RejectsBadProtocol(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	h := srv.Handler()
	cookie := authCookie(t, h)

	body, _ := json.Marshal(map[string]any{"name": "x", "protocol": "wireguard", "server": "a", "port": 1})
	req := httptest.NewRequest("POST", "/api/self-nodes", bytes.NewReader(body))
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for unsupported protocol", w.Code)
	}
}

func TestNodeBlockAPI_HidesNodeFromSubscription(t *testing.T) {
	srv, st := newTestServer(t, nodeMgmtNodes())
	h := srv.Handler()
	// Seed DB with self-hosted node (DB-authoritative after mergeSelfHosted)
	if err := st.CreateSelfHostedNode(&store.SelfHostedNode{
		Name:     "自建美国",
		Protocol: "trojan",
		Server:   "3.3.3.3",
		Port:     443,
		Password: "p",
		TLS:      true,
		Enabled:  true,
	}); err != nil {
		t.Fatalf("CreateSelfHostedNode: %v", err)
	}
	cookie := authCookie(t, h)
	ep, _ := st.CreateEndpoint("dev")

	// 屏蔽香港节点（NodeKey = 1.1.1.1:8388）
	body, _ := json.Marshal(map[string]string{"node_key": "1.1.1.1:8388"})
	req := httptest.NewRequest("POST", "/api/nodes/block", bytes.NewReader(body))
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("block status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}

	// 订阅不应含香港优选，但德国冷门与自建仍在
	out := fetchSub(t, h, ep)
	if strings.Contains(out, "香港优选") {
		t.Error("blocked node 香港优选 leaked into /sub")
	}
	if !strings.Contains(out, "德国冷门") || !strings.Contains(out, "自建美国") {
		t.Error("non-blocked nodes missing from /sub")
	}

	// 取消屏蔽后恢复
	req = httptest.NewRequest("POST", "/api/nodes/unblock", bytes.NewReader(body))
	req.AddCookie(cookie)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("unblock status = %d, want 200", w.Code)
	}
	out = fetchSub(t, h, ep)
	if !strings.Contains(out, "香港优选") {
		t.Error("unblocked node 香港优选 not restored in /sub")
	}
}

func TestSubscription_WhitelistKeepsOnlyMatching(t *testing.T) {
	srv, st := newTestServer(t, nodeMgmtNodes())
	h := srv.Handler()
	// Seed DB with self-hosted node (DB-authoritative after mergeSelfHosted)
	if err := st.CreateSelfHostedNode(&store.SelfHostedNode{
		Name:     "自建美国",
		Protocol: "trojan",
		Server:   "3.3.3.3",
		Port:     443,
		Password: "p",
		TLS:      true,
		Enabled:  true,
	}); err != nil {
		t.Fatalf("CreateSelfHostedNode: %v", err)
	}
	st.SaveSystemSettings(map[string]string{"filter_whitelist": "香港"})
	ep, _ := st.CreateEndpoint("dev")

	out := fetchSub(t, h, ep)
	if !strings.Contains(out, "香港优选") {
		t.Error("whitelisted 香港优选 missing")
	}
	if strings.Contains(out, "德国冷门") {
		t.Error("non-whitelisted 德国冷门 should be filtered out")
	}
	// 自建节点豁免白名单，必须始终存在
	if !strings.Contains(out, "自建美国") {
		t.Error("self-hosted node must be exempt from whitelist")
	}
}

func TestSubscription_WhitelistThenBlacklistThenBlock(t *testing.T) {
	// 组合：白名单留"优选/冷门"两地区节点，黑名单排"冷门"，屏蔽排香港 → 只剩自建
	nodes := []*subscription.Node{
		{Name: "香港优选", Type: "ss", Server: "1.1.1.1", Port: 8388, Cipher: "aes-256-gcm", Password: "p", Available: true, Source: "机场A"},
		{Name: "日本冷门", Type: "ss", Server: "2.2.2.2", Port: 8388, Cipher: "aes-256-gcm", Password: "p", Available: true, Source: "机场A"},
		{Name: "自建", Type: "trojan", Server: "3.3.3.3", Port: 443, Password: "p", TLS: true, Available: true, Source: subscription.SourceSelfHosted},
	}
	srv, st := newTestServer(t, nodes)
	h := srv.Handler()
	// Seed DB with self-hosted node (DB-authoritative after mergeSelfHosted)
	if err := st.CreateSelfHostedNode(&store.SelfHostedNode{
		Name:     "自建",
		Protocol: "trojan",
		Server:   "3.3.3.3",
		Port:     443,
		Password: "p",
		TLS:      true,
		Enabled:  true,
	}); err != nil {
		t.Fatalf("CreateSelfHostedNode: %v", err)
	}
	st.SaveSystemSettings(map[string]string{
		"filter_whitelist": "优选,冷门", // 两个机场节点都命中白名单
		"filter_keywords":  "冷门",     // 黑名单排掉日本冷门
	})
	st.BlockNode("1.1.1.1:8388") // 屏蔽香港优选
	ep, _ := st.CreateEndpoint("dev")

	out := fetchSub(t, h, ep)
	if strings.Contains(out, "香港优选") {
		t.Error("香港优选 should be removed by block")
	}
	if strings.Contains(out, "日本冷门") {
		t.Error("日本冷门 should be removed by blacklist")
	}
	if !strings.Contains(out, "自建") {
		t.Error("self-hosted node must survive all filters")
	}
}

func fetchSub(t *testing.T, h http.Handler, ep *store.Endpoint) string {
	t.Helper()
	req := httptest.NewRequest("GET", "/sub/"+ep.Path+"?token="+ep.Token+"&format=clash", nil)
	req.RemoteAddr = "1.2.3.4:5678"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("/sub status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	return w.Body.String()
}

// TestPreviewMatchesSubscription 验证后台预览与 /sub 走同一条过滤链（所见即所得，ADR 0005）：
// 屏蔽+白名单同时生效下，预览的节点清单应与 /sub 实际渲染的节点一致。
func TestPreviewMatchesSubscription(t *testing.T) {
	srv, st := newTestServer(t, nodeMgmtNodes())
	h := srv.Handler()
	cookie := authCookie(t, h)
	st.SaveSystemSettings(map[string]string{"filter_whitelist": "香港"}) // 只留香港优选（+自建豁免）
	st.BlockNode("9.9.9.9:1")                                          // 无关屏蔽，不影响
	ep, _ := st.CreateEndpoint("dev")

	// /sub 输出
	sub := fetchSub(t, h, ep)

	// 预览输出
	req := httptest.NewRequest("GET", "/api/endpoints/"+strconv.FormatInt(ep.ID, 10)+"/preview?format=clash", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("preview status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	var resp struct {
		Nodes []struct {
			Name string `json:"name"`
		} `json:"nodes"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)

	// 预览列出的每个节点名都应出现在 /sub 渲染结果里；被白名单剔除的德国冷门两边都不该有
	for _, n := range resp.Nodes {
		if !strings.Contains(sub, n.Name) {
			t.Errorf("preview lists %q but /sub does not contain it (chains diverged)", n.Name)
		}
	}
	if strings.Contains(sub, "德国冷门") {
		t.Error("德国冷门 filtered by whitelist should be absent from /sub")
	}
	// 预览里也不该有德国冷门
	for _, n := range resp.Nodes {
		if n.Name == "德国冷门" {
			t.Error("德国冷门 filtered by whitelist should be absent from preview")
		}
	}
}
