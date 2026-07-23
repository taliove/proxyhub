package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestHandleSpeedtestProxyTest_DirectMode 直连模式(node_key 空)应进入测速流程,
// 推送 SSE 帧(非 400/404)。真实访问 Cloudflare 在 CI 可能不可达,只验证 handler
// 正确解析 query 并进入 SSE 流(返回 200 且 body 含 "data:" 帧前缀,或 500 错误帧)。
func TestHandleSpeedtestProxyTest_DirectMode(t *testing.T) {
	srv, _ := newTestServer(t, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/speedtest/proxy-test/stream?mode=latency", nil)
	w := httptest.NewRecorder()
	srv.handleSpeedtestProxyTest(w, req)

	if w.Code == http.StatusBadRequest {
		t.Fatalf("status = 400, direct mode should not be bad request: %s", w.Body.String())
	}
	if w.Code == http.StatusNotFound {
		t.Fatalf("status = 404, direct mode should not be not found: %s", w.Body.String())
	}
	// 200:SSE 流,body 应含 "data:" 帧前缀(latency 或 error 帧)
	if w.Code == http.StatusOK {
		if !strings.Contains(w.Body.String(), "data:") {
			t.Errorf("response should contain SSE data: frames, got: %s", w.Body.String())
		}
	}
}

// TestHandleSpeedtestProxyTest_InvalidMode 非法 mode 应返回 400
func TestHandleSpeedtestProxyTest_InvalidMode(t *testing.T) {
	srv, _ := newTestServer(t, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/speedtest/proxy-test/stream?mode=bogus", nil)
	w := httptest.NewRecorder()
	srv.handleSpeedtestProxyTest(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for invalid mode, body = %s", w.Code, w.Body.String())
	}
}

// TestHandleSpeedtestProxyTest_NodeNotFound 给了不存在的 node_key 应返回 404
func TestHandleSpeedtestProxyTest_NodeNotFound(t *testing.T) {
	srv, _ := newTestServer(t, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/speedtest/proxy-test/stream?node_key=nonexistent.example.com:9999&mode=latency", nil)
	w := httptest.NewRecorder()
	srv.handleSpeedtestProxyTest(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for missing node, body = %s", w.Code, w.Body.String())
	}
}

// TestHandleSpeedtestProxyTest_InvalidSelfNodeID 非法 self_node_id 应返回 400
func TestHandleSpeedtestProxyTest_InvalidSelfNodeID(t *testing.T) {
	srv, _ := newTestServer(t, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/speedtest/proxy-test/stream?self_node_id=abc", nil)
	w := httptest.NewRecorder()
	srv.handleSpeedtestProxyTest(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for invalid self_node_id, body = %s", w.Code, w.Body.String())
	}
}
