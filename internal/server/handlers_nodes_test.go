package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

func TestHandleNodeShareURI(t *testing.T) {
	// Add a test node to the pool
	testNode := &subscription.Node{
		Name:   "Test Node",
		Type:   "vless",
		Server: "example.com",
		Port:   443,
		UUID:   "00000000-0000-0000-0000-000000000000",
		Region: "Test",
		Source: "test-source",
	}

	s, _ := newTestServer(t, []*subscription.Node{testNode})

	tests := []struct {
		name       string
		nodeKey    string
		wantStatus int
		wantErr    bool
	}{
		{
			name:       "valid node key",
			nodeKey:    testNode.NodeKey(),
			wantStatus: http.StatusOK,
			wantErr:    false,
		},
		{
			name:       "non-existent node key",
			nodeKey:    "nonexistent.example.com:999",
			wantStatus: http.StatusNotFound,
			wantErr:    true,
		},
		{
			name:       "empty node key",
			nodeKey:    "",
			wantStatus: http.StatusBadRequest,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := "/api/nodes/" + tt.nodeKey + "/share-uri"
			req := httptest.NewRequest("GET", path, nil)
			req.SetPathValue("nodeKey", tt.nodeKey)
			rec := httptest.NewRecorder()

			s.handleNodeShareURI(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}

			if !tt.wantErr {
				var res map[string]string
				if err := json.NewDecoder(rec.Body).Decode(&res); err != nil {
					t.Fatalf("decode response: %v", err)
				}
				if res["uri"] == "" {
					t.Error("expected non-empty uri in response")
				}
				// Verify it's a valid share link format
				if len(res["uri"]) < 10 || res["uri"][:6] != "vless:" {
					t.Errorf("invalid share URI format: %s", res["uri"])
				}
			}
		})
	}
}

