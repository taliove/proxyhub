package filter

import (
	"testing"

	"github.com/taliove/proxyhub/internal/subscription"
)

func names(nodes []*subscription.Node) []string {
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, n.Name)
	}
	return out
}

func TestFilterByKeywords_DropsMatchingAirportNodes(t *testing.T) {
	nodes := []*subscription.Node{
		{Name: "香港-01", Source: "机场A"},
		{Name: "剩余流量：100GB", Source: "机场A"},
		{Name: "日本-02", Source: "机场B"},
		{Name: "官网 example.com", Source: "机场B"},
	}

	result := FilterByKeywords(nodes, []string{"剩余流量", "官网"})

	got := names(result)
	if len(got) != 2 {
		t.Fatalf("len(result) = %d, want 2 (got %v)", len(got), got)
	}
	for _, n := range result {
		if n.Name == "剩余流量：100GB" || n.Name == "官网 example.com" {
			t.Errorf("blacklisted node leaked: %s", n.Name)
		}
	}
}

func TestFilterByKeywords_CaseInsensitiveSubstring(t *testing.T) {
	nodes := []*subscription.Node{
		{Name: "Official Site", Source: "机场A"},
		{Name: "HK-Premium", Source: "机场A"},
	}

	// 关键词小写、节点名混合大小写，子串命中即剔除
	result := FilterByKeywords(nodes, []string{"official"})

	if len(result) != 1 || result[0].Name != "HK-Premium" {
		t.Fatalf("case-insensitive substring failed, got %v", names(result))
	}
}

func TestFilterByKeywords_SelfHostedExempt(t *testing.T) {
	nodes := []*subscription.Node{
		{Name: "官网备用", Source: subscription.SourceSelfHosted},
		{Name: "官网 airport", Source: "机场A"},
	}

	// 即使自建节点名命中关键词，也必须保留（FailBack 安全网）
	result := FilterByKeywords(nodes, []string{"官网"})

	if len(result) != 1 || result[0].Source != subscription.SourceSelfHosted {
		t.Fatalf("self-hosted node must be exempt, got %v", names(result))
	}
}

func TestFilterByKeywords_EmptyKeywordsPassthrough(t *testing.T) {
	nodes := []*subscription.Node{
		{Name: "官网", Source: "机场A"},
		{Name: "香港", Source: "机场A"},
	}

	// 空关键词 / 全为空白 → 原样返回
	if got := FilterByKeywords(nodes, nil); len(got) != 2 {
		t.Errorf("nil keywords: len = %d, want 2", len(got))
	}
	if got := FilterByKeywords(nodes, []string{"", "  "}); len(got) != 2 {
		t.Errorf("blank keywords: len = %d, want 2", len(got))
	}
}

func TestFilterByKeywords_DoesNotMutateInput(t *testing.T) {
	nodes := []*subscription.Node{
		{Name: "官网", Source: "机场A"},
		{Name: "香港", Source: "机场A"},
	}

	_ = FilterByKeywords(nodes, []string{"官网"})

	if len(nodes) != 2 {
		t.Errorf("input slice mutated, len = %d, want 2", len(nodes))
	}
}

func TestSplitKeywords(t *testing.T) {
	cases := []struct {
		raw  string
		want int
	}{
		{"", 0},
		{"官网", 1},
		{"官网,剩余流量", 2},
		{"官网\n剩余流量\n到期", 3},
		{"官网, 剩余流量\n 到期 ", 3},          // 混合分隔 + 空白
		{"官网,,剩余流量\n\n", 2},             // 空片段丢弃
	}
	for _, c := range cases {
		if got := SplitKeywords(c.raw); len(got) != c.want {
			t.Errorf("SplitKeywords(%q) = %v (len %d), want len %d", c.raw, got, len(got), c.want)
		}
	}
}
