package server

import (
	"testing"

	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

// TestApplyNameSlots 槽位层:命中节点浅拷贝覆盖 DisplayName,入参不被改;
// 无槽位原样返回(零回归)
func TestApplyNameSlots(t *testing.T) {
	srv, st := newTestServer(t, nil)
	nodes := []*subscription.Node{
		{Name: "原名A", Server: "a.example.com", Port: 443, Source: "机场A"},
		{Name: "原名B", Server: "b.example.com", Port: 443, Source: "机场A"},
	}
	if err := st.CreateNameSlotForUser(0, "🇭🇰 香港01", nodes[0].NodeKey(), false); err != nil {
		t.Fatal(err)
	}

	out := srv.applyNameSlots(nodes, 0)
	if out[0].DisplayName != "🇭🇰 香港01" {
		t.Errorf("slot node DisplayName = %q, want 槽位名", out[0].DisplayName)
	}
	if out[1].DisplayName != "" {
		t.Errorf("non-slot node DisplayName = %q, want empty", out[1].DisplayName)
	}
	if nodes[0].DisplayName != "" {
		t.Error("input node mutated (池共享指针污染)")
	}

	// 无槽位用户:原样返回(同一批指针)
	out2 := srv.applyNameSlots(nodes, 42)
	if out2[0] != nodes[0] {
		t.Error("no-slot user should get passthrough")
	}
}

// TestApplyStandardization_SkipsSlotNodes 标准化跳过槽位节点(ADR 0047):
// 槽位节点 DisplayName 不被模板重算,其余节点编号/顺序不受影响
func TestApplyStandardization_SkipsSlotNodes(t *testing.T) {
	srv, st := newTestServer(t, nil)
	nodes := []*subscription.Node{
		{Name: "甲", Server: "a.example.com", Port: 443, Source: "机场A", Region: "HK"},
		{Name: "乙", Server: "b.example.com", Port: 443, Source: "机场A", Region: "HK"},
		{Name: "丙", Server: "c.example.com", Port: 443, Source: "机场A", Region: "HK"},
	}
	// 乙被槽位接管
	if err := st.CreateNameSlotForUser(0, "我的香港", nodes[1].NodeKey(), false); err != nil {
		t.Fatal(err)
	}

	out := srv.applyStandardization(nodes, true, "{region_code}-{index}", 0)
	if len(out) != 3 {
		t.Fatalf("len = %d, want 3", len(out))
	}
	// 顺序保持:甲/乙/丙
	if out[0].DisplayName != "HK-01" {
		t.Errorf("甲 DisplayName = %q, want HK-01", out[0].DisplayName)
	}
	if out[1].DisplayName != "" {
		t.Errorf("乙(槽位节点)DisplayName = %q, want 未被标准化", out[1].DisplayName)
	}
	// 丙在乙被摘出后排第 2(编号只数参与标准化的节点)
	if out[2].DisplayName != "HK-02" {
		t.Errorf("丙 DisplayName = %q, want HK-02", out[2].DisplayName)
	}
}

// TestApplyNameSlots_TemplateVars 槽位名模板变量:含 "{" 的名字按当前挂载节点
// 渲染,转移节点后名字跟随新节点地区("主节点-{region}"场景)
func TestApplyNameSlots_TemplateVars(t *testing.T) {
	srv, st := newTestServer(t, nil)
	hk := &subscription.Node{Name: "原名", Server: "hk.example.com", Port: 443, Source: "机场A", Region: "HK"}
	us := &subscription.Node{Name: "US原", Server: "us.example.com", Port: 443, Source: "机场A", Region: "US"}
	if err := st.CreateNameSlotForUser(0, "主节点-{emoji}{region}", hk.NodeKey(), false); err != nil {
		t.Fatal(err)
	}

	out := srv.applyNameSlots([]*subscription.Node{hk}, 0)
	if out[0].DisplayName != "主节点-🇭🇰香港" {
		t.Fatalf("rendered = %q, want 主节点-🇭🇰香港", out[0].DisplayName)
	}

	// 转移到美国节点:同一槽位名渲染出美国(槽位不变,文案跟随节点)
	if err := st.UpdateNameSlotForUser(0, "主节点-{emoji}{region}", "", us.NodeKey(), true); err != nil {
		t.Fatal(err)
	}
	out = srv.applyNameSlots([]*subscription.Node{us}, 0)
	if out[0].DisplayName != "主节点-🇺🇸美国" {
		t.Fatalf("after transfer rendered = %q, want 主节点-🇺🇸美国", out[0].DisplayName)
	}

	// 无变量的名字原样(不回读渲染依赖)
	if err := st.CreateNameSlotForUser(0, "固定名", hk.NodeKey(), true); err != nil {
		t.Fatal(err)
	}
	out = srv.applyNameSlots([]*subscription.Node{hk}, 0)
	if out[0].DisplayName != "固定名" {
		t.Errorf("literal slot name = %q, want 固定名", out[0].DisplayName)
	}
}

// TestNamingChainOrder 命名链优先级:精选 alias > 槽位名 > 模板标准化 > 原名
func TestNamingChainOrder(t *testing.T) {
	srv, st := newTestServer(t, nil)
	node := &subscription.Node{
		Name: "机场原名", Server: "a.example.com", Port: 443, Source: "机场A", Region: "HK",
	}
	if err := st.CreateNameSlotForUser(0, "槽位名", node.NodeKey(), false); err != nil {
		t.Fatal(err)
	}
	pool := []*subscription.Node{node}

	// 模板层
	chain := srv.applyStandardization(pool, true, "{region_code}-T{index}", 0)
	// 槽位层(压过模板)
	chain = srv.applyNameSlots(chain, 0)
	if chain[0].DisplayName != "槽位名" {
		t.Fatalf("slot should beat template, got %q", chain[0].DisplayName)
	}
	// alias 层(压过槽位)
	chain = applyNodePickAliases(chain, []store.NodePick{{Key: node.NodeKey(), Alias: "端点名"}})
	if chain[0].DisplayName != "端点名" {
		t.Fatalf("alias should beat slot, got %q", chain[0].DisplayName)
	}
	// 无 alias 无槽位的节点:模板生效;标准化关闭:原名(生成器回退,此处 DisplayName 空)
	if err := st.DeleteNameSlotForUser(0, "槽位名"); err != nil {
		t.Fatal(err)
	}
	chain = srv.applyNameSlots(srv.applyStandardization(pool, true, "{region_code}-T{index}", 0), 0)
	if chain[0].DisplayName != "HK-T01" {
		t.Errorf("template fallback = %q, want HK-T01", chain[0].DisplayName)
	}
}
