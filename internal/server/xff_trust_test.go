package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/taliove/proxyhub/internal/store"
)

// End-to-end pins for the first-push security review C1/C2 fixes:
// a forwarded 127.0.0.1 is caller-controlled and must never unlock any
// loopback exemption (IP2Ban, honeypot, captcha, IP deny list), and
// /api/setup must not be claimable by a remote caller.

// doLoginFrom issues a login request with an explicit peer address and
// optional forwarding headers, so tests can emulate both direct clients and
// reverse-proxy hops (including hostile header injection).
func doLoginFrom(t *testing.T, h http.Handler, username, password, peer, xff string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	req := httptest.NewRequest("POST", "/api/login", bytes.NewReader(body))
	req.RemoteAddr = peer + ":2000"
	if xff != "" {
		req.Header.Set("X-Forwarded-For", xff)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// TestLogin_HoneypotForwardedLoopbackBanned (C1): spoofing
// X-Forwarded-For: 127.0.0.1 through a loopback peer must NOT exempt the
// caller from the honeypot ban - the old ip == "127.0.0.1" carve-out made
// this a one-line bypass of every IP-based defence.
func TestLogin_HoneypotForwardedLoopbackBanned(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")

	w := doLoginFrom(t, h, "admin", "whatever", "127.0.0.1", "127.0.0.1")
	if w.Code != http.StatusForbidden {
		t.Fatalf("honeypot via forwarded loopback: status = %d, want 403 (body: %s)", w.Code, w.Body.String())
	}
	banned, err := st.IsBanned("127.0.0.1", time.Now())
	if err != nil {
		t.Fatalf("IsBanned: %v", err)
	}
	if !banned {
		t.Error("honeypot ban was not applied to forwarded 127.0.0.1")
	}
}

// TestLogin_HoneypotDirectLoopbackExempt: a header-less local connection
// keeps the historical escape hatch (local development must never lock
// itself out).
func TestLogin_HoneypotDirectLoopbackExempt(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")

	w := doLoginFrom(t, h, "admin", "whatever", "127.0.0.1", "")
	if w.Code == http.StatusForbidden {
		t.Fatalf("honeypot via direct loopback: status = 403, want non-403 (ban must not apply)")
	}
	banned, err := st.IsBanned("127.0.0.1", time.Now())
	if err != nil {
		t.Fatalf("IsBanned: %v", err)
	}
	if banned {
		t.Error("direct loopback must never be banned")
	}
}

// TestLogin_BruteForceForwardedLoopbackBanned (C1): password brute force
// behind a spoofed XFF 127.0.0.1 walks into IP2Ban exactly like any other
// source.
func TestLogin_BruteForceForwardedLoopbackBanned(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass") // ban_threshold = 3 in doSetup

	for i := 0; i < 3; i++ {
		w := doLoginFrom(t, h, "owner", "wrong-password", "127.0.0.1", "127.0.0.1")
		if w.Code == http.StatusForbidden {
			t.Fatalf("attempt %d: banned too early", i+1)
		}
	}
	banned, err := st.IsBanned("127.0.0.1", time.Now())
	if err != nil {
		t.Fatalf("IsBanned: %v", err)
	}
	if !banned {
		t.Error("brute force via forwarded loopback did not trigger IP2Ban")
	}
}

// TestSetup_RemoteCallerRejected (C2): see setup_gate_test.go.

// TestIPFilter_ForwardedLoopbackNotExempt (C1): a global deny rule now also
// matches a forwarded 127.0.0.1 - the store no longer special-cases loopback,
// and the middleware only skips the check for direct local connections.
func TestIPFilter_ForwardedLoopbackNotExempt(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	if _, err := st.AddIPAccessRule("127.0.0.0/8", store.IPRuleScopeGlobal, store.IPRuleSourceManual, "", 0); err != nil {
		t.Fatalf("AddIPAccessRule: %v", err)
	}

	newReq := func(xff string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/api/status", nil)
		req.RemoteAddr = "127.0.0.1:3000"
		if xff != "" {
			req.Header.Set("X-Forwarded-For", xff)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w
	}

	if w := newReq(""); w.Code != http.StatusOK {
		t.Fatalf("direct loopback with deny rule: status = %d, want 200 (escape hatch)", w.Code)
	}
	if w := newReq("127.0.0.1"); w.Code != http.StatusNotFound {
		t.Fatalf("forwarded loopback with deny rule: status = %d, want 404", w.Code)
	}
}
