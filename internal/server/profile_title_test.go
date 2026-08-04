package server

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/taliove/proxyhub/internal/geoip"
	"github.com/taliove/proxyhub/internal/store"
)

// 期望值写死(非现算)防回归:「·」是 U+00B7(UTF-8 C2 B7),中文按 UTF-8 字节
// 编码;base64 与 RFC 5987 两套编码独立断言。

// TestSubscription_ProfileHeaders_NoPublicName an endpoint without a public
// name falls back to the bare brand title on both naming headers.
func TestSubscription_ProfileHeaders_NoPublicName(t *testing.T) {
	srv, st := newTestServer(t, pullLogNodes())
	h := srv.Handler()
	ep, _ := st.CreateEndpoint("example.com")

	req := httptest.NewRequest("GET", "/sub/"+ep.Path+"?token="+ep.Token, nil)
	req.RemoteAddr = "5.6.7.8:1234"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Profile-Title"); got != "UHJveHlIdWI=" {
		t.Errorf("Profile-Title = %q, want base64(\"ProxyHub\")", got)
	}
	wantCD := "attachment; filename*=UTF-8''ProxyHub"
	if got := w.Header().Get("Content-Disposition"); got != wantCD {
		t.Errorf("Content-Disposition = %q, want %q", got, wantCD)
	}
}

// TestSubscription_ProfileHeaders_NonASCIIPublicName a public name with the
// middle dot and Chinese characters is encoded as UTF-8 bytes in both the
// base64 Profile-Title and the RFC 5987 filename*.
func TestSubscription_ProfileHeaders_NonASCIIPublicName(t *testing.T) {
	srv, st := newTestServer(t, pullLogNodes())
	h := srv.Handler()
	ep, _ := st.CreateEndpoint("example.com")
	if err := st.UpdateEndpointPublicName(ep.ID, "家里宽带"); err != nil {
		t.Fatalf("UpdateEndpointPublicName: %v", err)
	}

	req := httptest.NewRequest("GET", "/sub/"+ep.Path+"?token="+ep.Token, nil)
	req.RemoteAddr = "5.6.7.8:1234"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Profile-Title"); got != "UHJveHlIdWIgwrcg5a626YeM5a695bim" {
		t.Errorf("Profile-Title = %q, want base64(\"ProxyHub · 家里宽带\")", got)
	}
	wantCD := "attachment; filename*=UTF-8''ProxyHub%20%C2%B7%20%E5%AE%B6%E9%87%8C%E5%AE%BD%E5%B8%A6"
	if got := w.Header().Get("Content-Disposition"); got != wantCD {
		t.Errorf("Content-Disposition = %q, want %q", got, wantCD)
	}
}

// TestSubscription_ProfileHeaders_AliasNeverLeaks the private alias must not
// appear in any response header value, whether or not a public name is set.
func TestSubscription_ProfileHeaders_AliasNeverLeaks(t *testing.T) {
	srv, st := newTestServer(t, pullLogNodes())
	h := srv.Handler()
	const alias = "李总的私有备注"
	ep, _ := st.CreateEndpoint(alias)
	if err := st.UpdateEndpointPublicName(ep.ID, "office"); err != nil {
		t.Fatalf("UpdateEndpointPublicName: %v", err)
	}

	req := httptest.NewRequest("GET", "/sub/"+ep.Path+"?token="+ep.Token, nil)
	req.RemoteAddr = "5.6.7.8:1234"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	for name, values := range w.Header() {
		for _, v := range values {
			if strings.Contains(v, alias) {
				t.Errorf("header %s leaks the private alias: %q", name, v)
			}
		}
	}
}

