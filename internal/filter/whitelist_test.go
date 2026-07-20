package filter

import (
	"testing"

	"github.com/taliove/proxyhub/internal/subscription"
)

func TestFilterByWhitelist_KeepsOnlyMatchingAirportNodes(t *testing.T) {
	nodes := []*subscription.Node{
		{Name: "香港-01", Source: "机场A"},
		{Name: "日本-02", Source: "机场A"},
		{Name: "德国-03", Source: "机场B"},
		{Name: "新加坡-04", Source: "机场B"},
	}

	result := FilterByWhitelist(nodes, []string{"香港", "新加坡"})

	got := names(result)
	if len(got) != 2 {
		t.Fatalf("len(result) = %d, want 2 (got %v)", len(got), got)
	}
	for _, n := range result {
		if n.Name != "香港-01" && n.Name != "新加坡-04" {
			t.Errorf("non-whitelisted node leaked: %s", n.Name)
		}
	}
}

func TestFilterByWhitelist_EmptyPassthrough(t *testing.T) {
	nodes := []*subscription.Node{
		{Name: "香港", Source: "机场A"},
		{Name: "德国", Source: "机场A"},
	}

	// 空白名单 / 全空白 → 不启用，保留全部
	if got := FilterByWhitelist(nodes, nil); len(got) != 2 {
		t.Errorf("nil whitelist: len = %d, want 2 (passthrough)", len(got))
	}
	if got := FilterByWhitelist(nodes, []string{"", "  "}); len(got) != 2 {
		t.Errorf("blank whitelist: len = %d, want 2 (passthrough)", len(got))
	}
}

func TestFilterByWhitelist_CaseInsensitiveSubstring(t *testing.T) {
	nodes := []*subscription.Node{
		{Name: "HK-Premium", Source: "机场A"},
		{Name: "JP-Basic", Source: "机场A"},
	}

	result := FilterByWhitelist(nodes, []string{"hk"})

	if len(result) != 1 || result[0].Name != "HK-Premium" {
		t.Fatalf("case-insensitive substring failed, got %v", names(result))
	}
}

func TestFilterByWhitelist_SelfHostedExempt(t *testing.T) {
	nodes := []*subscription.Node{
		{Name: "自建美国", Source: subscription.SourceSelfHosted},
		{Name: "香港机场", Source: "机场A"},
		{Name: "德国机场", Source: "机场A"},
	}

	// 白名单只留"香港"，但自建节点即使不命中也必须保留（FailBack）
	result := FilterByWhitelist(nodes, []string{"香港"})

	got := names(result)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (香港机场 + 自建美国), got %v", len(got), got)
	}
	hasSelf := false
	for _, n := range result {
		if n.Source == subscription.SourceSelfHosted {
			hasSelf = true
		}
	}
	if !hasSelf {
		t.Error("self-hosted node must be exempt from whitelist")
	}
}

func TestFilterByWhitelist_DoesNotMutateInput(t *testing.T) {
	nodes := []*subscription.Node{
		{Name: "香港", Source: "机场A"},
		{Name: "德国", Source: "机场A"},
	}

	_ = FilterByWhitelist(nodes, []string{"香港"})

	if len(nodes) != 2 {
		t.Errorf("input slice mutated, len = %d, want 2", len(nodes))
	}
}
