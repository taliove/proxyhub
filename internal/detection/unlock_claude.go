package detection

import (
	"context"
	"net/http"

	"github.com/taliove/proxyhub/internal/subscription"
)

// claudeProbeURL Claude 站点根:未支持地区经 Cloudflare 返回 403 + 区域不可用文案,支持地区返回 200。
// 用内部探测常量而非 target.URL,判定逻辑不受播种目标 URL 变动影响。
const claudeProbeURL = "https://claude.ai/"

// claudeBlockMarkers Claude 未支持地区的正文标记(与 403 同时命中才判 blocked)。
// 覆盖区域不可用文案与 app unavailable 两类;标记大小写不敏感,便于随文案调整。
var claudeBlockMarkers = []string{
	"not available in your",
	"app unavailable",
	"app-unavailable",
}

// classifyClaude 输入状态码+正文输出判定(纯函数,表驱动可测)。
func classifyClaude(status int, body string) unlockVerdict {
	return classifyByMarkers(status, body, claudeBlockMarkers)
}

// checkClaude 经节点 client 判定 Claude 解锁状态,顺带 trace 出口国家码写 Region。
func checkClaude(ctx context.Context, client *http.Client, node *subscription.Node, target Target) Result {
	return runUnlockCheck(ctx, client, node, target, claudeProbeURL, cloudflareTraceURL, classifyClaude)
}

func init() {
	RegisterUnlockChecker(KindClaude, checkClaude)
}
