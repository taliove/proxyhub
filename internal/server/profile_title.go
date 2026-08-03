package server

import (
	"encoding/base64"
	"net/http"
	"strings"

	"github.com/taliove/proxyhub/internal/store"
)

// profileTitleBrand 订阅 profile 名称的品牌前缀(issue #38)。品牌串是常量,
// 不做全局配置项。
const profileTitleBrand = "ProxyHub"

// profileTitle 合成下发到订阅客户端的 profile 名称:端点设了公开名称时
// "ProxyHub · <public_name>",未设时裸品牌名。私有 alias 绝不下发。
func profileTitle(ep *store.Endpoint) string {
	if ep.PublicName == "" {
		return profileTitleBrand
	}
	return profileTitleBrand + " · " + ep.PublicName
}

// rfc5987Encode 按 RFC 5987 attr-char 白名单做 percent-encode(非白名单字节
// 一律 %XX 大写十六进制)。用于 Content-Disposition 的 filename* 取值。
func rfc5987Encode(s string) string {
	const hexUpper = "0123456789ABCDEF"
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9',
			c == '!', c == '#', c == '$', c == '&', c == '+', c == '-',
			c == '.', c == '^', c == '_', c == '`', c == '|', c == '~':
			b.WriteByte(c)
		default:
			b.WriteByte('%')
			b.WriteByte(hexUpper[c>>4])
			b.WriteByte(hexUpper[c&0x0F])
		}
	}
	return b.String()
}

// setSubscriptionProfileHeaders 给成功下发的订阅响应补 profile 命名头
// (issue #38):
//   - Profile-Title: base64(UTF-8 名称)(mihomo/Clash Verge/FlClash 系)
//   - Content-Disposition: attachment; filename*=UTF-8''<RFC 5987 编码名称>
//
// 两个头值都经过整体编码(base64 / 全量 percent-encode),CRLF 结构性进不去;
// 校验(trim/去控制字符/50 rune)在 store 边界,只是展示卫生。
// 只在守卫链通过后的成功路径调用;404/429/403 守卫路径绝不带头。
func setSubscriptionProfileHeaders(w http.ResponseWriter, ep *store.Endpoint) {
	title := profileTitle(ep)
	w.Header().Set("Profile-Title", base64.StdEncoding.EncodeToString([]byte(title)))
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+rfc5987Encode(title))
}
