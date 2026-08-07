package aggregator

import (
	"testing"

	"github.com/taliove/proxyhub/internal/subscription"
)

// standardizePoolNames 按属主生效设置决定是否重算名称(issue #51):
// 设置不存在时按默认值 false(不标准化);开启时调 Standardizer;关闭/依赖失败时原样返回。
func TestStandardizePoolNames(t *testing.T) {
	agg, st := newTestAggregator(t)

	nodes := []*subscription.Node{
		{Name: "old-HK-1", Type: "vmess", Region: "HK", Source: "极速机场", Server: "1.1.1.1", Port: 443},
		{Name: "old-HK-2", Type: "vmess", Region: "HK", Source: "极速机场", Server: "2.2.2.2", Port: 443},
	}

	// 建机场并给简称
	if _, err := st.CreateAirport("极速机场", "http://example.com"); err != nil {
		t.Fatalf("create airport: %v", err)
	}
	airports, _ := st.ListAirports()
	if err := st.UpdateAirport(airports[0].ID, "极速机场", "http://example.com", "JS"); err != nil {
		t.Fatalf("set abbr: %v", err)
	}

	// 设置不存在(默认关):原样返回,DisplayName 不填
	out := agg.standardizePoolNames(0, nodes)
	if len(out) != 2 {
		t.Fatalf("len = %d, want 2", len(out))
	}
	for i, n := range out {
		if n.DisplayName != "" {
			t.Errorf("[%d] DisplayName = %q, want empty (默认关闭)", i, n.DisplayName)
		}
		if n.Name != nodes[i].Name {
			t.Errorf("[%d] Name changed", i)
		}
	}

	// 开启标准化:DisplayName 填充为标准格式
	if err := st.SaveSystemSettings(map[string]string{"standardize_names": "true"}); err != nil {
		t.Fatalf("save settings: %v", err)
	}
	out = agg.standardizePoolNames(0, nodes)
	if len(out) != 2 {
		t.Fatalf("len = %d, want 2", len(out))
	}
	for i, n := range out {
		if n.DisplayName == "" {
			t.Errorf("[%d] DisplayName empty, want 标准格式", i)
		}
		// Name 不变,DisplayName 是新字段
		if n.Name != nodes[i].Name {
			t.Errorf("[%d] Name changed", i)
		}
	}
	// 两个 HK 节点应有序号
	if out[0].DisplayName == out[1].DisplayName {
		t.Errorf("两个 HK 节点 DisplayName 相同,want 序号区分")
	}

	// 关闭标准化:DisplayName 不填
	if err := st.SaveSystemSettings(map[string]string{"standardize_names": "false"}); err != nil {
		t.Fatalf("save settings: %v", err)
	}
	out = agg.standardizePoolNames(0, nodes)
	for i, n := range out {
		if n.DisplayName != "" {
			t.Errorf("[%d] DisplayName = %q, want empty (显式关闭)", i, n.DisplayName)
		}
	}
}
