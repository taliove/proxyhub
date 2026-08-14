package server

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/taliove/proxyhub/internal/subscription"
)

// formatTestNodes 一条可用的机场节点,够生成两种格式即可。
func formatTestNodes() []*subscription.Node {
	return []*subscription.Node{
		{Name: "格式节点", Type: "ss", Server: "fmt.example.com", Port: 8388,
			Cipher: "aes-256-gcm", Password: "p", Available: true, Source: "机场A"},
	}
}

// getSubscription 拉一次 /sub 并断言 200,返回 recorder 供调用方检查头与体。
func getSubscription(t *testing.T, h http.Handler, path, token, query string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", "/sub/"+path+"?token="+token+query, nil)
	req.RemoteAddr = "1.2.3.4:5678"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	return w
}

// TestSubscription_UANegotiation 钉死 UA 分流(issue #122 / ADR 0049):
// Clash 系 UA 得 YAML;空 UA、浏览器、curl、未知客户端一律默认 base64
// (最小必要暴露)。显式 format 参数永远优先于 UA。
func TestSubscription_UANegotiation(t *testing.T) {
	srv, st := newTestServer(t, formatTestNodes())
	h := srv.Handler()
	ep, err := st.CreateEndpointForUser(0, "dev")
	if err != nil {
		t.Fatalf("CreateEndpointForUser: %v", err)
	}

	for _, tc := range []struct {
		label    string
		ua       string
		query    string
		wantYaml bool
	}{
		// Clash 系名单(含大小写与厂商变体)
		{"clash for windows", "ClashforWindows/0.20.39", "", true},
		{"mihomo", "mihomo/v1.18.0", "", true},
		{"clash.meta", "clash.meta/1.17.0", "", true},
		{"clash-verge", "Clash-Verge/2.0.0", "", true},
		{"flclash", "FlClash/0.8.70", "", true},
		{"clashx", "ClashX/1.95.1", "", true},
		{"stash", "Stash/2.6.0", "", true},
		// 其余一切默认 base64
		{"empty UA", "", "", false},
		{"browser", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36", "", false},
		{"curl", "curl/8.5.0", "", false},
		{"v2rayNG", "v2rayNG/1.8.5", "", false},
		{"shadowrocket", "Shadowrocket/3378", "", false},
		{"sing-box", "sing-box/1.9.0", "", false},
		// 子串边界:近形但不含名单 token 的 UA 不得误判
		{"near-miss classico", "classico/1.0", "", false},
		{"near-miss stashed", "stashed-away/2.0", "", true}, // 含 "stash" 子串,按名单语义命中(钉死现状)
		// 显式 format 永远优先于 UA
		{"explicit base64 beats mihomo", "mihomo/v1.18.0", "&format=base64", false},
		{"explicit v2ray alias beats mihomo", "mihomo/v1.18.0", "&format=v2ray", false},
		{"explicit clash beats curl", "curl/8.5.0", "&format=clash", true},
	} {
		t.Run(tc.label, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/sub/"+ep.Path+"?token="+ep.Token+tc.query, nil)
			req.RemoteAddr = "1.2.3.4:5678"
			if tc.ua != "" {
				req.Header.Set("User-Agent", tc.ua)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
			}
			ct := w.Header().Get("Content-Type")
			isYaml := strings.HasPrefix(ct, "text/yaml")
			if isYaml != tc.wantYaml {
				t.Errorf("UA %q query %q: Content-Type = %q, wantYaml = %v", tc.ua, tc.query, ct, tc.wantYaml)
			}
		})
	}
}

