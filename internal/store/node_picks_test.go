package store

import (
	"strings"
	"testing"
)

// 精选 {key, alias} 对象形态(spec #84 / issue #85)的解析缝:
// 双格式兼容(旧字符串数组零迁移)、非法形状拒绝、别名边界归一。
// fixture 全合成(example.com)。

func TestParseNodePicks_LegacyStringArray(t *testing.T) {
	picks, err := ParseNodePicks(`["a.example.com:8388","b.example.com:443"]`)
	if err != nil {
		t.Fatalf("ParseNodePicks legacy: %v", err)
	}
	if len(picks) != 2 {
		t.Fatalf("len = %d, want 2", len(picks))
	}
	if picks[0].Key != "a.example.com:8388" || picks[0].Alias != "" {
		t.Errorf("picks[0] = %+v, want key only, empty alias", picks[0])
	}
	if picks[1].Key != "b.example.com:443" || picks[1].Alias != "" {
		t.Errorf("picks[1] = %+v, want key only, empty alias", picks[1])
	}
}

func TestParseNodePicks_ObjectForm(t *testing.T) {
	picks, err := ParseNodePicks(`[{"key":"a.example.com:8388","alias":"老爸的香港"}]`)
	if err != nil {
		t.Fatalf("ParseNodePicks object: %v", err)
	}
	if len(picks) != 1 || picks[0].Key != "a.example.com:8388" || picks[0].Alias != "老爸的香港" {
		t.Errorf("picks = %+v, want key+alias", picks)
	}

	// alias 缺省与留空等价(=无别名,跟随命名链)
	picks, err = ParseNodePicks(`[{"key":"a.example.com:8388"},{"key":"b.example.com:443","alias":""}]`)
	if err != nil {
		t.Fatalf("ParseNodePicks omitted alias: %v", err)
	}
	if len(picks) != 2 || picks[0].Alias != "" || picks[1].Alias != "" {
		t.Errorf("picks = %+v, want empty aliases", picks)
	}
}

func TestParseNodePicks_MixedForms(t *testing.T) {
	picks, err := ParseNodePicks(`[{"key":"a.example.com:8388","alias":"别名"},"b.example.com:443"]`)
	if err != nil {
		t.Fatalf("ParseNodePicks mixed: %v", err)
	}
	if len(picks) != 2 || picks[0].Alias != "别名" || picks[1].Key != "b.example.com:443" || picks[1].Alias != "" {
		t.Errorf("picks = %+v, want mixed forms parsed", picks)
	}
}

func TestParseNodePicks_EmptyAndInvalid(t *testing.T) {
	picks, err := ParseNodePicks("")
	if err != nil || picks != nil {
		t.Errorf("ParseNodePicks(\"\") = %v, %v; want nil, nil", picks, err)
	}

	// 非法 JSON / 非数组 / 非法元素形状(数字、布尔、嵌套数组、key 非字符串)一律 error,
	// 边界校验(validNodePicksJSON)据此拒绝落库。
	for _, bad := range []string{
		"{bad", `{"a":1}`, `"just-a-string"`, `[1]`, `[true]`, `[["a"]]`,
		`[{"key":123}]`, `["a.example.com:8388",1]`,
		`[{}]`, `[{"alias":"x"}]`, // 无 key 对象:与旧版 []string 报错 parity(降级哲学)
	} {
		if _, err := ParseNodePicks(bad); err == nil {
			t.Errorf("ParseNodePicks(%q) expected error, got nil", bad)
		}
	}

	// null 元素与旧版 []string 行为一致:解析为空 Key(永不命中,无害),不算非法。
	picks, err = ParseNodePicks(`[null]`)
	if err != nil || len(picks) != 1 || picks[0].Key != "" {
		t.Errorf("ParseNodePicks([null]) = %v, %v; want one empty-key pick", picks, err)
	}
}

func TestSanitizeNodePickAlias(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"  老爸的香港  ", "老爸的香港"},
		{"带\n换行\r控制\t符", "带换行控制符"},
		{strings.Repeat("长", 60), strings.Repeat("长", 50)}, // 50 rune 截断
	}
	for _, c := range cases {
		if got := SanitizeNodePickAlias(c.in); got != c.want {
			t.Errorf("SanitizeNodePickAlias(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// 新格式对象数组经边界校验落库并读回;旧格式同样仍被接受(双格式兼容)。
func TestUpdateEndpointNodePicks_ObjectFormRoundtrip(t *testing.T) {
	st := newTestStore(t)
	ep, _ := st.CreateEndpoint("测试")

	raw := `[{"key":"a.example.com:8388","alias":"别名"},{"key":"b.example.com:443"}]`
	if err := st.UpdateEndpointNodePicks(ep.ID, raw); err != nil {
		t.Fatalf("UpdateEndpointNodePicks object form: %v", err)
	}
	got, _ := st.GetEndpointByID(ep.ID)
	if got.NodePicks != raw {
		t.Errorf("NodePicks = %q, want %q", got.NodePicks, raw)
	}

	// 旧格式仍合法(存量零迁移)
	if err := st.UpdateEndpointNodePicks(ep.ID, `["a.example.com:8388"]`); err != nil {
		t.Fatalf("UpdateEndpointNodePicks legacy form: %v", err)
	}
}
