package detection

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/taliove/proxyhub/internal/subscription"
)

// 最小化脱敏 fixture。

// 正常落地页:含 disneyplus.com 落地标记与 region 字段,不含 unavailable 标记。
const disneyBodyAvailableUS = `<!doctype html><html><head><title>Disney+ | Stream Now</title>
<meta property="og:url" content="https://www.disneyplus.com/"></head>
<body>Sign up for Disney+. <script>{"region":"US"}</script></body></html>`

// 正常落地页,地区解析不到:留空 region。
const disneyBodyAvailableNoRegion = `<html><head><title>Disney+</title></head>
<body>Welcome to disneyplus.com, start streaming today.</body></html>`

// 区域限制:命中"not available in your region"跳转。
const disneyBodyUnavailable = `<html><head><title>Disney+</title></head>
<body>Disney+ is not available in your region.</body></html>`

// 区域限制:命中 /unavailable 路径跳转。
const disneyBodyUnavailablePath = `<html><head><meta content="https://www.disneyplus.com/unavailable"></head>
<body>redirecting</body></html>`

// 页面改版/被拦:无任何已知标记(保守路径,应报错)。
const disneyBodyChanged = `<html><head><title>Access Denied</title></head><body>Request blocked.</body></html>`

func TestParseDisneyPlus(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		wantLevel  string
		wantRegion string
		wantErr    bool
	}{
		{"available US", http.StatusOK, disneyBodyAvailableUS, LevelFull, "US", false},
		{"available region unparsable", http.StatusOK, disneyBodyAvailableNoRegion, LevelFull, "", false},
		{"unavailable region marker", http.StatusOK, disneyBodyUnavailable, LevelBlocked, "", false},
		{"unavailable path redirect", http.StatusOK, disneyBodyUnavailablePath, LevelBlocked, "", false},
		{"page changed -> error", http.StatusOK, disneyBodyChanged, "", "", true},
		{"no markers non-200 -> error", http.StatusForbidden, disneyBodyChanged, "", "", true},
		// 状态门:非 200 即便命中落地标记也不得判可用(防软封锁/限流页误判)。
		{"landing marker but non-200 -> error", http.StatusForbidden, disneyBodyAvailableUS, "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			level, region, err := parseDisneyPlus(tt.status, tt.body)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got level=%q region=%q", level, region)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if level != tt.wantLevel {
				t.Errorf("level = %q, want %q", level, tt.wantLevel)
			}
			if region != tt.wantRegion {
				t.Errorf("region = %q, want %q", region, tt.wantRegion)
			}
		})
	}
}

// TestDisneyPlusRegistered Disney+ checker 已在 init 注册到专用表。
func TestDisneyPlusRegistered(t *testing.T) {
	if _, ok := unlockCheckers[KindDisneyPlus]; !ok {
		t.Fatal("KindDisneyPlus checker not registered")
	}
}

// disneyStubRoundTripper 忽略 URL,按预设返回状态/正文/错误,隔离真实网络。
// 刻意以 disney 前缀独占:并行开发下 youtube/netflix/AI 各自持有同名前缀助手,规避符号冲突。
type disneyStubRoundTripper struct {
	status int
	body   string
	err    error
}

func (s disneyStubRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &http.Response{
		StatusCode: s.status,
		Body:       io.NopCloser(strings.NewReader(s.body)),
		Header:     make(http.Header),
	}, nil
}

func disneyStubClient(status int, body string, err error) *http.Client {
	return &http.Client{Transport: disneyStubRoundTripper{status: status, body: body, err: err}}
}

func disneyTestNode() *subscription.Node {
	return &subscription.Node{Server: "example.com", Port: 443, UUID: "00000000-0000-0000-0000-000000000000"}
}

func TestCheckDisneyPlus(t *testing.T) {
	target := Target{Name: "Disney+", Kind: KindDisneyPlus}
	node := disneyTestNode()

	t.Run("available maps to full", func(t *testing.T) {
		res := checkDisneyPlus(context.Background(), disneyStubClient(200, disneyBodyAvailableUS, nil), node, target)
		if !res.Available || res.Level != LevelFull || res.Region != "US" {
			t.Fatalf("got %+v", res)
		}
		if res.NodeKey != node.NodeKey() || res.TargetName != target.Name {
			t.Errorf("identity fields not set: %+v", res)
		}
	})

	t.Run("unavailable maps to blocked", func(t *testing.T) {
		res := checkDisneyPlus(context.Background(), disneyStubClient(200, disneyBodyUnavailable, nil), node, target)
		if res.Available || res.Level != LevelBlocked {
			t.Fatalf("got %+v", res)
		}
	})

	t.Run("page changed -> error not available", func(t *testing.T) {
		res := checkDisneyPlus(context.Background(), disneyStubClient(200, disneyBodyChanged, nil), node, target)
		if res.Available || res.Level != "" || res.Error == "" {
			t.Fatalf("expected conservative error result, got %+v", res)
		}
	})

	t.Run("network error", func(t *testing.T) {
		res := checkDisneyPlus(context.Background(), disneyStubClient(0, "", errors.New("dial refused")), node, target)
		if res.Available || res.Error == "" {
			t.Fatalf("expected network error result, got %+v", res)
		}
	})
}