// TestSubscription_ProfileHeaders_GuardPathsOmitHeaders the naming headers are
// a success-path feature: a bad token (404) and guard rejections (429/403)
// must not carry them.
func TestSubscription_ProfileHeaders_GuardPathsOmitHeaders(t *testing.T) {
	assertNoProfileHeaders := func(t *testing.T, label string, w *httptest.ResponseRecorder) {
		t.Helper()
		if got := w.Header().Get("Profile-Title"); got != "" {
			t.Errorf("%s: Profile-Title = %q, want absent", label, got)
		}
		if got := w.Header().Get("Content-Disposition"); got != "" {
			t.Errorf("%s: Content-Disposition = %q, want absent", label, got)
		}
	}

	// 404: bad token never reaches the guard chain or the header block.
	srv, st := newTestServer(t, pullLogNodes())
	h := srv.Handler()
	ep, _ := st.CreateEndpoint("example.com")
	if err := st.UpdateEndpointPublicName(ep.ID, "office"); err != nil {
		t.Fatalf("UpdateEndpointPublicName: %v", err)
	}
	req := httptest.NewRequest("GET", "/sub/"+ep.Path+"?token=wrong", nil)
	req.RemoteAddr = "5.6.7.8:1234"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("bad token: status = %d, want 404", w.Code)
	}
	assertNoProfileHeaders(t, "404 bad token", w)

	// 429: a blocking guard owns the response.
	srv2, st2 := newTestServer(t, pullLogNodes())
	srv2.subGuards = []subGuard{blockingSpy("spy")}
	h2 := srv2.Handler()
	ep2, _ := st2.CreateEndpoint("example.com")
	if err := st2.UpdateEndpointPublicName(ep2.ID, "office"); err != nil {
		t.Fatalf("UpdateEndpointPublicName: %v", err)
	}
	req2 := httptest.NewRequest("GET", "/sub/"+ep2.Path+"?token="+ep2.Token, nil)
	req2.RemoteAddr = "6.6.6.6:2222"
	w2 := httptest.NewRecorder()
	h2.ServeHTTP(w2, req2)
	if w2.Code != http.StatusTooManyRequests {
		t.Fatalf("blocked pull: status = %d, want 429", w2.Code)
	}
	assertNoProfileHeaders(t, "429 guard block", w2)

	// 403: the geo guard in enforce mode rejects a foreign location.
	srv3, st3 := newTestServer(t, pullLogNodes())
	srv3.countryLookup = func(ip string) (string, error) {
		if f, ok := geoTable[ip]; ok {
			return f.country, nil
		}
		return "", geoip.ErrCountryNotFound
	}
	srv3.subGuards = srv3.newSubGuardChain()
	h3 := srv3.Handler()
	ep3, _ := st3.CreateEndpoint("example.com")
	if err := st3.UpdateEndpointPublicName(ep3.ID, "office"); err != nil {
		t.Fatalf("UpdateEndpointPublicName: %v", err)
	}
	if err := st3.UpdateEndpointGeoConfig(ep3.ID, store.GeoModeEnforce, "CN", ""); err != nil {
		t.Fatalf("UpdateEndpointGeoConfig: %v", err)
	}
	w3 := pullFrom(t, h3, ep3, "8.8.8.8")
	if w3.Code != http.StatusForbidden {
		t.Fatalf("geo miss: status = %d, want 403", w3.Code)
	}
	assertNoProfileHeaders(t, "403 geo block", w3)
}

// decodeSubBody 解开 base64(v2ray 格式)订阅体明文;非 base64 直接原样返回
// (clash YAML 路径),便于统一断言。
func decodeSubBody(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	if plain, err := base64.StdEncoding.DecodeString(w.Body.String()); err == nil {
		return string(plain)
	}
	return w.Body.String()
}

// TestSubscription_ShadowrocketRemarks 小火箭的订阅命名通道(issue #39,QA 实测
// 确认):Shadowrocket UA 拉 base64(v2ray 格式)订阅时,明文开头注入
// REMARKS=<profile 标题> 行;其余节点链接原样保留。
func TestSubscription_ShadowrocketRemarks(t *testing.T) {
	srv, st := newTestServer(t, pullLogNodes())
	h := srv.Handler()
	ep, _ := st.CreateEndpoint("example.com")
	if err := st.UpdateEndpointPublicName(ep.ID, "家里宽带"); err != nil {
		t.Fatalf("UpdateEndpointPublicName: %v", err)
	}

	req := httptest.NewRequest("GET", "/sub/"+ep.Path+"?token="+ep.Token, nil)
	req.RemoteAddr = "5.6.7.8:1234"
	req.Header.Set("User-Agent", "Shadowrocket/3378")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
	plain := decodeSubBody(t, w)
	lines := strings.Split(plain, "\n")
	if len(lines) != 2 || lines[0] != "REMARKS=ProxyHub · 家里宽带" || !strings.HasPrefix(lines[1], "ss://") {
		t.Errorf("want exactly [REMARKS 行, ss 链接], got %d 行:\n%s", len(lines), plain)
	}
}