// TestHandleNodeShareURI_ScopedToUserPool 验证 share-uri 按请求者用户空间隔离:
// 普通用户读不到其他用户池内节点的分享链接(含凭证,跨租户泄露红线),
// 命中自己池则正常返回;节点属他人时一律 404,不暴露存在性。
func TestHandleNodeShareURI_ScopedToUserPool(t *testing.T) {
	adminNode := &subscription.Node{
		Name:    "Admin Node",
		Type:    "vless",
		Server:  "example.com",
		Port:    443,
		UUID:    "00000000-0000-0000-0000-000000000000",
		Source:  "test-source",
		RawLink: "vless://00000000-0000-0000-0000-000000000000@example.com:443?type=tcp#admin",
		UserID:  1, // 属 admin(user 1)的池分片
	}
	s, _ := newTestServer(t, []*subscription.Node{adminNode})

	call := func(scope UserScope) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/api/nodes/"+adminNode.NodeKey()+"/share-uri", nil)
		req.SetPathValue("nodeKey", adminNode.NodeKey())
		req = req.WithContext(ContextWithUserScope(req.Context(), scope))
		rec := httptest.NewRecorder()
		s.handleNodeShareURI(rec, req)
		return rec
	}

	// 普通用户(user 2)请求 admin 池里的节点:404,不泄露
	if rec := call(UserScope{UserID: 2, Role: store.RoleUser}); rec.Code != http.StatusNotFound {
		t.Errorf("cross-user status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	// 属主本人:200 且回放到 RawLink
	rec := call(UserScope{UserID: 1, Role: store.RoleSuperAdmin})
	if rec.Code != http.StatusOK {
		t.Fatalf("owner status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), "vless://") {
		t.Errorf("owner response missing share uri: %s", rec.Body.String())
	}
}

// TestHandleNodeShareURI_PrefersRawLink 验证 share-uri 端点优先回放解析时保留的
// 原始分享 URI(RawLink),而非走生成器重造(ticket 56)。
func TestHandleNodeShareURI_PrefersRawLink(t *testing.T) {
	rawLink := "vless://00000000-0000-0000-0000-000000000000@example.com:443?type=tcp&security=tls&flow=xtls-rprx-vision#raw-original"
	node := &subscription.Node{
		Name:    "Raw Node",
		Type:    "vless",
		Server:  "example.com",
		Port:    443,
		UUID:    "00000000-0000-0000-0000-000000000000",
		Source:  "test-source",
		RawLink: rawLink,
	}
	s, _ := newTestServer(t, []*subscription.Node{node})

	req := httptest.NewRequest("GET", "/api/nodes/"+node.NodeKey()+"/share-uri", nil)
	req.SetPathValue("nodeKey", node.NodeKey())
	rec := httptest.NewRecorder()
	s.handleNodeShareURI(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var res map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&res); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if res["uri"] != rawLink {
		t.Errorf("uri = %q, want original RawLink %q", res["uri"], rawLink)
	}
}

// TestHandleNodeShareURI_RawLinkFragmentNormalized 验证 RawLink 回放时 fragment(备注)
// 中的 + 被规范为 %20:+ 是 query 表单编码约定,fragment 只做 percent-decode,
// Shadowrocket 等客户端会把 + 原样显示在备注里。除 fragment 外其余部分必须
// 逐字节保真(ticket 56)。
func TestHandleNodeShareURI_RawLinkFragmentNormalized(t *testing.T) {
	rawLink := "ss://YWVzLTI1Ni1nY206cGFzc3dvcmQ=@example.com:8080/?plugin=simple-obfs%3Bobfs%3Dhttp%3Bobfs-host%3Dobfs.example.com#HK+%E9%A6%99%E6%B8%AF+01"
	node := &subscription.Node{
		Name:    "HK 香港 01",
		Type:    "ss",
		Server:  "example.com",
		Port:    8080,
		Cipher:  "aes-256-gcm",
		Password: "password",
		Source:  "test-source",
		RawLink: rawLink,
	}
	s, _ := newTestServer(t, []*subscription.Node{node})

	req := httptest.NewRequest("GET", "/api/nodes/"+node.NodeKey()+"/share-uri", nil)
	req.SetPathValue("nodeKey", node.NodeKey())
	rec := httptest.NewRecorder()
	s.handleNodeShareURI(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var res map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&res); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	wantPrefix := "ss://YWVzLTI1Ni1nY206cGFzc3dvcmQ=@example.com:8080/?plugin=simple-obfs%3Bobfs%3Dhttp%3Bobfs-host%3Dobfs.example.com#"
	if !strings.HasPrefix(res["uri"], wantPrefix) {
		t.Errorf("uri body changed, want prefix %q, got %q", wantPrefix, res["uri"])
	}
	frag := res["uri"][len(wantPrefix):]
	if strings.Contains(frag, "+") {
		t.Errorf("fragment %q still contains +", frag)
	}
	if frag != "HK%20%E9%A6%99%E6%B8%AF%2001" {
		t.Errorf("fragment = %q, want HK%%20...%%2001", frag)
	}
}

// TestHandleNodeShareURI_FallsBackToGenerator 验证 RawLink 缺失时(如自建节点或
// 本字段引入前解析的节点)端点回退到生成器生成分享 URI(ticket 56)。
func TestHandleNodeShareURI_FallsBackToGenerator(t *testing.T) {
	node := &subscription.Node{
		Name:   "No Raw Node",
		Type:   "vless",
		Server: "example.com",
		Port:   443,
		UUID:   "00000000-0000-0000-0000-000000000000",
		Source: "test-source",
		// RawLink 故意留空
	}
	s, _ := newTestServer(t, []*subscription.Node{node})

	req := httptest.NewRequest("GET", "/api/nodes/"+node.NodeKey()+"/share-uri", nil)
	req.SetPathValue("nodeKey", node.NodeKey())
	rec := httptest.NewRecorder()
	s.handleNodeShareURI(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var res map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&res); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if res["uri"] == "" {
		t.Error("expected generator-produced uri, got empty")
	}
	if len(res["uri"]) < 6 || res["uri"][:6] != "vless:" {
		t.Errorf("expected vless share URI from generator, got %q", res["uri"])
	}
}

// TestHandleListNodes_OmitsRawLinkCredentials 验证 RawLink(含凭证)绝不出现在
// /nodes 视图响应中(ticket 56 安全红线)。
func TestHandleListNodes_OmitsRawLinkCredentials(t *testing.T) {
	secretUUID := "11111111-2222-3333-4444-555555555555"
	rawLink := "vless://" + secretUUID + "@example.com:443?type=tcp&security=tls#leaky"
	node := &subscription.Node{
		Name:    "Secret Node",
		Type:    "vless",
		Server:  "example.com",
		Port:    443,
		UUID:    secretUUID,
		Source:  "test-source",
		RawLink: rawLink,
	}
	s, _ := newTestServer(t, []*subscription.Node{node})

	req := httptest.NewRequest("GET", "/api/nodes", nil)
	rec := httptest.NewRecorder()
	s.handleListNodes(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	if strings.Contains(body, rawLink) {
		t.Error("/nodes response leaked RawLink")
	}
	if strings.Contains(body, secretUUID) {
		t.Error("/nodes response leaked node credential (UUID)")
	}
}

