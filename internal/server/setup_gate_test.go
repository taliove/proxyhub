package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// End-to-end pins for the first-push security review C2 fix: /api/setup
// claims the admin account, so it must only answer a local caller or a
// caller presenting the configured setup token. The documented Docker path
// used to expose this endpoint on 0.0.0.0 to the whole network.

// TestSetup_RemoteCallerRejected (C2): a remote caller must not be able to
// claim the admin account of an uninitialized instance.
func TestSetup_RemoteCallerRejected(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	h := srv.Handler()

	body, _ := json.Marshal(map[string]any{"username": "owner", "password": "a-very-strong-pass"})
	req := httptest.NewRequest("POST", "/api/setup", bytes.NewReader(body))
	req.RemoteAddr = "203.0.113.9:5000"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("remote setup: status = %d, want 403", w.Code)
	}
}

// TestSetup_RemoteCallerWithToken: server.setup_token re-enables remote
// bootstrap explicitly (constant-time compared, header or query).
func TestSetup_RemoteCallerWithToken(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	srv.cfg.Server.SetupToken = "bootstrap-token-123"
	h := srv.Handler()

	newReq := func(withToken bool) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]any{"username": "owner", "password": "a-very-strong-pass"})
		req := httptest.NewRequest("POST", "/api/setup", bytes.NewReader(body))
		req.RemoteAddr = "203.0.113.9:5000"
		if withToken {
			req.Header.Set("X-Setup-Token", "bootstrap-token-123")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w
	}

	if w := newReq(false); w.Code != http.StatusForbidden {
		t.Fatalf("remote setup without token: status = %d, want 403", w.Code)
	}
	if w := newReq(true); w.Code != http.StatusOK {
		t.Fatalf("remote setup with token: status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
}

// TestSetup_WrongTokenRejected: a near-miss token must not pass the
// constant-time compare.
func TestSetup_WrongTokenRejected(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	srv.cfg.Server.SetupToken = "bootstrap-token-123"
	h := srv.Handler()

	body, _ := json.Marshal(map[string]any{"username": "owner", "password": "a-very-strong-pass"})
	req := httptest.NewRequest("POST", "/api/setup", bytes.NewReader(body))
	req.RemoteAddr = "203.0.113.9:5000"
	req.Header.Set("X-Setup-Token", "bootstrap-token-124")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("remote setup with wrong token: status = %d, want 403", w.Code)
	}
}

// TestSetup_ForwardedLoopbackRejected (M-1): a proxy-resolved loopback client
// must NOT pass the gate. trusted_proxies lets an operator declare arbitrary
// proxies, and an append-style proxy lets any caller forge
// X-Forwarded-For: 127.0.0.1 into a loopback resolution - the gate only
// accepts direct connections or the token.
func TestSetup_ForwardedLoopbackRejected(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	h := srv.Handler()

	body, _ := json.Marshal(map[string]any{"username": "owner", "password": "a-very-strong-pass"})
	req := httptest.NewRequest("POST", "/api/setup", bytes.NewReader(body))
	req.RemoteAddr = "127.0.0.1:5000" // loopback peer (trusted proxy position)
	req.Header.Set("X-Forwarded-For", "127.0.0.1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("setup via forwarded loopback: status = %d, want 403", w.Code)
	}
}

// TestSetup_TokenInQueryRejected (M-2): the setup token is only accepted via
// the X-Setup-Token header - query-string credentials land in proxy access
// logs and Referer headers.
func TestSetup_TokenInQueryRejected(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	srv.cfg.Server.SetupToken = "bootstrap-token-123"
	h := srv.Handler()

	body, _ := json.Marshal(map[string]any{"username": "owner", "password": "a-very-strong-pass"})
	req := httptest.NewRequest("POST", "/api/setup?setup_token=bootstrap-token-123", bytes.NewReader(body))
	req.RemoteAddr = "203.0.113.9:5000"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("remote setup with query token: status = %d, want 403", w.Code)
	}
}
