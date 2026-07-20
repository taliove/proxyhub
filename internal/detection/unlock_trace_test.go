package detection

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/taliove/proxyhub/internal/subscription"
)

// TestParseTraceLoc 表驱动:从 Cloudflare trace 正文解析 loc= 国家码(容忍空格/缺失)。
func TestParseTraceLoc(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"typical", "fl=1x2\nh=example.com\nloc=US\nwarp=off\n", "US"},
		{"hk only", "loc=HK\n", "HK"},
		{"loc with spaces", "loc= JP \n", "JP"},
		{"missing loc", "fl=1\nwarp=off\n", ""},
		{"empty body", "", ""},
	}
	for _, c := range cases {
		if got := parseTraceLoc(c.body); got != c.want {
			t.Errorf("%s: parseTraceLoc(%q) = %q, want %q", c.name, c.body, got, c.want)
		}
	}
}

// TestFetchTraceLoc_Success trace 请求成功时返回解析出的国家码。
func TestFetchTraceLoc_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("fl=1\nloc=SG\nwarp=off\n"))
	}))
	defer srv.Close()

	got := fetchTraceLoc(context.Background(), srv.Client(), srv.URL)
	if got != "SG" {
		t.Fatalf("fetchTraceLoc = %q, want SG", got)
	}
}

// TestFetchTraceLoc_Failure trace 请求失败(5xx / 不可达)时返回空,不 panic。
func TestFetchTraceLoc_Failure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	if got := fetchTraceLoc(context.Background(), srv.Client(), srv.URL); got != "" {
		t.Errorf("fetchTraceLoc on 500 = %q, want empty", got)
	}
	if got := fetchTraceLoc(context.Background(), http.DefaultClient, "http://127.0.0.1:0"); got != "" {
		t.Errorf("fetchTraceLoc on unreachable = %q, want empty", got)
	}
}

// TestClassifyByMarkers 表驱动:403+标记=blocked;2xx=available;其余(裸403/5xx/429)=inconclusive。
func TestClassifyByMarkers(t *testing.T) {
	markers := []string{"unsupported_country"}
	cases := []struct {
		name   string
		status int
		body   string
		want   unlockVerdict
	}{
		{"403 with marker", http.StatusForbidden, `{"error":{"code":"unsupported_country"}}`, verdictBlocked},
		{"403 marker case-insensitive", http.StatusForbidden, "UNSUPPORTED_COUNTRY", verdictBlocked},
		{"200 ok", http.StatusOK, `{"cookie_config":{}}`, verdictAvailable},
		{"204 ok", http.StatusNoContent, "", verdictAvailable},
		{"bare 403 no marker", http.StatusForbidden, "forbidden", verdictInconclusive},
		{"500 server error", http.StatusBadGateway, "bad gateway", verdictInconclusive},
		{"429 rate limited", http.StatusTooManyRequests, "slow down", verdictInconclusive},
	}
	for _, c := range cases {
		if got := classifyByMarkers(c.status, c.body, markers); got != c.want {
			t.Errorf("%s: classifyByMarkers(%d) = %v, want %v", c.name, c.status, got, c.want)
		}
	}
}

// TestRunUnlockCheck 的通用行为:available/blocked 填 region;error 路径不触 trace、region 留空。
func TestRunUnlockCheck(t *testing.T) {
	node := &subscription.Node{Server: "example.com", Port: 443}
	target := Target{Name: "OpenAI", Kind: KindOpenAI}

	traceOK := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("loc=US\n"))
	}))
	defer traceOK.Close()
	traceFail := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer traceFail.Close()

	newProbe := func(status int, body string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		}))
	}

	t.Run("available fills full and region", func(t *testing.T) {
		probe := newProbe(http.StatusOK, `{"cookie_config":{}}`)
		defer probe.Close()
		res := runUnlockCheck(context.Background(), probe.Client(), node, target, probe.URL, traceOK.URL, classifyOpenAI)
		if !res.Available || res.Level != LevelFull {
			t.Fatalf("available=%v level=%q, want true/full", res.Available, res.Level)
		}
		if res.Region != "US" {
			t.Errorf("region = %q, want US", res.Region)
		}
		if res.Error != "" {
			t.Errorf("error = %q, want empty", res.Error)
		}
		if res.NodeKey != node.NodeKey() || res.TargetName != target.Name {
			t.Errorf("identity mismatch: %q/%q", res.NodeKey, res.TargetName)
		}
	})

	t.Run("blocked fills blocked and region", func(t *testing.T) {
		probe := newProbe(http.StatusForbidden, `{"error":{"code":"unsupported_country"}}`)
		defer probe.Close()
		res := runUnlockCheck(context.Background(), probe.Client(), node, target, probe.URL, traceOK.URL, classifyOpenAI)
		if res.Available || res.Level != LevelBlocked {
			t.Fatalf("available=%v level=%q, want false/blocked", res.Available, res.Level)
		}
		if res.Region != "US" {
			t.Errorf("region = %q, want US", res.Region)
		}
		if res.Error != "" {
			t.Errorf("error = %q, want empty on blocked", res.Error)
		}
	})

	t.Run("5xx goes error path not blocked", func(t *testing.T) {
		probe := newProbe(http.StatusBadGateway, "bad gateway")
		defer probe.Close()
		res := runUnlockCheck(context.Background(), probe.Client(), node, target, probe.URL, traceOK.URL, classifyOpenAI)
		if res.Available {
			t.Errorf("available = true, want false")
		}
		if res.Level == LevelBlocked {
			t.Errorf("level = blocked, want empty (must not misjudge 5xx as blocked)")
		}
		if res.Error == "" {
			t.Errorf("error empty, want non-empty on 5xx")
		}
		if res.Region != "" {
			t.Errorf("region = %q, want empty on error path (trace not consulted)", res.Region)
		}
	})

	t.Run("transport error goes error path", func(t *testing.T) {
		res := runUnlockCheck(context.Background(), http.DefaultClient, node, target, "http://127.0.0.1:0", traceOK.URL, classifyOpenAI)
		if res.Available || res.Level == LevelBlocked || res.Error == "" {
			t.Errorf("transport error mishandled: available=%v level=%q error=%q", res.Available, res.Level, res.Error)
		}
	})

	t.Run("trace failure leaves region empty without breaking verdict", func(t *testing.T) {
		probe := newProbe(http.StatusOK, `{"cookie_config":{}}`)
		defer probe.Close()
		res := runUnlockCheck(context.Background(), probe.Client(), node, target, probe.URL, traceFail.URL, classifyOpenAI)
		if !res.Available || res.Level != LevelFull {
			t.Fatalf("available=%v level=%q, want true/full despite trace failure", res.Available, res.Level)
		}
		if res.Region != "" {
			t.Errorf("region = %q, want empty when trace fails", res.Region)
		}
	})
}
