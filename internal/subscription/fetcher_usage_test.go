package subscription

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestFetch_CapturesUsageHeaders 拉取型机场:200 响应带 subscription-userinfo /
// profile-web-page-url 头时,用量信息随诊断捕获(spec-manual-airport-import T3)。
func TestFetch_CapturesUsageHeaders(t *testing.T) {
	node := "trojan://pw@node1.example.com:443#HK 01"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("subscription-userinfo", "upload=100; download=200; total=1000; expire=1893456000")
		w.Header().Set("profile-web-page-url", "https://example.com")
		w.Write([]byte(base64.StdEncoding.EncodeToString([]byte(node))))
	}))
	t.Cleanup(srv.Close)

	f := NewFetcher(0)
	_, diag, err := f.FetchWithDiagnostics("测试机场", srv.URL)
	if err != nil {
		t.Fatalf("FetchWithDiagnostics() error = %v", err)
	}
	if diag.Usage == nil {
		t.Fatal("Usage = nil, want captured from headers")
	}
	u := diag.Usage
	if u.Upload != 100 || u.Download != 200 || u.Total != 1000 || u.Expire != 1893456000 {
		t.Errorf("Usage = %+v, want 100/200/1000/1893456000", u)
	}
	if u.WebPageURL != "https://example.com" {
		t.Errorf("WebPageURL = %q, want https://example.com", u.WebPageURL)
	}
}

// TestFetch_NoUsageHeaders 无响应头时 Usage 为 nil(调用方据此保留既有落库值)。
func TestFetch_NoUsageHeaders(t *testing.T) {
	node := "trojan://pw@node1.example.com:443#HK 01"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(node))
	}))
	t.Cleanup(srv.Close)

	f := NewFetcher(0)
	_, diag, err := f.FetchWithDiagnostics("测试机场", srv.URL)
	if err != nil {
		t.Fatalf("FetchWithDiagnostics() error = %v", err)
	}
	if diag.Usage != nil {
		t.Errorf("Usage = %+v, want nil when headers absent", diag.Usage)
	}
}

// TestParseUsageHeaders_Malformed 畸形字段容错:非数字/未知键忽略,头在则不返回 nil。
func TestParseUsageHeaders_Malformed(t *testing.T) {
	h := http.Header{}
	h.Set("subscription-userinfo", "upload=abc; download=300; garbage; unknown=1; expire=")
	u := ParseUsageHeaders(h)
	if u == nil {
		t.Fatal("Usage = nil, want non-nil when header present")
	}
	if u.Upload != 0 || u.Download != 300 || u.Total != 0 || u.Expire != 0 {
		t.Errorf("Usage = %+v, want upload=0 download=300 total=0 expire=0", u)
	}
}

// TestParseUsageHeaders_WebPageOnly 只有官网头也返回非 nil(官网手填/捕获同一列)。
func TestParseUsageHeaders_WebPageOnly(t *testing.T) {
	h := http.Header{}
	h.Set("profile-web-page-url", "https://example.com")
	u := ParseUsageHeaders(h)
	if u == nil {
		t.Fatal("Usage = nil, want non-nil when profile-web-page-url present")
	}
	if u.WebPageURL != "https://example.com" {
		t.Errorf("WebPageURL = %q, want https://example.com", u.WebPageURL)
	}
}

// TestParseUsageHeaders_WebPageSchemeWhitelist 官网头 scheme 白名单(Check H2):
// javascript:/data: 等非 http(s) 归一为空串(响应头不可信,最终 <a href> 渲染)。
func TestParseUsageHeaders_WebPageSchemeWhitelist(t *testing.T) {
	cases := []struct {
		header string
		want   string
	}{
		{"https://example.com", "https://example.com"},
		{"http://example.com", "http://example.com"},
		{"HTTPS://example.com", "HTTPS://example.com"},
		{"javascript:alert(1)", ""},
		{"data:text/html,<script>alert(1)</script>", ""},
		{"vbscript:msgbox(1)", ""},
		{"example.com", ""}, // 无 scheme 不收
	}
	for _, c := range cases {
		h := http.Header{}
		h.Set("profile-web-page-url", c.header)
		u := ParseUsageHeaders(h)
		got := ""
		if u != nil {
			got = u.WebPageURL
		}
		if got != c.want {
			t.Errorf("header %q: WebPageURL = %q, want %q", c.header, got, c.want)
		}
	}
}
