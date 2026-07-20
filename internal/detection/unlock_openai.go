package detection

import (
	"context"
	"net/http"

	"github.com/taliove/proxyhub/internal/subscription"
)

// openAIProbeURL OpenAI 合规检查端点:未支持地区返回 403 + unsupported_country,支持地区返回 200 cookie 配置。
// 用内部探测常量而非 target.URL,判定逻辑不受播种目标 URL 变动影响。
const openAIProbeURL = "https://api.openai.com/compliance/cookie_requirements"

// openAIBlockMarkers OpenAI 未支持地区的正文标记(与 403 同时命中才判 blocked)。
var openAIBlockMarkers = []string{"unsupported_country"}

// classifyOpenAI 输入状态码+正文输出判定(纯函数,表驱动可测)。
func classifyOpenAI(status int, body string) unlockVerdict {
	return classifyByMarkers(status, body, openAIBlockMarkers)
}

// checkOpenAI 经节点 client 判定 OpenAI 解锁状态,顺带 trace 出口国家码写 Region。
func checkOpenAI(ctx context.Context, client *http.Client, node *subscription.Node, target Target) Result {
	return runUnlockCheck(ctx, client, node, target, openAIProbeURL, cloudflareTraceURL, classifyOpenAI)
}

func init() {
	RegisterUnlockChecker(KindOpenAI, checkOpenAI)
}
