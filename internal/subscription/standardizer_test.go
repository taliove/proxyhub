package subscription

import "testing"

func testRegions() map[string]RegionInfo {
	return map[string]RegionInfo{
		"HK": {Code: "HK", Name: "香港", Emoji: "🇭🇰"},
		"SG": {Code: "SG", Name: "新加坡", Emoji: "🇸🇬"},
	}
}

func TestStandardizeSingleNode(t *testing.T) {
	std := NewStandardizer(
		"{emoji} {region} {source_abbr}-{index}",
		map[string]string{"极速机场": "JS"},
		testRegions(),
	)

	node := &Node{Name: "HK-01", Region: "HK", Source: "极速机场", Server: "1.2.3.4", Port: 443}
	got := std.Format(node, 1)
	want := "🇭🇰 香港 JS-01"
	if got != want {
		t.Errorf("Format() = %q, want %q", got, want)
	}
}

func TestStandardizeAllVariables(t *testing.T) {
	std := NewStandardizer(
		"{emoji}|{region}|{region_code}|{source}|{source_abbr}|{index}|{original_name}",
		map[string]string{"极速机场": "JS"},
		testRegions(),
	)
	node := &Node{Name: "原名", Region: "HK", Source: "极速机场"}
	got := std.Format(node, 7)
	want := "🇭🇰|香港|HK|极速机场|JS|07|原名"
	if got != want {
		t.Errorf("Format() = %q, want %q", got, want)
	}
}

func TestStandardizeUnknownRegion(t *testing.T) {
	std := NewStandardizer(
		"{emoji} {region} {source_abbr}-{index}",
		map[string]string{"某机场": "MJ"},
		testRegions(),
	)
	// 地区不在映射表中:emoji 用地球,region 使用去噪后的原名首段
	node := &Node{Name: "x", Region: "Unknown", Source: "某机场"}
	got := std.Format(node, 3)
	want := "🌐 x MJ-03"
	if got != want {
		t.Errorf("Format() = %q, want %q", got, want)
	}
}

// StandardizeNodes 把整池节点按 (机场, 地区) 分组,组内按 NodeKey 排序后编号,
// 返回带 DisplayName 的新节点切片(不修改入参)。
func TestStandardizeNodes(t *testing.T) {
	std := NewStandardizer(
		"{emoji} {region} {source_abbr}-{index}",
		map[string]string{"极速": "JS"},
		testRegions(),
	)

	nodes := []*Node{
		{Name: "b", Region: "HK", Source: "极速", Server: "5.5.5.5", Port: 443},
		{Name: "a", Region: "HK", Source: "极速", Server: "1.1.1.1", Port: 443},
		{Name: "c", Region: "SG", Source: "极速", Server: "9.9.9.9", Port: 443},
	}

	got := std.StandardizeNodes(nodes)

	// 入参不被修改(immutability)
	if nodes[0].DisplayName != "" {
		t.Errorf("input node was mutated: DisplayName=%q", nodes[0].DisplayName)
	}

	byServer := map[string]string{}
	for _, n := range got {
		byServer[n.Server] = n.DisplayName
	}

	// HK 组按 NodeKey(server:port) 排序:1.1.1.1 → 01, 5.5.5.5 → 02
	if byServer["1.1.1.1"] != "🇭🇰 香港 JS-01" {
		t.Errorf("1.1.1.1 = %q, want 🇭🇰 香港 JS-01", byServer["1.1.1.1"])
	}
	if byServer["5.5.5.5"] != "🇭🇰 香港 JS-02" {
		t.Errorf("5.5.5.5 = %q, want 🇭🇰 香港 JS-02", byServer["5.5.5.5"])
	}
	// SG 组独立编号,从 01 开始
	if byServer["9.9.9.9"] != "🇸🇬 新加坡 JS-01" {
		t.Errorf("9.9.9.9 = %q, want 🇸🇬 新加坡 JS-01", byServer["9.9.9.9"])
	}
}

