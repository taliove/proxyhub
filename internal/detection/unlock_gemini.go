package detection

import (
	"context"
	"net/http"

	"github.com/taliove/proxyhub/internal/subscription"
)

// geminiProbeURL Gemini 站点根:未支持地区返回 403 + 区域不可用文案,支持地区返回 200。
// 用内部探测常量而非 target.URL,判定逻辑不受播种目标 URL 变动影响。
const geminiProbeURL = "https://gemini.google.com/"

// geminiBlockMarkers Gemini 未支持地区的正文标记(与 403 同时命中才判 blocked)。
// 覆盖 "not available in your country" 与 "aren't currently supported" 等文案;大小写不敏感。
var geminiBlockMarkers = []string{
	"available in your country",
	"not available in your",
	"currently supported in your",
	"not currently supported",
}

// classifyGemini 输入状态码+正文输出判定(纯函数,表驱动可测)。
func classifyGemini(status int, body string) unlockVerdict {
	return classifyByMarkers(status, body, geminiBlockMarkers)
}

// checkGemini 经节点 client 判定 Gemini 解锁状态,顺带 trace 出口国家码写 Region。
func checkGemini(ctx context.Context, client *http.Client, node *subscription.Node, target Target) Result {
	return runUnlockCheck(ctx, client, node, target, geminiProbeURL, cloudflareTraceURL, classifyGemini)
}

func init() {
	RegisterUnlockChecker(KindGemini, checkGemini)
}
