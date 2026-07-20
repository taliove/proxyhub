package subscription

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/mozillazg/go-pinyin"
)

// maxAbbrLen 简称最长字符数,避免长机场名产出冗长简称(如 JSJCZZ)。
const maxAbbrLen = 4

// genericAbbrSuffixes 机场名里的通用后缀,生成简称前从含中文的名称尾部剥离,
// 让「极速机场」得到 JS 而非 JSJC。按长的在前匹配。纯拉丁名不剥离(见下)。
var genericAbbrSuffixes = []string{
	"加速器", "机场", "专线", "网络", "中转", "加速", "云", "机",
	"cloud", "airport", "vpn", "net",
}

// GenerateAbbreviation 从机场名称生成简称(见 ADR 0012)。
//
// 规则:
//   - 含中文的名称:先剥离通用后缀(机场/云/VPN...),再取各汉字拼音首字母
//     (极速机场 → 极速 → JS,飞跃VPN → 飞跃 → FY)
//   - 纯拉丁名称:按 camelCase / 空格 / 分隔符切词,每词取首字母(FlowerCloud → FC)
//   - 数字原样保留(123 → 123),其他符号作分隔符
//   - 结果超过 maxAbbrLen 截断
//
// 简称仅需稳定、可读;冲突由去重处理,用户也可在机场管理页手动覆盖。
func GenerateAbbreviation(name string) string {
	work := name
	if containsHan(name) {
		if stripped := stripGenericSuffixes(name); stripped != "" {
			work = stripped
		}
	}

	abbr := initials(work)
	if r := []rune(abbr); len(r) > maxAbbrLen {
		abbr = string(r[:maxAbbrLen])
	}
	return abbr
}

// initials 取名称的首字母缩写:汉字→拼音首字母,拉丁→词首字母,数字保留。
func initials(name string) string {
	var b strings.Builder

	pyArgs := pinyin.NewArgs()
	pyArgs.Style = pinyin.FirstLetter

	prevLower := false  // 上一个拉丁字母是小写
	atWordStart := true // 当前位置是一个词的开头(拉丁字母应被采纳)

	for _, r := range name {
		switch {
		case unicode.Is(unicode.Han, r):
			py := pinyin.Pinyin(string(r), pyArgs)
			if len(py) > 0 && len(py[0]) > 0 && py[0][0] != "" {
				b.WriteString(strings.ToUpper(py[0][0]))
			}
			prevLower = false
			atWordStart = true

		case isASCIILetter(r):
			isUpper := unicode.IsUpper(r)
			// 词首,或 camelCase 边界(小写后接大写)才采纳
			if atWordStart || (isUpper && prevLower) {
				b.WriteRune(unicode.ToUpper(r))
			}
			prevLower = !isUpper
			atWordStart = false

		case unicode.IsDigit(r):
			b.WriteRune(r)
			prevLower = false
			atWordStart = true // 数字结束一个词

		default:
			// 分隔符 / 符号:切词
			prevLower = false
			atWordStart = true
		}
	}

	return b.String()
}

// stripGenericSuffixes 反复剥离名称尾部的通用后缀(不区分大小写)。
func stripGenericSuffixes(s string) string {
	for changed := true; changed; {
		changed = false
		for _, suf := range genericAbbrSuffixes {
			if len(s) >= len(suf) && strings.EqualFold(s[len(s)-len(suf):], suf) {
				s = s[:len(s)-len(suf)]
				changed = true
				break
			}
		}
	}
	return s
}

// containsHan 判断名称是否包含汉字。
func containsHan(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

// isASCIILetter 判断是否为 ASCII 字母(a-z / A-Z)。
func isASCIILetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

// NextFreeAbbr 返回不与 used 冲突的简称:base 可用则用它,否则依次加数字后缀
// (JS → JS2 → JS3)。base 为空时退化为 "X",避免产生空简称。
// 供机场简称去重复用(内存去重与含手动简称的库内去重共享同一逻辑,见 ADR 0012)。
func NextFreeAbbr(base string, used map[string]bool) string {
	if base == "" {
		base = "X"
	}
	abbr := base
	for i := 2; used[abbr]; i++ {
		abbr = fmt.Sprintf("%s%d", base, i)
	}
	return abbr
}

// DeduplicateAbbreviations 为一组机场名生成互不冲突的简称。
//
// 按输入顺序处理:首个占用基础简称(JS),后续冲突者依次追加序号(JS2、JS3)。
// 返回 机场名 → 去重后简称 的映射。输入顺序决定分配结果,故结果稳定。
func DeduplicateAbbreviations(names []string) map[string]string {
	result := make(map[string]string, len(names))
	used := make(map[string]bool)

	for _, name := range names {
		abbr := NextFreeAbbr(GenerateAbbreviation(name), used)
		used[abbr] = true
		result[name] = abbr
	}

	return result
}
