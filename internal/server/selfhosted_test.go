package server

import (
	"testing"

	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

func countSelfHosted(nodes []*subscription.Node) int {
	n := 0
	for _, node := range nodes {
		if node.Source == subscription.SourceSelfHosted {
			n++
		}
	}
	return n
}

// 刚新增的自建节点(尚未进入聚合池)应在订阅生成时即刻被合并进来,
// 无需等下一轮刷新——坐实需求②:订阅地址里要有自建节点。
func TestFilteredNodes_MergesFreshSelfHosted(t *testing.T) {
	// 节点池里没有任何自建节点(模拟刷新后新增的空档)
	srv, st := newTestServer(t, nil)
	if err := st.CreateSelfHostedNode(&store.SelfHostedNode{
		Name: "自建DMIT香港", Protocol: "vmess", Server: "hysteria.taliove.com", Port: 443, Enabled: true,
	}); err != nil {
		t.Fatalf("CreateSelfHostedNode: %v", err)
	}

	result := srv.filteredNodes(srv.nodes.Nodes(), 0)

	if countSelfHosted(result) != 1 {
		t.Fatalf("self-hosted count = %d, want 1 (should be merged at serve time)", countSelfHosted(result))
	}
}

// 禁用的自建节点不应被合并(ListSelfHostedNodes 只返回启用的)。
func TestMergeSelfHosted_SkipsDisabled(t *testing.T) {
	srv, st := newTestServer(t, nil)
	if err := st.CreateSelfHostedNode(&store.SelfHostedNode{
		Name: "已禁用", Protocol: "vmess", Server: "1.2.3.4", Port: 443, Enabled: false,
	}); err != nil {
		t.Fatalf("CreateSelfHostedNode: %v", err)
	}

	if got := countSelfHosted(srv.mergeSelfHosted(nil, 0)); got != 0 {
		t.Errorf("merged disabled self-hosted count = %d, want 0", got)
	}
}

// 池中已存在的自建节点不应被重复合并(按 NodeKey 去重),且不修改入参底层数组。
func TestMergeSelfHosted_DedupsExisting(t *testing.T) {
	srv, st := newTestServer(t, nil)
	if err := st.CreateSelfHostedNode(&store.SelfHostedNode{
		Name: "自建A", Protocol: "vmess", Server: "1.2.3.4", Port: 443, Enabled: true,
	}); err != nil {
		t.Fatalf("CreateSelfHostedNode: %v", err)
	}

	// 池中已有同一自建节点(相同 server:port → 相同 NodeKey)
	pool := []*subscription.Node{
		{Name: "自建A", Server: "1.2.3.4", Port: 443, Source: subscription.SourceSelfHosted, Available: true, Latency: 42},
	}
	result := srv.mergeSelfHosted(pool, 0)

	if countSelfHosted(result) != 1 {
		t.Errorf("self-hosted count = %d, want 1 (no duplicate)", countSelfHosted(result))
	}
	// 池中带真实健康状态的版本应被保留
	if result[0].Latency != 42 {
		t.Errorf("existing pool node (latency 42) should be preserved, got latency %d", result[0].Latency)
	}
}

// 编辑自建节点配置(如协议 vmess→vless)后,即使池中还留着旧版,
// serve-time 合并也应以库为准输出新配置,同时保留池中的真实健康状态。坐实 #1。
func TestMergeSelfHosted_DBConfigAuthoritative(t *testing.T) {
	srv, st := newTestServer(t, nil)
	if err := st.CreateSelfHostedNode(&store.SelfHostedNode{
		Name: "自建A", Protocol: "vless", Server: "1.2.3.4", Port: 443, Enabled: true,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	// 池中是旧的 vmess 版本(相同 server:port → 相同 NodeKey),带真实健康状态
	pool := []*subscription.Node{
		{Name: "自建A", Type: "vmess", Server: "1.2.3.4", Port: 443,
			Source: subscription.SourceSelfHosted, Available: true, Latency: 42},
	}
	out := srv.mergeSelfHosted(pool, 0)

	if countSelfHosted(out) != 1 {
		t.Fatalf("self-hosted count = %d, want 1", countSelfHosted(out))
	}
	if out[0].Type != "vless" {
		t.Errorf("Type = %q, want vless (DB config must win)", out[0].Type)
	}
	if out[0].Latency != 42 {
		t.Errorf("Latency = %d, want 42 (pool health must be preserved)", out[0].Latency)
	}
}

// 机场节点必须原样保留,不被合并逻辑丢弃。
func TestMergeSelfHosted_KeepsAirportNodes(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	pool := []*subscription.Node{
		{Name: "机场节点", Type: "vless", Server: "9.9.9.9", Port: 8443, Source: "机场X"},
	}
	out := srv.mergeSelfHosted(pool, 0)
	if len(out) != 1 || out[0].Source != "机场X" {
		t.Fatalf("airport node not preserved: %+v", out)
	}
}
