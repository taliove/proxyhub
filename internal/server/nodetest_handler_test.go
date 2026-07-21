package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/taliove/proxyhub/internal/store"
)

func TestHandleTestNode_SelfNodeQuick(t *testing.T) {
	srv, st := newTestServer(t, nil)
	if err := st.CreateSelfHostedNode(&store.SelfHostedNode{
		Name: "自建A", Protocol: "vless", Server: "127.0.0.1", Port: 1, Enabled: true,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	ids, _ := st.ListAllSelfHostedNodes()
	body, _ := json.Marshal(map[string]any{"self_node_id": ids[0].ID, "mode": "quick"})

	req := httptest.NewRequest(http.MethodPost, "/api/nodes/test", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleTestNode(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var res map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if res["mode"] != "quick" {
		t.Errorf("mode = %v, want quick", res["mode"])
	}
	// 端口 1 不通,available 应为 false
	if res["available"] != false {
		t.Errorf("available = %v, want false", res["available"])
	}
}

func TestHandleTestNode_MissingTarget(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	body, _ := json.Marshal(map[string]any{"mode": "quick"}) // 既无 id 也无 key
	req := httptest.NewRequest(http.MethodPost, "/api/nodes/test", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleTestNode(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// 给了目标但解析不到:语义是"资源不存在",返回 404(与未给目标的 400 区分)。
func TestHandleTestNode_UnresolvableTarget(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	cases := map[string]map[string]any{
		"node_key 不存在":    {"node_key": "example.com:443", "mode": "quick"},
		"self_node_id 不存在": {"self_node_id": 99999, "mode": "quick"},
	}
	for name, payload := range cases {
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/api/nodes/test", bytes.NewReader(body))
		w := httptest.NewRecorder()
		srv.handleTestNode(w, req)
		if w.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", name, w.Code)
		}
	}
}
