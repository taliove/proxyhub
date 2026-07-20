package subscription

import "testing"

func TestGenerateAbbreviation(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		// 中文:剥离通用后缀后取拼音首字母(与 ADR 0012 示例一致)
		{"chinese with suffix", "极速机场", "JS"},
		{"chinese cloud suffix", "悠悠云", "YY"},
		{"chinese vpn suffix", "飞跃VPN", "FY"},
		{"chinese line suffix", "极速专线", "JS"},
		{"chinese no suffix", "极速", "JS"},
		// 纯拉丁:camelCase / 空格 / 分隔符切词,每词取首字母,不剥离后缀
		{"english camelcase", "FlowerCloud", "FC"},
		{"english cloudvpn", "CloudVPN", "CV"},
		{"english bestproxy", "BestProxy", "BP"},
		{"english single word", "bestproxy", "B"},
		{"english with spaces", "Best Proxy", "BP"},
		{"english hyphen", "fast-link", "FL"},
		// 长名截断到 maxAbbrLen(拼音首字母:香港中转优 → XGZZY,截断为 XGZZ)
		{"long chinese", "疾风迅雷闪电", "JFXL"},
		// 边界
		{"empty", "", ""},
		{"digits only", "123", "123"},
		{"symbols stripped", "★极速★", "JS"},
	}
	for _, tt := range tests {
		got := GenerateAbbreviation(tt.in)
		if got != tt.want {
			t.Errorf("GenerateAbbreviation(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// ADR 0012 旗舰示例:三个「极速*」机场都缩写成 JS,去重后为 JS/JS2/JS3。
func TestDeduplicateAbbreviations_ADRExample(t *testing.T) {
	got := DeduplicateAbbreviations([]string{"极速机场", "极速专线", "极速VPN"})
	want := map[string]string{"极速机场": "JS", "极速专线": "JS2", "极速VPN": "JS3"}
	for name, w := range want {
		if got[name] != w {
			t.Errorf("%s = %q, want %q", name, got[name], w)
		}
	}
}

func TestDeduplicateAbbreviations(t *testing.T) {
	conflict := DeduplicateAbbreviations([]string{"极速", "急速"})
	first := conflict["极速"]
	second := conflict["急速"]
	if first == second {
		t.Errorf("expected distinct abbreviations, both = %q", first)
	}
	if first != "JS" {
		t.Errorf("first should keep base JS, got %q", first)
	}
	if second != "JS2" {
		t.Errorf("second should be JS2, got %q", second)
	}
}

func TestDeduplicateAbbreviationsStable(t *testing.T) {
	// 同样输入,多次调用结果一致(不依赖 map 遍历顺序)
	names := []string{"急速", "极速", "疾速"}
	a := DeduplicateAbbreviations(names)
	b := DeduplicateAbbreviations(names)
	for _, n := range names {
		if a[n] != b[n] {
			t.Errorf("unstable result for %q: %q vs %q", n, a[n], b[n])
		}
	}
}

func TestNextFreeAbbr(t *testing.T) {
	used := map[string]bool{"JS": true, "JS2": true}
	if got := NextFreeAbbr("JS", used); got != "JS3" {
		t.Errorf("NextFreeAbbr = %q, want JS3", got)
	}
	// 空 base 退化为 X,不产生空简称
	if got := NextFreeAbbr("", map[string]bool{}); got != "X" {
		t.Errorf("NextFreeAbbr(empty) = %q, want X", got)
	}
}