// TestSubscription_ShadowrocketRemarks_NoPublicName 未设公开名称时 REMARKS
// 回退裸品牌名,与 Profile-Title 同一合成规则。
func TestSubscription_ShadowrocketRemarks_NoPublicName(t *testing.T) {
	srv, st := newTestServer(t, pullLogNodes())
	h := srv.Handler()
	ep, _ := st.CreateEndpoint("example.com")

	req := httptest.NewRequest("GET", "/sub/"+ep.Path+"?token="+ep.Token, nil)
	req.RemoteAddr = "5.6.7.8:1234"
	req.Header.Set("User-Agent", "Shadowrocket/3378")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
	firstLine, _, _ := strings.Cut(decodeSubBody(t, w), "\n")
	if firstLine != "REMARKS=ProxyHub" {
		t.Errorf("first line = %q, want REMARKS=ProxyHub", firstLine)
	}
}

// TestSubscription_RemarksOnlyForShadowrocket 注入是小火箭专属通道:其他
// UA(含同样走 v2ray 格式的 v2rayNG、走 clash 的 mihomo)的订阅体都不含
// REMARKS 行;私有 alias 也绝不借 REMARKS 泄漏。
func TestSubscription_RemarksOnlyForShadowrocket(t *testing.T) {
	const alias = "李总的私有备注"
	for _, tc := range []struct {
		label string
		ua    string
	}{
		{"v2rayNG", "v2rayNG/1.8.5"},
		{"clash(mihomo)", "mihomo/v1.18.0"},
		{"no UA", ""},
	} {
		t.Run(tc.label, func(t *testing.T) {
			srv, st := newTestServer(t, pullLogNodes())
			h := srv.Handler()
			ep, _ := st.CreateEndpoint(alias)
			if err := st.UpdateEndpointPublicName(ep.ID, "office"); err != nil {
				t.Fatalf("UpdateEndpointPublicName: %v", err)
			}
			req := httptest.NewRequest("GET", "/sub/"+ep.Path+"?token="+ep.Token, nil)
			req.RemoteAddr = "5.6.7.8:1234"
			if tc.ua != "" {
				req.Header.Set("User-Agent", tc.ua)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
			}
			plain := decodeSubBody(t, w)
			if strings.Contains(plain, "REMARKS=") {
				t.Errorf("UA %q must not get a REMARKS line, body head: %.80s", tc.ua, plain)
			}
			if strings.Contains(plain, alias) {
				t.Errorf("UA %q body leaks the private alias", tc.ua)
			}
		})
	}
}

// TestInjectShadowrocketRemarks_InvalidBase64 解码失败的 fallback 契约:坏输入
// 原样返回(线上不可达——GenerateV2Ray 输出恒为合法 base64,这里直接钉函数契约)。
func TestInjectShadowrocketRemarks_InvalidBase64(t *testing.T) {
	bad := []byte("%%%")
	ep := &store.Endpoint{}
	got := injectShadowrocketRemarks(bad, ep)
	if string(got) != string(bad) {
		t.Errorf("invalid base64 should pass through unchanged, got %q", got)
	}
}

// TestRFC5987Encode pins the attr-char whitelist: unreserved bytes pass
// through, everything else (spaces, UTF-8 multibyte, reserved punctuation)
// becomes uppercase %XX.
func TestRFC5987Encode(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"ProxyHub", "ProxyHub"},
		{"a b", "a%20b"},
		{"·", "%C2%B7"},
		{"家里宽带", "%E5%AE%B6%E9%87%8C%E5%AE%BD%E5%B8%A6"},
		{"a+b!c#d$e&f-g.h^i_j`k|l~m", "a+b!c#d$e&f-g.h^i_j`k|l~m"},
		{"a/b?c=d%", "a%2Fb%3Fc%3Dd%25"},
		{"\r\n", "%0D%0A"},
	}
	for _, c := range cases {
		if got := rfc5987Encode(c.in); got != c.want {
			t.Errorf("rfc5987Encode(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
