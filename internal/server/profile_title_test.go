package server

import (
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
