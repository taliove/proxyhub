// Package sitepath 提供 Site Path 的规范化与校验。
//
// Site Path 是安装时生成的管理面路径前缀:配置后,管理 UI、API 与订阅端点
// 只挂在 /<site-path>/ 之下,其余路径一律返回普通 404。校验规则与安装器
// (scripts/install/lib.sh)保持一致,ReservedWords 为保留字清单的唯一事实来源。
package sitepath

import (
	"fmt"
	"strings"
)

// Site Path 长度边界(作用于规范化后的字符串)。
const (
	MinLength = 20
	MaxLength = 64
)

// ReservedWords 是不能用作 Site Path 的保留字清单(大小写不敏感)。
// 它们都是路由或静态资源命名空间,被占用会产生歧义。
// 该清单是服务端与安装器共用的唯一事实来源,新增条目需同步安装器。
var ReservedWords = []string{
	"admin",
	"api",
	"assets",
	"dist",
	"distribution",
	"favicon",
	"health",
	"healthz",
	"login",
	"proxyhub",
	"root",
	"setup",
	"sub",
	"subscription",
}

// Normalize 返回 Site Path 的规范形式:去除首尾空白与首尾斜杠。
func Normalize(path string) string {
	return strings.Trim(strings.TrimSpace(path), "/")
}

// Validate 校验 Site Path 是否符合安装器规则:
//
//   - 长度 20-64(规范化后)
//   - 字符集仅 [A-Za-z0-9_-]
//   - 4 类字符(小写/大写/数字/分隔符 - 与 _)至少命中 3 类
//   - 不得命中保留字清单(大小写不敏感,精确匹配)
//
// 通过返回 nil,否则返回描述性错误。
func Validate(path string) error {
	p := Normalize(path)

	if len(p) < MinLength || len(p) > MaxLength {
		return fmt.Errorf("site path must be %d-%d characters, got %d", MinLength, MaxLength, len(p))
	}

	var hasLower, hasUpper, hasDigit, hasSep bool
	for _, r := range p {
		switch {
		case r >= 'a' && r <= 'z':
			hasLower = true
		case r >= 'A' && r <= 'Z':
			hasUpper = true
		case r >= '0' && r <= '9':
			hasDigit = true
		case r == '-' || r == '_':
			hasSep = true
		default:
			return fmt.Errorf("site path contains invalid character %q (allowed: A-Z a-z 0-9 - _)", r)
		}
	}

	classes := 0
	for _, hit := range []bool{hasLower, hasUpper, hasDigit, hasSep} {
		if hit {
			classes++
		}
	}
	if classes < 3 {
		return fmt.Errorf("site path must use at least 3 of 4 character classes (lower/upper/digit/separator), got %d", classes)
	}

	if IsReserved(p) {
		return fmt.Errorf("site path %q is a reserved word", p)
	}
	return nil
}

// IsReserved 报告 word 是否命中保留字清单(大小写不敏感)。
func IsReserved(word string) bool {
	for _, w := range ReservedWords {
		if strings.EqualFold(w, word) {
			return true
		}
	}
	return false
}
