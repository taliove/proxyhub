package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/taliove/proxyhub/internal/subscription"
)

// TestSpeedtestProxy_ClientErrorSanitized 代理客户端构造失败时,底层错误源自含凭证的
// Clash 配置(map 里有 uuid/password)与 mihomo adapter.ParseProxy,响应必须只回
// 固定泛化文案,绝不回显底层错误文本(防凭证随依赖升级外泄)。
// 注入方式:节点 Type 置为 "SECRET-MARKER",generator.ClashProxy 报
// "unsupported type: SECRET-MARKER",经 newProxyAdapter/ProxyHTTPClient 逐层包装上传。
func TestSpeedtestProxy_ClientErrorSanitized(t *testing.T) {
	markerNode := &subscription.Node{
		Name: "marker", Type: "SECRET-MARKER", Server: "example.com", Port: 1234,
	}
	srv, _ := newTestServer(t, []*subscription.Node{markerNode})
	h := srv.Handler()
	cookie := authCookie(t, h)

	endpoints := []struct {
		method string
		path   string
	}{
		{"GET", "/api/speedtest/proxy-download/stream"},
		{"GET", "/api/speedtest/proxy-latency"},
		{"POST", "/api/speedtest/proxy-upload/stream"},
	}
	for _, ep := range endpoints {
		path := ep.path + "?node_key=" + markerNode.NodeKey()
		var w = doReq(t, h, ep.method, path, strings.NewReader("x"), cookie)
		if w.Code != http.StatusInternalServerError {
			t.Errorf("%s %s: status = %d, want 500", ep.method, ep.path, w.Code)
		}
		body := strings.TrimSpace(w.Body.String())
		if body != "proxy client unavailable" {
			t.Errorf("%s %s: body = %q, want fixed generic %q", ep.method, ep.path, body, "proxy client unavailable")
		}
		if strings.Contains(w.Body.String(), "SECRET-MARKER") {
			t.Errorf("%s %s: body leaks underlying error detail: %q", ep.method, ep.path, w.Body.String())
		}
	}
}

// TestSpeedtestProxy_UpstreamErrorSanitized 上游请求失败(dial 被拒)时,响应同样只回
// 固定泛化文案,不回显 net/mihomo 的 dial 错误细节(其中含节点地址等内部信息)。
func TestSpeedtestProxy_UpstreamErrorSanitized(t *testing.T) {
	deadNode := &subscription.Node{
		Name: "dead", Type: "ss", Server: "127.0.0.1", Port: 1,
		Cipher: "aes-256-gcm", Password: "p",
	}
	srv, _ := newTestServer(t, []*subscription.Node{deadNode})
	h := srv.Handler()
	cookie := authCookie(t, h)

	w := doReq(t, h, "GET", "/api/speedtest/proxy-download/stream?node_key="+deadNode.NodeKey(), nil, cookie)
	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 (body: %s)", w.Code, w.Body.String())
	}
	body := strings.TrimSpace(w.Body.String())
	if body != "upstream request failed" {
		t.Errorf("body = %q, want fixed generic %q", body, "upstream request failed")
	}
	if strings.Contains(w.Body.String(), "127.0.0.1") {
		t.Errorf("body leaks dial detail: %q", w.Body.String())
	}
}
