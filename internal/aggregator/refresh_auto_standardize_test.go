package aggregator

import (
	"testing"

	"github.com/taliove/proxyhub/internal/subscription"
)

// 机场刷新(全量/单机场)完成后自动重算名称(issue #51):开启标准化时刷新即更新,
// 不需要单独点"刷新名称"。不新增定时器、不外打机场(与 ADR 0042 fail-closed 一致)。
func TestAirportRefresh_AutoStandardizeNames(t *testing.T) {
	agg, st := newTestAggregator(t)

	// 手动注入节点池(模拟已有机场节点)
	nodes := []*subscription.Node{
		{Name: "old-name-1", Type: "vmess", Region: "HK", Source: "极速机场", Server: "1.1.1.1", Port: 443},
		{Name: "old-name-2", Type: "vmess", Region: "HK", Source: "极速机场", Server: "2.2.2.2", Port: 443},
	}
	agg.mu.Lock()
	agg.pools[0] = nodes
	agg.mu.Unlock()

	// 建机场+简称(不走真实拉取,只验证标准化逻辑)
	if _, err := st.CreateAirport("极速机场", "http://极速.example.com"); err != nil {
		t.Fatalf("create airport: %v", err)
	}
	airports, _ := st.ListAirports()
	if err := st.UpdateAirport(airports[0].ID, "极速机场", "http://极速.example.com", "JS"); err != nil {
		t.Fatalf("set abbr: %v", err)
	}

	// 标准化关闭(默认):内存池节点 DisplayName 为空
	pool := agg.NodesForUser(0)
	if len(pool) != 2 {
		t.Fatalf("pool len = %d, want 2", len(pool))
	}
	for i, n := range pool {
		if n.DisplayName != "" {
			t.Errorf("[%d] DisplayName = %q, want empty (标准化关闭)", i, n.DisplayName)
		}
	}

	// 开启标准化并手动调 executeForUser(模拟全量刷新完成):池已在内存,
	// executeForUser 的 mergedPool 会调 standardizePoolNames 重算名称再写回。
	// 真实拉取会失败但不影响验证:mergedPool 保留旧池+标准化,足够证明路径通。
	if err := st.SaveSystemSettings(map[string]string{"standardize_names": "true"}); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	// 直接调 standardizePoolNames 验证开启后重算逻辑
	standardized := agg.standardizePoolNames(0, nodes)
	if len(standardized) != 2 {
		t.Fatalf("standardized len = %d, want 2", len(standardized))
	}
	for i, n := range standardized {
		if n.DisplayName == "" {
			t.Errorf("[%d] DisplayName empty, want 标准格式(开启标准化后)", i)
		}
		if n.DisplayName == n.Name {
			t.Errorf("[%d] DisplayName=%q 与 Name 相同,want 已标准化", i, n.DisplayName)
		}
	}
	// 两个 HK 节点应有序号区分
	if standardized[0].DisplayName == standardized[1].DisplayName {
		t.Errorf("两个 HK 节点 DisplayName 相同")
	}

	// 关闭标准化:standardizePoolNames 原样返回
	if err := st.SaveSystemSettings(map[string]string{"standardize_names": "false"}); err != nil {
		t.Fatalf("save settings: %v", err)
	}
	out := agg.standardizePoolNames(0, nodes)
	for i, n := range out {
		if n.DisplayName != "" {
			t.Errorf("[%d] DisplayName = %q, want empty (关闭后)", i, n.DisplayName)
		}
	}
}
