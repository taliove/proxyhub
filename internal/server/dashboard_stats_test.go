package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

// TestHandleDashboardStats_ExcludesStaleAndMergesSelfHosted 仪表盘统计口径与节点
// 管理页同源(issue #143):下架节点(Stale=true,即使残留 Available=true)不计入
// availableNodes/totalNodes;serve-time 自建节点(未经聚合入池)计入统计。
func TestHandleDashboardStats_ExcludesStaleAndMergesSelfHosted(t *testing.T) {
	// 池内:在架可用、在架不可用、下架但残留 Available=true、下架且不可用。
	// fixture 全零 UUID + example.com,绝不含真实凭证。
	pool := []*subscription.Node{
		{Name: "hk-01", Server: "a.example.com", Port: 443, Type: "vmess",
			UUID: "00000000-0000-0000-0000-000000000000", Source: "airport-a",
			Available: true, Latency: 100},
		{Name: "jp-01", Server: "b.example.com", Port: 443, Type: "vmess",
			UUID: "00000000-0000-0000-0000-000000000000", Source: "airport-a",
			Available: false},
		{Name: "us-01-stale", Server: "c.example.com", Port: 443, Type: "vmess",
			UUID: "00000000-0000-0000-0000-000000000000", Source: "airport-a",
			Available: true, Stale: true, Latency: 300},
		{Name: "sg-01-stale", Server: "d.example.com", Port: 443, Type: "vmess",
			UUID: "00000000-0000-0000-0000-000000000000", Source: "airport-a",
			Available: false, Stale: true},
	}
	srv, st := newTestServer(t, pool)

	// serve-time 自建节点:启用者(ToNode 默认 Available=true)应计入;禁用者不计入。
	if err := st.CreateSelfHostedNode(&store.SelfHostedNode{
		Name: "自建HK", Protocol: "vless", Server: "self.example.com", Port: 443,
		UUID: "00000000-0000-0000-0000-000000000000", Enabled: true,
	}); err != nil {
		t.Fatalf("CreateSelfHostedNode enabled: %v", err)
	}
	if err := st.CreateSelfHostedNode(&store.SelfHostedNode{
		Name: "自建US", Protocol: "vless", Server: "self-disabled.example.com", Port: 443,
		UUID: "00000000-0000-0000-0000-000000000000", Enabled: false,
	}); err != nil {
		t.Fatalf("CreateSelfHostedNode disabled: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/stats", nil)
	w := httptest.NewRecorder()
	srv.handleDashboardStats(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var stats struct {
		TotalNodes     int `json:"totalNodes"`
		AvailableNodes int `json:"availableNodes"`
		AvgLatency     int `json:"avgLatency"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &stats); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
	}

	// 在架 = 2 个机场节点 + 1 个启用的自建节点;下架 2 个与禁用自建不计入。
	if stats.TotalNodes != 3 {
		t.Errorf("totalNodes = %d, want 3 (2 active airport + 1 enabled self-hosted)", stats.TotalNodes)
	}
	// 可用 = hk-01 + 启用的自建节点(ToNode 默认 Available=true);jp-01 不可用,
	// stale 节点即使 Available=true 也不计入。
	if stats.AvailableNodes != 2 {
		t.Errorf("availableNodes = %d, want 2 (1 active airport + 1 self-hosted)", stats.AvailableNodes)
	}
	// 平均延迟只统计在架可用节点:(100 + 0) / 2,不含 stale 节点的 300。
	if stats.AvgLatency != 50 {
		t.Errorf("avgLatency = %d, want 50 (stale latency must not leak in)", stats.AvgLatency)
	}
}
