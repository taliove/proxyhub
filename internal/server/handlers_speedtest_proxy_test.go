package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestHandleSpeedtestProxyTest_DirectMode 直连模式(node_key 空)应返回测速结果,
// 不报 400/404。由于真实访问 Cloudflare 在测试环境不可达,只验证 handler
// 能正确解析请求并进入测速流程(返回 200 或 500,而非 400 参数错误)。
// 不验证具体数值——那依赖外部网络,属集成测试范畴。
func TestHandleSpeedtestProxyTest_DirectMode(t *testing.T) {
	srv, _ := newTestServer(t, nil)

	body := bytes.NewReader([]byte(`{"mode":"latency"}`))
	req := httptest.NewRequest(http.MethodPost, "/api/speedtest/proxy-test", body)
	w := httptest.NewRecorder()
	srv.handleSpeedtestProxyTest(w, req)

	// 直连模式 latency 应返回 200(或因网络不可达 500),但不应是 400/404
	if w.Code == http.StatusBadRequest {
		t.Fatalf("status = 400, direct mode should not be bad request: %s", w.Body.String())
	}
	if w.Code == http.StatusNotFound {
		t.Fatalf("status = 404, direct mode should not be not found: %s", w.Body.String())
	}
	// 200 或 500 都可接受(取决于测试环境能否访问 speed.cloudflare.com)
	if w.Code == http.StatusOK {
		var res map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		// latency 模式应至少有 idle_latency_ms 字段
		if _, ok := res["idle_latency_ms"]; !ok {
			t.Errorf("response missing idle_latency_ms: %v", res)
		}
	}
}

// TestHandleSpeedtestProxyTest_InvalidMode 非法 mode 应返回 400
func TestHandleSpeedtestProxyTest_InvalidMode(t *testing.T) {
	srv, _ := newTestServer(t, nil)

	body := bytes.NewReader([]byte(`{"mode":"bogus"}`))
	req := httptest.NewRequest(http.MethodPost, "/api/speedtest/proxy-test", body)
	w := httptest.NewRecorder()
	srv.handleSpeedtestProxyTest(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for invalid mode, body = %s", w.Code, w.Body.String())
	}
}

// TestHandleSpeedtestProxyTest_NodeNotFound 给了不存在的 node_key 应返回 404
func TestHandleSpeedtestProxyTest_NodeNotFound(t *testing.T) {
	srv, _ := newTestServer(t, nil)

	body := bytes.NewReader([]byte(`{"node_key":"nonexistent.example.com:9999","mode":"latency"}`))
	req := httptest.NewRequest(http.MethodPost, "/api/speedtest/proxy-test", body)
	w := httptest.NewRecorder()
	srv.handleSpeedtestProxyTest(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for missing node, body = %s", w.Code, w.Body.String())
	}
}

// TestHandleSpeedtestProxyTest_InvalidJSON 非法 JSON 应返回 400
func TestHandleSpeedtestProxyTest_InvalidJSON(t *testing.T) {
	srv, _ := newTestServer(t, nil)

	body := bytes.NewReader([]byte(`{not json`))
	req := httptest.NewRequest(http.MethodPost, "/api/speedtest/proxy-test", body)
	w := httptest.NewRecorder()
	srv.handleSpeedtestProxyTest(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for invalid json, body = %s", w.Code, w.Body.String())
	}
}
