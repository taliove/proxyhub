package sitepath

import (
	"strings"
	"testing"
)

func TestNormalize(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"abc", "abc"},
		{"/abc", "abc"},
		{"abc/", "abc"},
		{"/abc/", "abc"},
		{"///abc//", "abc"},
		{"  abc  ", "abc"},
		{" /abc/ ", "abc"},
		{"", ""},
		{"/", ""},
		{"  ", ""},
	}
	for _, c := range cases {
		if got := Normalize(c.in); got != c.want {
			t.Errorf("Normalize(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestValidate(t *testing.T) {
	valid20 := "X9k-Qm_2Tz7pLw4Nc8Vb" // 20 chars, 4 classes
	if len(valid20) != 20 {
		t.Fatalf("test fixture length = %d, want 20", len(valid20))
	}

	cases := []struct {
		name    string
		in      string
		wantErr string // 空串表示期望通过;否则为错误信息应包含的子串
	}{
		// 合法:20/64 边界、3 类与 4 类组合
		{"min length 4 classes", valid20, ""},
		{"max length", strings.Repeat("aB1-", 16), ""}, // 64 chars
		{"lower upper digit", "abcdeABCDE12345abcde", ""},
		{"lower digit separator", "abcdefghij-123456789", ""},
		{"upper digit separator", "ABCDEFGHIJ_123456789", ""},
		{"lower upper separator", "abcdefghij-ABCDEFGHIJ", ""},
		{"leading trailing slashes tolerated", "/" + valid20 + "/", ""},

		// 长度
		{"empty", "", "20-64"},
		{"too short", "aB1-cd5678901234567", "20-64"}, // 19 chars
		{"too long", strings.Repeat("aB1-", 16) + "x", "20-64"},

		// 字符集
		{"invalid char bang", "abcdeABCDE12345ab!de", "invalid character"},
		{"invalid char dot", "abcdeABCDE12345ab.de", "invalid character"},
		{"invalid char slash inside", "abcdeABCDE12345ab/de", "invalid character"},
		{"invalid char space", "abcdeABCDE12345ab de", "invalid character"},
		{"invalid char unicode", "abcdeABCDE12345ab中de", "invalid character"},

		// 字符类别不足(需 4 类中至少 3 类)
		{"one class lower only", "aaaaaaaaaaaaaaaaaaaa", "3 of 4"},
		{"two classes lower digit", "abcdefghij1234567890", "3 of 4"},
		{"two classes lower separator", "abcdefghij-klmnopqrst", "3 of 4"},
		{"two classes upper digit", "ABCDEFGHIJ1234567890", "3 of 4"},

		// 保留字:恰好等于保留字的串必然短于 20,先被长度规则拒绝;
		// 仅包含保留字前缀的 20+ 字符串不受影响(精确匹配语义)
		{"reserved prefix is allowed", "adminX9k-Qm_2Tz7pLw4", ""},
	}
	for _, c := range cases {
		err := Validate(c.in)
		if c.wantErr == "" {
			if err != nil {
				t.Errorf("%s: Validate(%q) error = %v, want nil", c.name, c.in, err)
			}
			continue
		}
		if err == nil {
			t.Errorf("%s: Validate(%q) = nil, want error containing %q", c.name, c.in, c.wantErr)
			continue
		}
		if !strings.Contains(err.Error(), c.wantErr) {
			t.Errorf("%s: Validate(%q) error = %q, want containing %q", c.name, c.in, err.Error(), c.wantErr)
		}
	}
}

func TestIsReserved(t *testing.T) {
	// 清单内:大小写不敏感
	for _, w := range ReservedWords {
		if !IsReserved(w) {
			t.Errorf("IsReserved(%q) = false, want true", w)
		}
		if !IsReserved(strings.ToUpper(w)) {
			t.Errorf("IsReserved(%q) = false, want true (case-insensitive)", strings.ToUpper(w))
		}
	}
	// 清单外
	for _, w := range []string{"", "x", "admins", "api2", "X9k-Qm_2Tz7pLw4Nc8Vb"} {
		if IsReserved(w) {
			t.Errorf("IsReserved(%q) = true, want false", w)
		}
	}
}

func TestReservedWordsContents(t *testing.T) {
	// 与安装器(scripts/install/lib.sh)约定一致的完整清单,顺序无关
	want := map[string]bool{
		"admin": true, "api": true, "assets": true, "dist": true,
		"distribution": true, "favicon": true, "health": true, "healthz": true,
		"login": true, "proxyhub": true, "root": true, "setup": true,
		"sub": true, "subscription": true,
	}
	if len(ReservedWords) != len(want) {
		t.Fatalf("ReservedWords has %d entries, want %d", len(ReservedWords), len(want))
	}
	for _, w := range ReservedWords {
		if !want[w] {
			t.Errorf("unexpected reserved word %q", w)
		}
		if w != strings.ToLower(w) {
			t.Errorf("reserved word %q must be lowercase (single source of truth)", w)
		}
	}
}
