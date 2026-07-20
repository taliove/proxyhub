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

// 最小化脱敏 fixture:仅保留判定所需关键字,凭证一律 example.com / 全零。

// 可用:含 "YouTube Premium" 品牌短语与 INNERTUBE_CONTEXT_GL 地区,不含"不可用"标记。
const ytBodyAvailableUS = `<!doctype html><html><head><title>YouTube Premium</title></head>
<body>Get YouTube Premium: ad-free, background play.
<script>var ytcfg={"INNERTUBE_CONTEXT_GL":"US","countryCode":"US"};</script></body></html>`

// 可用:地区用小写 gl,验证值大小写归一化。
const ytBodyAvailableHKLower = `<html><body>YouTube Premium available
<script>{"gl":"hk"}</script></body></html>`

// 可用但地区解析不到:留空 region。
const ytBodyAvailableNoRegion = `<html><body>YouTube Premium is here, enjoy ad-free.</body></html>`

// 不可用:命中"Premium is not available in your country"。
const ytBodyBlocked = `<html><body>Sorry, YouTube Premium is not available in your country.
<script>{"countryCode":"CN"}</script></body></html>`

// 中国大陆:重定向到 google.cn。
const ytBodyCN = `<html><head><meta content="https://www.google.cn/"></head><body>redirect</body></html>`

// 页面改版:既无可用标记也无不可用标记(保守路径,应报错)。
const ytBodyChanged = `<html><head><title>Something Else</title></head><body>Please verify you are human.</body></html>`

func TestParseYouTubePremium(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		wantLevel  string
		wantRegion string
		wantErr    bool
	}{
		{"available US", http.StatusOK, ytBodyAvailableUS, LevelFull, "US", false},
		{"available HK lowercase gl", http.StatusOK, ytBodyAvailableHKLower, LevelFull, "HK", false},
		{"available region unparsable", http.StatusOK, ytBodyAvailableNoRegion, LevelFull, "", false},
		{"blocked not available", http.StatusOK, ytBodyBlocked, LevelBlocked, "CN", false},
		{"blocked cn redirect", http.StatusOK, ytBodyCN, LevelBlocked, "CN", false},
		{"page changed -> error", http.StatusOK, ytBodyChanged, "", "", true},
		{"no markers non-200 -> error", http.StatusForbidden, ytBodyChanged, "", "", true},
		// 状态门:非 200 即便命中可用标记也不得判可用(防软封锁/限流页误判)。
		{"available marker but non-200 -> error", http.StatusForbidden, ytBodyAvailableUS, "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			level, region, err := parseYouTubePremium(tt.status, tt.body)
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

// TestYouTubePremiumRegistered YouTube Premium checker 已在 init 注册到专用表。
func TestYouTubePremiumRegistered(t *testing.T) {
	if _, ok := unlockCheckers[KindYouTubePremium]; !ok {
		t.Fatal("KindYouTubePremium checker not registered")
	}
}

// ytStubRoundTripper 忽略 URL,按预设返回状态/正文/错误,隔离真实网络。
// 刻意以 yt 前缀独占:并行开发下 disney/netflix/AI 各自持有同名前缀助手,规避符号冲突。
type ytStubRoundTripper struct {
	status int
	body   string
	err    error
}

func (s ytStubRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &http.Response{
		StatusCode: s.status,
		Body:       io.NopCloser(strings.NewReader(s.body)),
		Header:     make(http.Header),
	}, nil
}

func ytStubClient(status int, body string, err error) *http.Client {
	return &http.Client{Transport: ytStubRoundTripper{status: status, body: body, err: err}}
}

func ytTestNode() *subscription.Node {
	return &subscription.Node{Server: "example.com", Port: 443, UUID: "00000000-0000-0000-0000-000000000000"}
}

func TestCheckYouTubePremium(t *testing.T) {
	target := Target{Name: "YouTube Premium", Kind: KindYouTubePremium}
	node := ytTestNode()

	t.Run("available maps to full", func(t *testing.T) {
		res := checkYouTubePremium(context.Background(), ytStubClient(200, ytBodyAvailableUS, nil), node, target)
		if !res.Available || res.Level != LevelFull || res.Region != "US" {
			t.Fatalf("got %+v", res)
		}
		if res.Error != "" {
			t.Errorf("unexpected error: %q", res.Error)
		}
		if res.NodeKey != node.NodeKey() || res.TargetName != target.Name {
			t.Errorf("identity fields not set: %+v", res)
		}
	})

	t.Run("blocked maps to blocked", func(t *testing.T) {
		res := checkYouTubePremium(context.Background(), ytStubClient(200, ytBodyBlocked, nil), node, target)
		if res.Available || res.Level != LevelBlocked {
			t.Fatalf("got %+v", res)
		}
	})

	t.Run("page changed -> error not available", func(t *testing.T) {
		res := checkYouTubePremium(context.Background(), ytStubClient(200, ytBodyChanged, nil), node, target)
		if res.Available || res.Level != "" || res.Error == "" {
			t.Fatalf("expected conservative error result, got %+v", res)
		}
	})

	t.Run("network error", func(t *testing.T) {
		res := checkYouTubePremium(context.Background(), ytStubClient(0, "", errors.New("dial refused")), node, target)
		if res.Available || res.Error == "" {
			t.Fatalf("expected network error result, got %+v", res)
		}
	})
}
