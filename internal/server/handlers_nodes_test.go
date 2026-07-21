package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

