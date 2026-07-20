package store

import (
	"testing"
)

func TestRegionRecognizer(t *testing.T) {
	st := newTestStore(t)
	recognizer, err := st.NewRegionRecognizer()
	if err != nil {
		t.Fatalf("create recognizer: %v", err)
	}

	tests := []struct {
		name string
		want string
	}{
		{"🇭🇰 香港节点", "HK"},
		{"XX 香港", "HK"},
		{"HK-Premium-01", "HK"},
		{"Hong Kong Server", "HK"},
		{"新加坡高速", "SG"},
		{"🇸🇬 SG-01", "SG"},
		{"US Server 01", "US"},
		{"🇺🇸 美国节点", "US"},
		{"日本 Tokyo", "JP"},
		{"台湾节点", "TW"},
		{"印度孟买", "IN"},
		{"🇷🇺 莫斯科", "RU"},
		{"Netherlands 01", "NL"},
		{"土耳其伊斯坦布尔", "TR"},
		{"菲律宾马尼拉", "PH"},
		{"泰国曼谷", "TH"},
		{"阿根廷节点", "AR"},
		{"神秘节点X", "Unknown"},
		{"以色列节点", "Unknown"},
	}

	for _, tt := range tests {
		got := recognizer.Recognize(tt.name)
		if got != tt.want {
			t.Errorf("Recognize(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestListRegions(t *testing.T) {
	st := newTestStore(t)
	regions, err := st.ListRegions()
	if err != nil {
		t.Fatalf("list regions: %v", err)
	}
	if len(regions) == 0 {
		t.Fatal("no regions found")
	}

	// 应该至少包含常见地区
	codes := make(map[string]bool)
	for _, r := range regions {
		codes[r.Code] = true
	}

	for _, expected := range []string{"HK", "SG", "US", "JP", "TW"} {
		if !codes[expected] {
			t.Errorf("missing expected region: %s", expected)
		}
	}
}