// 自建节点现在也参与标准化(2026-07-16 改为注入 SELF 简称,与机场统一处理)。
func TestStandardizeNodes_SkipsSelfHosted(t *testing.T) {
	std := NewStandardizer(
		"{emoji} {region} {source_abbr}-{index}",
		map[string]string{SourceSelfHosted: "SELF"},
		testRegions(),
	)
	nodes := []*Node{
		{Name: "我的备用节点", Region: "HK", Source: SourceSelfHosted, Server: "1.1.1.1", Port: 443},
	}
	got := std.StandardizeNodes(nodes)
	if got[0].DisplayName != "🇭🇰 香港 SELF-01" {
		t.Errorf("self-hosted DisplayName = %q, want 🇭🇰 香港 SELF-01", got[0].DisplayName)
	}
	if got[0].EffectiveName() != "🇭🇰 香港 SELF-01" {
		t.Errorf("self-hosted EffectiveName = %q, want 🇭🇰 香港 SELF-01", got[0].EffectiveName())
	}
}

func TestRegionEmoji(t *testing.T) {
	tests := []struct {
		code string
		want string
	}{
		{"HK", "🇭🇰"},
		{"US", "🇺🇸"},
		{"JP", "🇯🇵"},
		{"Unknown", "🌐"},
		{"", "🌐"},
	}
	for _, tt := range tests {
		if got := RegionEmoji(tt.code); got != tt.want {
			t.Errorf("RegionEmoji(%q) = %q, want %q", tt.code, got, tt.want)
		}
	}
}

func TestExtractKeyName(t *testing.T) {
	cases := map[string]string{
		"✨专线-A1":           "专线",
		"🇺🇸 US Premium 01": "US Premium", // 去掉前导旗+空格,取到数字前
		"香港01":             "香港",
		"   ":              "", // 纯空白 → 空
		"🚀🚀🚀":              "", // 纯符号 → 空
		"Node_5":           "Node",
	}
	for in, want := range cases {
		if got := extractKeyName(in); got != want {
			t.Errorf("extractKeyName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFormat_UnknownRegionUsesExtractedName(t *testing.T) {
	s := NewStandardizer("{emoji} {region} {source_abbr}-{index}",
		map[string]string{"机场X": "JS"}, map[string]RegionInfo{})
	n := &Node{Name: "✨专线-A1", Region: "Unknown", Source: "机场X"}
	if got := s.Format(n, 1); got != "🌐 专线 JS-01" {
		t.Errorf("Format = %q, want 🌐 专线 JS-01", got)
	}
}

func TestFormat_UnknownEmptyExtractionFallsBackToAbbr(t *testing.T) {
	s := NewStandardizer("{emoji} {region} {source_abbr}-{index}",
		map[string]string{"机场X": "JS"}, map[string]RegionInfo{})
	n := &Node{Name: "🚀", Region: "Unknown", Source: "机场X"}
	if got := s.Format(n, 2); got != "🌐 JS JS-02" {
		t.Errorf("Format = %q, want 🌐 JS JS-02", got)
	}
}

func TestStandardizeNodes_SelfHostedNowTemplated(t *testing.T) {
	regions := map[string]RegionInfo{"JP": {Code: "JP", Name: "日本", Emoji: "🇯🇵"}}
	abbrs := map[string]string{SourceSelfHosted: "SELF"}
	s := NewStandardizer(DefaultNameTemplate, abbrs, regions)
	nodes := []*Node{
		{Name: "我的东京机", Region: "JP", Source: SourceSelfHosted, Server: "1.2.3.4", Port: 443},
	}
	out := s.StandardizeNodes(nodes)
	if out[0].DisplayName != "🇯🇵 日本 SELF-01" {
		t.Fatalf("DisplayName = %q, want 🇯🇵 日本 SELF-01", out[0].DisplayName)
	}
}
