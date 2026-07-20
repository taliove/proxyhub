package detection

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/taliove/proxyhub/internal/subscription"
)

// TestNetflixChecker_Registered netflix checker 在 init 中注册到共享注册表。
func TestNetflixChecker_Registered(t *testing.T) {
	if _, ok := unlockCheckers[KindNetflix]; !ok {
		t.Fatal("netflix unlock checker not registered for KindNetflix")
	}
}

// TestClassifyNetflixTitle 单部影片可看状态解析(状态码 + 正文),表驱动。
func TestClassifyNetflixTitle(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   nfPlay
	}{
		{"200 playable", 200, "<html>watchable</html>", nfPlayYes},
		{"404 title not in region", 404, "<html>page-404</html>", nfPlayNo},
		{"403 region blocked", 403, "<html>forbidden</html>", nfPlayBlocked},
		{"proxy detected marker forces blocked", 200, `{"errorCode":"NSEZ-403"}`, nfPlayBlocked},
		{"unexpected status", 500, "oops", nfPlayUnknown},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := classifyNetflixTitle(c.status, c.body); got != c.want {
				t.Errorf("classifyNetflixTitle(%d) = %v, want %v", c.status, got, c.want)
			}
		})
	}
}

// TestDecideNetflixLevel 三级判定聚合:非自制可看=full,仅自制可看=originals_only,均不可=blocked。
func TestDecideNetflixLevel(t *testing.T) {
	cases := []struct {
		name        string
		nonOriginal nfPlay
		original    nfPlay
		want        string
	}{
		{"full: non-original playable", nfPlayYes, nfPlayYes, LevelFull},
		{"full even if original unknown", nfPlayYes, nfPlayUnknown, LevelFull},
		{"originals only: only original playable", nfPlayNo, nfPlayYes, LevelOriginalsOnly},
		{"blocked: both 404", nfPlayNo, nfPlayNo, LevelBlocked},
		{"blocked: region forbidden", nfPlayBlocked, nfPlayBlocked, LevelBlocked},
		{"blocked: any 403 is region-level", nfPlayUnknown, nfPlayBlocked, LevelBlocked},
		{"inconclusive: both unknown", nfPlayUnknown, nfPlayUnknown, ""},
		{"inconclusive: 404 + unknown", nfPlayNo, nfPlayUnknown, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := decideNetflixLevel(c.nonOriginal, c.original); got != c.want {
				t.Errorf("decideNetflixLevel(%v,%v) = %q, want %q", c.nonOriginal, c.original, got, c.want)
			}
		})
	}
}

// TestParseNetflixRegion 地区解析表驱动:能解析写国家码,解析不到留空(不报错)。
func TestParseNetflixRegion(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"requestCountry id", `x netflix.reactContext = {"requestCountry":{"id":"US","timezone":"x"}}`, "US"},
		{"countryOfSignup", `{"countryOfSignup":"HK","other":1}`, "HK"},
		{"requestCountry preferred over signup", `{"requestCountry":{"id":"JP"},"countryOfSignup":"US"}`, "JP"},
		{"no marker", "<html>nothing here</html>", ""},
		{"empty body", "", ""},
		{"lowercase not matched", `{"countryOfSignup":"us"}`, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := parseNetflixRegion(c.body); got != c.want {
				t.Errorf("parseNetflixRegion() = %q, want %q", got, c.want)
			}
		})
	}
}

// fakeNetflixRT 按请求 URL 中的 title ID 路由到预置响应,不触真实网络。
type fakeNetflixRT struct {
	nonOriginal fakeResp
	original    fakeResp
}

type fakeResp struct {
	status int
	body   string
	err    error
}

func (f fakeNetflixRT) RoundTrip(req *http.Request) (*http.Response, error) {
	var r fakeResp
	switch {
	case strings.Contains(req.URL.Path, netflixNonOriginalTitle):
		r = f.nonOriginal
	case strings.Contains(req.URL.Path, netflixOriginalTitle):
		r = f.original
	default:
		return nil, fmt.Errorf("unexpected url %s", req.URL)
	}
	if r.err != nil {
		return nil, r.err
	}
	return &http.Response{
		StatusCode: r.status,
		Body:       io.NopCloser(strings.NewReader(r.body)),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}

// TestNetflixChecker_FakeTransport 集成:经 fake transport 覆盖 full/originals_only/blocked/请求失败四类。
func TestNetflixChecker_FakeTransport(t *testing.T) {
	node := &subscription.Node{Server: "example.com", Port: 443}
	target := Target{Name: "Netflix", Kind: KindNetflix}
	regionBody := `{"requestCountry":{"id":"US"}}`

	cases := []struct {
		name          string
		rt            fakeNetflixRT
		wantAvailable bool
		wantLevel     string
		wantRegion    string
		wantErr       bool
	}{
		{
			name:          "full unlock",
			rt:            fakeNetflixRT{nonOriginal: fakeResp{status: 200, body: regionBody}, original: fakeResp{status: 200, body: regionBody}},
			wantAvailable: true, wantLevel: LevelFull, wantRegion: "US",
		},
		{
			name:          "originals only",
			rt:            fakeNetflixRT{nonOriginal: fakeResp{status: 404, body: "page-404"}, original: fakeResp{status: 200, body: regionBody}},
			wantAvailable: true, wantLevel: LevelOriginalsOnly, wantRegion: "US",
		},
		{
			name:          "blocked",
			rt:            fakeNetflixRT{nonOriginal: fakeResp{status: 404, body: "page-404"}, original: fakeResp{status: 404, body: "page-404"}},
			wantAvailable: false, wantLevel: LevelBlocked,
		},
		{
			name:    "request failure",
			rt:      fakeNetflixRT{nonOriginal: fakeResp{err: fmt.Errorf("dial timeout")}, original: fakeResp{status: 200, body: regionBody}},
			wantErr: true,
		},
		{
			name:    "inconclusive: unexpected status both",
			rt:      fakeNetflixRT{nonOriginal: fakeResp{status: 503, body: "busy"}, original: fakeResp{status: 503, body: "busy"}},
			wantErr: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			client := &http.Client{Transport: c.rt}
			got := netflixChecker(context.Background(), client, node, target)

			if got.NodeKey != node.NodeKey() {
				t.Errorf("NodeKey = %q, want %q", got.NodeKey, node.NodeKey())
			}
			if got.TargetName != target.Name {
				t.Errorf("TargetName = %q, want %q", got.TargetName, target.Name)
			}
			if c.wantErr {
				if got.Error == "" {
					t.Error("expected non-empty Error on request failure")
				}
				if got.Available {
					t.Error("expected Available=false on request failure")
				}
				if got.Level != "" {
					t.Errorf("expected empty Level on request failure, got %q", got.Level)
				}
				return
			}
			if got.Error != "" {
				t.Errorf("unexpected Error: %q", got.Error)
			}
			if got.Available != c.wantAvailable {
				t.Errorf("Available = %v, want %v", got.Available, c.wantAvailable)
			}
			if got.Level != c.wantLevel {
				t.Errorf("Level = %q, want %q", got.Level, c.wantLevel)
			}
			if got.Region != c.wantRegion {
				t.Errorf("Region = %q, want %q", got.Region, c.wantRegion)
			}
			if got.Latency < 0 {
				t.Errorf("Latency = %d, want >= 0", got.Latency)
			}
		})
	}
}
