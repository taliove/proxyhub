package subfilter

import (
	"reflect"
	"testing"

	"github.com/taliove/proxyhub/internal/subscription"
)

// condNodes 固定节点池:覆盖机场/地区/标签/名称的不同组合。
// fixture 一律 example.com + 全零 UUID(见 AGENTS.md 安全红线)。
func condNodes() []*subscription.Node {
	return []*subscription.Node{
		{Name: "HK-01", Region: "HK", Source: "极速机场", Server: "a.example.com", Port: 443},
		{Name: "HK-02", Region: "HK", Source: "备用机场", Server: "b.example.com", Port: 443},
		{Name: "JP-01", Region: "JP", Source: "极速机场", Server: "c.example.com", Port: 443},
		{Name: "US-01", Region: "US", Source: "备用机场", Server: "d.example.com", Port: 443},
	}
}

// condTags NodeKey -> 标签集,模拟 node_tags 表消费结果。
func condTags() map[string][]string {
	return map[string][]string{
		"a.example.com:443": {"nf-full", "stable-good", "fast"},
		"b.example.com:443": {"stable-fair"},
		"c.example.com:443": {"nf-full", "openai", "stable-good"},
		"d.example.com:443": {"nf-originals"},
	}
}

// names 抽取过滤结果的节点名(顺序保持),便于断言。
func names(nodes []*subscription.Node) []string {
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, n.Name)
	}
	return out
}

func TestFilter_TableDriven(t *testing.T) {
	tags := condTags()
	cases := []struct {
		name string
		cond Conditions
		want []string
	}{
		{
			name: "empty conditions returns all (zero regression)",
			cond: Conditions{},
			want: []string{"HK-01", "HK-02", "JP-01", "US-01"},
		},
		{
			name: "single airport OR",
			cond: Conditions{Airports: []string{"极速机场"}},
			want: []string{"HK-01", "JP-01"},
		},
		{
			name: "multiple airports OR",
			cond: Conditions{Airports: []string{"极速机场", "备用机场"}},
			want: []string{"HK-01", "HK-02", "JP-01", "US-01"},
		},
		{
			name: "single region",
			cond: Conditions{Regions: []string{"HK"}},
			want: []string{"HK-01", "HK-02"},
		},
		{
			name: "multiple regions OR",
			cond: Conditions{Regions: []string{"HK", "US"}},
			want: []string{"HK-01", "HK-02", "US-01"},
		},
		{
			name: "single tag",
			cond: Conditions{Tags: []string{"nf-full"}},
			want: []string{"HK-01", "JP-01"},
		},
		{
			name: "multiple tags AND (must have all)",
			cond: Conditions{Tags: []string{"nf-full", "openai"}},
			want: []string{"JP-01"},
		},
		{
			name: "tag with no node matching yields empty",
			cond: Conditions{Tags: []string{"disney-plus"}},
			want: []string{},
		},
		{
			name: "keyword case-insensitive substring",
			cond: Conditions{Keyword: "hk"},
			want: []string{"HK-01", "HK-02"},
		},
		{
			name: "keyword uppercase matches lowercase-free name",
			cond: Conditions{Keyword: "JP"},
			want: []string{"JP-01"},
		},
		{
			name: "combined region AND tag AND keyword",
			cond: Conditions{Regions: []string{"HK"}, Tags: []string{"stable-good"}, Keyword: "01"},
			want: []string{"HK-01"},
		},
		{
			name: "airport AND tag narrows across categories",
			cond: Conditions{Airports: []string{"极速机场"}, Tags: []string{"nf-full"}},
			want: []string{"HK-01", "JP-01"},
		},
		{
			name: "no match returns empty non-nil",
			cond: Conditions{Regions: []string{"SG"}},
			want: []string{},
		},
		{
			name: "whitespace-only keyword ignored (treated empty)",
			cond: Conditions{Keyword: "   "},
			want: []string{"HK-01", "HK-02", "JP-01", "US-01"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := names(Filter(condNodes(), tags, tc.cond))
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Filter() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestFilter_EmptyReturnsNonNil 空条件返回非 nil 空安全切片,并保留全部节点。
func TestFilter_EmptyReturnsNonNil(t *testing.T) {
	got := Filter(nil, nil, Conditions{})
	if got == nil {
		t.Fatal("Filter(nil,...) returned nil, want non-nil empty slice")
	}
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

// TestFilter_MissingTagsMap 标签 map 缺失该节点键时,tag 条件视为不命中(不 panic)。
func TestFilter_MissingTagsMap(t *testing.T) {
	nodes := []*subscription.Node{
		{Name: "x", Region: "HK", Source: "机场", Server: "x.example.com", Port: 443},
	}
	// 空 map:任何 tag 条件都不该命中
	got := Filter(nodes, map[string][]string{}, Conditions{Tags: []string{"nf-full"}})
	if len(got) != 0 {
		t.Errorf("expected no match with empty tags map, got %v", names(got))
	}
	// 无 tag 条件时仍返回节点
	got = Filter(nodes, map[string][]string{}, Conditions{Regions: []string{"HK"}})
	if len(got) != 1 {
		t.Errorf("expected 1 node without tag condition, got %v", names(got))
	}
}

func TestConditions_IsEmpty(t *testing.T) {
	cases := []struct {
		name string
		cond Conditions
		want bool
	}{
		{"all empty", Conditions{}, true},
		{"blank keyword only", Conditions{Keyword: "  "}, true},
		{"empty slices", Conditions{Airports: []string{}, Regions: []string{}, Tags: []string{}}, true},
		{"has airport", Conditions{Airports: []string{"x"}}, false},
		{"has region", Conditions{Regions: []string{"HK"}}, false},
		{"has tag", Conditions{Tags: []string{"fast"}}, false},
		{"has keyword", Conditions{Keyword: "hk"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.cond.IsEmpty(); got != tc.want {
				t.Errorf("IsEmpty() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestConditions_NeedsTags(t *testing.T) {
	if (Conditions{}).NeedsTags() {
		t.Error("empty conditions should not need tags")
	}
	if (Conditions{Regions: []string{"HK"}}).NeedsTags() {
		t.Error("region-only conditions should not need tags")
	}
	if !(Conditions{Tags: []string{"fast"}}).NeedsTags() {
		t.Error("tag conditions should need tags")
	}
}

func TestParse(t *testing.T) {
	// 空串 -> 空条件(旧端点无 conditions 列的默认)
	c, err := Parse("")
	if err != nil {
		t.Fatalf("Parse(\"\") error = %v", err)
	}
	if !c.IsEmpty() {
		t.Errorf("Parse(\"\") not empty: %+v", c)
	}

	// "{}" -> 空条件
	c, err = Parse("{}")
	if err != nil || !c.IsEmpty() {
		t.Errorf("Parse(\"{}\") = %+v, err=%v", c, err)
	}

	// 正常 JSON 往返
	raw := `{"airports":["极速机场"],"regions":["HK"],"tags":["nf-full"],"keyword":"01"}`
	c, err = Parse(raw)
	if err != nil {
		t.Fatalf("Parse valid error = %v", err)
	}
	want := Conditions{Airports: []string{"极速机场"}, Regions: []string{"HK"}, Tags: []string{"nf-full"}, Keyword: "01"}
	if !reflect.DeepEqual(c, want) {
		t.Errorf("Parse = %+v, want %+v", c, want)
	}

	// 非法 JSON -> 错误
	if _, err := Parse("{not json"); err == nil {
		t.Error("Parse(invalid) expected error, got nil")
	}
}
