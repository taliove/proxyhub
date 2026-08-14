package server

import "strings"

// 订阅格式(术语见 CONTEXT.md「订阅格式」「UA 分流」)的取值规范化与 UA 判定。
//
// 规范值:
//   - formatClash:Clash YAML(模板引擎渲染)
//   - formatBase64:通用订阅(逐行分享链接整体 base64)
//
// formatV2RayAlias 是 base64 的永久别名(issue #121):存量订阅链接与
// 客户端配置里的 format=v2ray 必须继续可用,识别点统一收敛在
// normalizeSubscriptionFormat,内部一律用规范值传递。
const (
	formatClash      = "clash"
	formatBase64     = "base64"
	formatV2RayAlias = "v2ray"
)

// clashUATokens Clash 系 UA 名单(UA 分流,issue #122 / ADR 0049)。
// 子串匹配、不区分大小写(调用方传入前已 ToLower)。可维护常量表:
// 新增 Clash 系客户端在此追加;注意 "clash" 已覆盖 clash.meta /
// clash-verge / flclash / clashx 等全部 clash 前缀变体。
var clashUATokens = []string{
	"clash",
	"mihomo",
	"stash",
}

// normalizeSubscriptionFormat 规范化显式 format 参数:v2ray 别名折叠为
// 规范值 base64,其余取值原样返回(未知值由渲染层 default 分支按 clash
// 兜底,维持「非法值回退默认格式」的既有行为)。
func normalizeSubscriptionFormat(format string) string {
	if format == formatV2RayAlias {
		return formatBase64
	}
	return format
}

// subscriptionFormatForUA 按 UA 判定订阅格式(UA 分流,issue #122 / ADR 0049):
// 命中 Clash 系名单 → clash;其余一切(空 UA、浏览器、curl、未知客户端)
// → base64。默认方向是最小必要暴露:认不出的一律给信息最少的格式
// (YAML 内含的模板骨架/规则集/面板指纹不下发)。只做格式分流,不做封禁。
// ua 必须已 ToLower。
func subscriptionFormatForUA(ua string) string {
	for _, token := range clashUATokens {
		if strings.Contains(ua, token) {
			return formatClash
		}
	}
	return formatBase64
}
