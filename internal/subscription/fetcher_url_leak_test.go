package subscription

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

// 安全回归:机场订阅 URL 内含 bearer token,任何错误路径都不得把 URL/token
// 带进 error 字符串(错误会进日志、refresh_fetch_diags.error、refresh_runs.error
// 与 /api/refresh/runs/{id} 响应,落盘即泄密)。

const leakFakeToken = "SECRETTOKEN123"

// roundTripperFunc 测试用 RoundTripper 适配器。
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestFetch_ErrorNeverLeaksSubscriptionURL(t *testing.T) {
	tokenURL := "http://127.0.0.1:1/subscribe?token=" + leakFakeToken

	cases := []struct {
		name string
		f    *Fetcher
		url  string
	}{
		{
			// 连接被拒:*url.Error.Error() 原生会拼 "Get \"<url>\": dial ...",
			// 修复后只允许出现 dial 原因。
			name: "dial failure",
			f:    NewFetcher(2 * time.Second),
			url:  tokenURL,
		},
		{
			// 注入 RoundTripper 错误:http.Client 一律包成 *url.Error 带出 URL。
			name: "roundtripper error",
			f: &Fetcher{client: &http.Client{
				Timeout:   2 * time.Second,
				Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("boom") }),
			}},
			url: tokenURL,
		},
		{
			// 配置 URL 本身畸形:host 含空格,url.Parse 报错会引用原始输入串。
			name: "malformed configured url",
			f:    NewFetcher(2 * time.Second),
			url:  "http://exa mple.com/subscribe?token=" + leakFakeToken,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, diag, err := tc.f.FetchWithDiagnostics("测试机场", tc.url)
			if err == nil {
				t.Fatal("expected error")
			}
			if diag == nil {
				t.Fatal("diag should be non-nil on error")
			}
			if strings.Contains(err.Error(), leakFakeToken) {
				t.Errorf("error leaks token: %q", err.Error())
			}
			if strings.Contains(err.Error(), tc.url) {
				t.Errorf("error leaks full url: %q", err.Error())
			}
		})
	}
}

func TestFetchContext_CancelErrorNeverLeaksSubscriptionURL(t *testing.T) {
	tokenURL := "http://127.0.0.1:1/subscribe?token=" + leakFakeToken
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消:client.Do 返回 *url.Error 包装 context.Canceled

	f := NewFetcher(5 * time.Second)
	_, _, err := f.FetchContext(ctx, "测试机场", tokenURL)
	if err == nil {
		t.Fatal("expected error on cancelled ctx")
	}
	if strings.Contains(err.Error(), leakFakeToken) {
		t.Errorf("error leaks token: %q", err.Error())
	}
	if strings.Contains(err.Error(), tokenURL) {
		t.Errorf("error leaks full url: %q", err.Error())
	}
}
