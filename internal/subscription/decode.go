package subscription

import (
	"encoding/base64"
	"strings"
)

// SanitizeWebPageURL 官网地址 scheme 白名单(仅 http:// / https://,不区分大小写);
// 非白名单(javascript:/data: 等)归一为空串。XSS 防线:该字段最终在详情抽屉
// 以 <a href> 渲染,拉取响应头与用户手填两条入库路径都必须过它。
func SanitizeWebPageURL(u string) string {
	lower := strings.ToLower(strings.TrimSpace(u))
	if strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "http://") {
		return strings.TrimSpace(u)
	}
	return ""
}


// DecodeSubscription 识别订阅内容的整体 base64 编码并解码(raw/padded 均支持);
// 解码失败按原文返回(明文多行分享链接形态)。机场面板标准导出是 base64 整段,
// 手动粘贴两种形态都可能出现,调用方无需区分。
//
// 三处共用:URL 拉取(fetcher)、机场测试诊断(airporttest)、手动机场粘贴导入(server)。
func DecodeSubscription(body []byte) string {
	content := strings.TrimSpace(string(body))
	if decoded, err := base64.RawStdEncoding.DecodeString(content); err == nil {
		return string(decoded)
	}
	if decoded, err := base64.StdEncoding.DecodeString(content); err == nil {
		return string(decoded)
	}
	return string(body)
}
