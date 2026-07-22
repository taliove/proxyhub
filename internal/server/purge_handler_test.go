package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/taliove/proxyhub/internal/aggregator"
	"github.com/taliove/proxyhub/internal/subscription"
)

// purgeFixtureNodes 一个机场节点 + 一个自建节点(fixture 一律 example.com)。
func purgeFixtureNodes() []*subscription.Node {
	return []*subscription.Node{
		{Name: "机场节点", Type: "ss", Server: "hk01.example.com", Port: 8388,
			Cipher: "aes-256-gcm", Password: "p1", Source: "机场A"},
		{Name: "自建节点", Type: "trojan", Server: "self.example.com", Port: 443,
			Password: "p2", TLS: true, Source: subscription.SourceSelfHosted},
	}
}

// TestPurgeAirportNodes_Success 验证接口双清委托:机场节点移除、自建豁免、返回移除数。
func TestPurgeAirportNodes_Success(t *testing.T) {
	srv, _ := newTestServer(t, purgeFixtureNodes())

	req := httptest.NewRequest(http.MethodPost, "/api/nodes/purge-airport", nil)
	w := httptest.NewRecorder()
	srv.handlePurgeAirportNodes(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var resp struct {
		Success bool `json:"success"`
		Removed int  `json:"removed"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !resp.Success || resp.Removed != 1 {
		t.Errorf("response = %+v, want success=true removed=1", resp)
	}

	// fake 池只剩自建节点
	fake := srv.nodes.(*fakeNodes)
	if len(fake.nodes) != 1 || fake.nodes[0].Source != subscription.SourceSelfHosted {
		t.Errorf("pool after purge = %+v, want only the self-hosted node", fake.nodes)
	}
}

// TestPurgeAirportNodes_Conflict 有刷新任务进行中时返回 409(拒绝而非等待)。
func TestPurgeAirportNodes_Conflict(t *testing.T) {
	srv, _ := newTestServer(t, purgeFixtureNodes())
	srv.nodes.(*fakeNodes).purgeErr = aggregator.ErrPurgeConflict

	req := httptest.NewRequest(http.MethodPost, "/api/nodes/purge-airport", nil)
	w := httptest.NewRecorder()
	srv.handlePurgeAirportNodes(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409, body = %s", w.Code, w.Body.String())
	}
}
