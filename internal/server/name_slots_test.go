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
	if _, err := st.CreateNameSlotForUser(0, "🇭🇰 香港01", nodes[0].NodeKey(), false); err != nil {
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
	if _, err := st.CreateNameSlotForUser(0, "我的香港", nodes[1].NodeKey(), false); err != nil {
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
	slotID, err := st.CreateNameSlotForUser(0, "主节点-{emoji}{region}", hk.NodeKey(), false)
	if err != nil {
		t.Fatal(err)
	}

	out := srv.applyNameSlots([]*subscription.Node{hk}, 0)
	if out[0].DisplayName != "主节点-🇭🇰香港" {
		t.Fatalf("rendered = %q, want 主节点-🇭🇰香港", out[0].DisplayName)
	}

	// 转移到美国节点:同一槽位(ID 寻址)渲染出美国(槽位不变,文案跟随节点)
	if err := st.UpdateNameSlotForUser(0, slotID, "", us.NodeKey(), true); err != nil {
		t.Fatal(err)
	}
	out = srv.applyNameSlots([]*subscription.Node{us}, 0)
	if out[0].DisplayName != "主节点-🇺🇸美国" {
		t.Fatalf("after transfer rendered = %q, want 主节点-🇺🇸美国", out[0].DisplayName)
	}

	// 无变量的名字原样(不回读渲染依赖)
	if _, err := st.CreateNameSlotForUser(0, "固定名", hk.NodeKey(), true); err != nil {
		t.Fatal(err)
	}
	out = srv.applyNameSlots([]*subscription.Node{hk}, 0)
	if out[0].DisplayName != "固定名" {
		t.Errorf("literal slot name = %q, want 固定名", out[0].DisplayName)
	}
}

// TestApplyNameSlots_Index 槽位 {index}:渲染前缀({index} 之前的渲染结果)
// 相同的多个槽位按槽位名排序自动编号(01/02…),互不撞名;无同前缀者恒 01
func TestApplyNameSlots_Index(t *testing.T) {
	srv, st := newTestServer(t, nil)
	hk1 := &subscription.Node{Name: "甲", Server: "hk1.example.com", Port: 443, Source: "机场A", Region: "HK"}
	hk2 := &subscription.Node{Name: "乙", Server: "hk2.example.com", Port: 443, Source: "机场A", Region: "HK"}
	us1 := &subscription.Node{Name: "丙", Server: "us1.example.com", Port: 443, Source: "机场A", Region: "US"}

	// 两个前缀同为"主节点-香港-"的槽位({index} 后的后缀不参与前缀分组)
	// + 一个美国槽位(独占前缀,恒 01)
	if _, err := st.CreateNameSlotForUser(0, "主节点-{region}-{index}B", hk1.NodeKey(), false); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateNameSlotForUser(0, "主节点-{region}-{index}A", hk2.NodeKey(), false); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateNameSlotForUser(0, "美线-{region}-{index}", us1.NodeKey(), false); err != nil {
		t.Fatal(err)
	}

	out := srv.applyNameSlots([]*subscription.Node{hk1, hk2, us1}, 0)
	got := map[string]string{}
	for _, n := range out {
		got[n.Server] = n.DisplayName
	}
	// 同前缀组内按槽位名排序:"…{index}A" < "…{index}B" → A=01 B=02;美国独占 01
	if got["hk2.example.com"] != "主节点-香港-01A" {
		t.Errorf("hk2 = %q, want 主节点-香港-01A", got["hk2.example.com"])
	}
	if got["hk1.example.com"] != "主节点-香港-02B" {
		t.Errorf("hk1 = %q, want 主节点-香港-02B", got["hk1.example.com"])
	}
	if got["us1.example.com"] != "美线-美国-01" {
		t.Errorf("us1 = %q, want 美线-美国-01", got["us1.example.com"])
	}
}

// TestApplyNameSlots_IndexDuplicateTemplates issue #113 核心:同一含 {index}
// 模板可挂多个节点,按创建顺序(槽位 ID)从 01 自动编号;跨地区前缀各自成组;
// 改名不改变创建顺序编号;空槽不占号。
func TestApplyNameSlots_IndexDuplicateTemplates(t *testing.T) {
	srv, st := newTestServer(t, nil)
	hk1 := &subscription.Node{Name: "甲", Server: "hk1.example.com", Port: 443, Source: "机场A", Region: "HK"}
	hk2 := &subscription.Node{Name: "乙", Server: "hk2.example.com", Port: 443, Source: "机场A", Region: "HK"}
	us1 := &subscription.Node{Name: "丙", Server: "us1.example.com", Port: 443, Source: "机场A", Region: "US"}

	// 同模板三个槽位:hk1 → hk2 → 空槽(不占号) → us1(跨地区独立编号)
	const tmpl = "主节点-{region}-{index}"
	if _, err := st.CreateNameSlotForUser(0, tmpl, hk1.NodeKey(), false); err != nil {
		t.Fatalf("first slot: %v", err)
	}
	id2, err := st.CreateNameSlotForUser(0, tmpl, hk2.NodeKey(), false)
	if err != nil {
		t.Fatalf("duplicate template name must be allowed: %v", err)
	}
	if _, err := st.CreateNameSlotForUser(0, tmpl, "", false); err != nil {
		t.Fatalf("empty slot with same template must be allowed: %v", err)
	}
	if _, err := st.CreateNameSlotForUser(0, tmpl, us1.NodeKey(), false); err != nil {
		t.Fatalf("cross-region same template must be allowed: %v", err)
	}

	out := srv.applyNameSlots([]*subscription.Node{hk1, hk2, us1}, 0)
	got := map[string]string{}
	for _, n := range out {
		got[n.Server] = n.DisplayName
	}
	// 同前缀(香港)同模板:按创建顺序 01/02;跨地区(美国)独立从 01 编起
	if got["hk1.example.com"] != "主节点-香港-01" {
		t.Errorf("hk1 = %q, want 主节点-香港-01", got["hk1.example.com"])
	}
	if got["hk2.example.com"] != "主节点-香港-02" {
		t.Errorf("hk2 = %q, want 主节点-香港-02", got["hk2.example.com"])
	}
	if got["us1.example.com"] != "主节点-美国-01" {
		t.Errorf("us1 = %q, want 主节点-美国-01", got["us1.example.com"])
	}

	// 改名不改变创建顺序编号(ID 不变,编号不动)
	if err := st.UpdateNameSlotForUser(0, id2, "主力-{region}-{index}", "", false); err != nil {
		t.Fatal(err)
	}
	out = srv.applyNameSlots([]*subscription.Node{hk1, hk2, us1}, 0)
	got = map[string]string{}
	for _, n := range out {
		got[n.Server] = n.DisplayName
	}
	if got["hk1.example.com"] != "主节点-香港-01" {
		t.Errorf("after rename hk1 = %q, want 主节点-香港-01 (编号不漂移)", got["hk1.example.com"])
	}
	if got["hk2.example.com"] != "主力-香港-01" {
		t.Errorf("after rename hk2 = %q, want 主力-香港-01 (新模板独占前缀从 01 编起)", got["hk2.example.com"])
	}
}

func TestNamingChainOrder(t *testing.T) {
	srv, st := newTestServer(t, nil)
	node := &subscription.Node{
		Name: "机场原名", Server: "a.example.com", Port: 443, Source: "机场A", Region: "HK",
	}
	slotID, err := st.CreateNameSlotForUser(0, "槽位名", node.NodeKey(), false)
	if err != nil {
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
	if err := st.DeleteNameSlotForUser(0, slotID); err != nil {
		t.Fatal(err)
	}
	chain = srv.applyNameSlots(srv.applyStandardization(pool, true, "{region_code}-T{index}", 0), 0)
	if chain[0].DisplayName != "HK-T01" {
		t.Errorf("template fallback = %q, want HK-T01", chain[0].DisplayName)
	}
}
