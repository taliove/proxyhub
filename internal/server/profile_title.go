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
//   - Content-Disposition: inline; filename*=UTF-8''<RFC 5987 编码名称>
//     (inline:浏览器直接显示订阅内容而非强制下载,便于人工查看;
//     客户端不读 disposition,filename 在另存时仍生效)
//
// 两个头值都经过整体编码(base64 / 全量 percent-encode),CRLF 结构性进不去;
// 校验(trim/去控制字符/50 rune)在 store 边界,只是展示卫生。
// 只在守卫链通过后的成功路径调用;404/429/403 守卫路径绝不带头。
func setSubscriptionProfileHeaders(w http.ResponseWriter, ep *store.Endpoint) {
	title := profileTitle(ep)
	w.Header().Set("Profile-Title", base64.StdEncoding.EncodeToString([]byte(title)))
	w.Header().Set("Content-Disposition", "inline; filename*=UTF-8''"+rfc5987Encode(title))
}

// injectShadowrocketRemarks 小火箭的订阅命名通道(issue #39,QA 实测确认):
// 小火箭不读 Profile-Title/Content-Disposition,也不按规范剥离 URL fragment,
// 只认 base64(v2ray 格式)订阅明文开头的 REMARKS=<名称> 行。这里解开明文、
// 注入 REMARKS 行(与 Profile-Title 同一 profileTitle 合成规则)后整体重新编码。
//
// 名称校验(trim/去控制字符/50 rune)在 store 边界,REMARKS 行结构性不可能
// 携带 CRLF;解码失败理论不可达(GenerateV2Ray 输出恒为合法 base64),原样返回。
func injectShadowrocketRemarks(data []byte, ep *store.Endpoint) []byte {
	plain, err := base64.StdEncoding.DecodeString(string(data))
	if err != nil {
		return data
	}
	titled := "REMARKS=" + profileTitle(ep) + "\n" + string(plain)
	return []byte(base64.StdEncoding.EncodeToString([]byte(titled)))
}