// TestSubscription_FormatBase64IsCanonical 钉死 issue #121 核心:
// format=base64 是规范值,产出与 v2ray 别名逐字节一致(base64 链接列表)。
func TestSubscription_FormatBase64IsCanonical(t *testing.T) {
	srv, st := newTestServer(t, formatTestNodes())
	h := srv.Handler()
	ep, err := st.CreateEndpointForUser(0, "dev")
	if err != nil {
		t.Fatalf("CreateEndpointForUser: %v", err)
	}

	canonical := getSubscription(t, h, ep.Path, ep.Token, "&format=base64")
	alias := getSubscription(t, h, ep.Path, ep.Token, "&format=v2ray")

	if ct := canonical.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("base64 Content-Type = %q, want text/plain", ct)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(canonical.Body.String()))
	if err != nil {
		t.Fatalf("base64 output not decodable: %v", err)
	}
	if !strings.Contains(string(raw), "ss://") {
		t.Errorf("base64 output should contain share links, got: %q", raw)
	}
	if canonical.Body.String() != alias.Body.String() {
		t.Errorf("format=base64 and format=v2ray must produce identical output")
	}
}

// TestSubscription_CacheControlNoStore 钉死 issue #121:两种格式的订阅响应
// 都带 Cache-Control: no-store,既有 profile 命名头不回归。
func TestSubscription_CacheControlNoStore(t *testing.T) {
	srv, st := newTestServer(t, formatTestNodes())
	h := srv.Handler()
	ep, err := st.CreateEndpointForUser(0, "dev")
	if err != nil {
		t.Fatalf("CreateEndpointForUser: %v", err)
	}

	for _, format := range []string{"clash", "base64"} {
		w := getSubscription(t, h, ep.Path, ep.Token, "&format="+format)
		if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
			t.Errorf("format=%s Cache-Control = %q, want no-store", format, cc)
		}
		if w.Header().Get("Profile-Update-Interval") == "" {
			t.Errorf("format=%s missing Profile-Update-Interval", format)
		}
		if w.Header().Get("Profile-Title") == "" {
			t.Errorf("format=%s missing Profile-Title", format)
		}
		if w.Header().Get("Content-Disposition") == "" {
			t.Errorf("format=%s missing Content-Disposition", format)
		}
	}
}

// TestSubscription_InvalidFormatFallsBackToClash 非法 format 值行为不变:
// 回退默认格式(渲染层 default 分支 = Clash YAML)。
func TestSubscription_InvalidFormatFallsBackToClash(t *testing.T) {
	srv, st := newTestServer(t, formatTestNodes())
	h := srv.Handler()
	ep, err := st.CreateEndpointForUser(0, "dev")
	if err != nil {
		t.Fatalf("CreateEndpointForUser: %v", err)
	}

	w := getSubscription(t, h, ep.Path, ep.Token, "&format=bogus")
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/yaml") {
		t.Errorf("invalid format Content-Type = %q, want text/yaml (clash fallback)", ct)
	}
}

// TestEndpointPreview_FormatBase64 后台预览接口同样识别 base64 规范值与
// v2ray 别名,两者内容一致。
func TestEndpointPreview_FormatBase64(t *testing.T) {
	srv, st := newTestServer(t, formatTestNodes())
	h := srv.Handler()
	ep, err := st.CreateEndpointForUser(0, "dev")
	if err != nil {
		t.Fatalf("CreateEndpointForUser: %v", err)
	}
	cookie := authCookie(t, h)

	preview := func(query string) (string, string) {
		t.Helper()
		req := httptest.NewRequest("GET", "/api/endpoints/"+strconv.FormatInt(ep.ID, 10)+"/preview?"+query, nil)
		req.AddCookie(cookie)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("preview status = %d, want 200 (body: %s)", w.Code, w.Body.String())
		}
		var resp struct {
			Format  string `json:"format"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal preview response: %v", err)
		}
		return resp.Format, resp.Content
	}

	format, canonical := preview("format=base64")
	if format != "base64" {
		t.Errorf("preview format = %q, want base64", format)
	}
	if _, err := base64.StdEncoding.DecodeString(strings.TrimSpace(canonical)); err != nil {
		t.Fatalf("preview base64 content not decodable: %v", err)
	}
	_, alias := preview("format=v2ray")
	if canonical != alias {
		t.Errorf("preview format=base64 and format=v2ray must produce identical content")
	}
}
