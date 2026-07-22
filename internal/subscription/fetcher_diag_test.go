package subscription

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// diagSubscriptionServer 返回内容可定制的订阅服务器。
// content 为原始文本(非 base64),便于直接混合有效/无效行。
func diagSubscriptionServer(t *testing.T, status int, content string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		w.Write([]byte(base64.StdEncoding.EncodeToString([]byte(content))))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFetchWithDiagnostics_Success(t *testing.T) {
	content := strings.Join([]string{
		"trojan://pw@example.com:443#HK node1",
		"ss://YWVzLTEyOC1nY206cHc@example.com:8388#US node2",
		"not-a-valid-share-link",
		"also::broken",
	}, "\n")
	srv := diagSubscriptionServer(t, http.StatusOK, content)

	f := NewFetcher(5 * time.Second)
	sub, diag, err := f.FetchWithDiagnostics("测试机场", srv.URL)
	if err != nil {
		t.Fatalf("FetchWithDiagnostics() error = %v", err)
	}
	if len(sub.Nodes) != 2 {
		t.Errorf("nodes = %d, want 2", len(sub.Nodes))
	}
	if diag.HTTPStatus != http.StatusOK {
		t.Errorf("HTTPStatus = %d, want 200", diag.HTTPStatus)
	}
	if diag.NodeCount != 2 {
		t.Errorf("NodeCount = %d, want 2", diag.NodeCount)
	}
	if diag.ParseFailures != 2 {
		t.Errorf("ParseFailures = %d, want 2", diag.ParseFailures)
	}
	if diag.DurationMs < 0 {
		t.Errorf("DurationMs = %d, want >= 0", diag.DurationMs)
	}
}

func TestFetchWithDiagnostics_HTTPError(t *testing.T) {
	srv := diagSubscriptionServer(t, http.StatusServiceUnavailable, "")

	f := NewFetcher(5 * time.Second)
	sub, diag, err := f.FetchWithDiagnostics("测试机场", srv.URL)
	if err == nil {
		t.Fatal("FetchWithDiagnostics() should fail on non-200")
	}
	if sub != nil {
		t.Errorf("sub = %v, want nil", sub)
	}
	if diag == nil {
		t.Fatal("diag should be non-nil on error")
	}
	if diag.HTTPStatus != http.StatusServiceUnavailable {
		t.Errorf("HTTPStatus = %d, want 503", diag.HTTPStatus)
	}
}

func TestFetchWithDiagnostics_NetworkError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close() // 立即关闭,连接必失败

	f := NewFetcher(5 * time.Second)
	_, diag, err := f.FetchWithDiagnostics("测试机场", url)
	if err == nil {
		t.Fatal("FetchWithDiagnostics() should fail on unreachable server")
	}
	if diag == nil {
		t.Fatal("diag should be non-nil on network error")
	}
	if diag.HTTPStatus != 0 {
		t.Errorf("HTTPStatus = %d, want 0 on network error", diag.HTTPStatus)
	}
}

func TestFetchWithDiagnostics_NoValidNodes(t *testing.T) {
	srv := diagSubscriptionServer(t, http.StatusOK, "garbage-line-1\ngarbage-line-2")

	f := NewFetcher(5 * time.Second)
	_, diag, err := f.FetchWithDiagnostics("测试机场", srv.URL)
	if err == nil {
		t.Fatal("FetchWithDiagnostics() should fail when no valid nodes")
	}
	if diag.NodeCount != 0 {
		t.Errorf("NodeCount = %d, want 0", diag.NodeCount)
	}
	if diag.ParseFailures != 2 {
		t.Errorf("ParseFailures = %d, want 2", diag.ParseFailures)
	}
}

// Fetch 包装保持既有行为:丢弃诊断,只返回订阅与错误。
func TestFetch_StillWorks(t *testing.T) {
	srv := diagSubscriptionServer(t, http.StatusOK, "trojan://pw@example.com:443#HK node1")

	f := NewFetcher(5 * time.Second)
	sub, err := f.Fetch("测试机场", srv.URL)
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if len(sub.Nodes) != 1 || sub.Nodes[0].Server != "example.com" {
		t.Errorf("nodes = %+v, want 1 trojan node on example.com", sub.Nodes)
	}
}
