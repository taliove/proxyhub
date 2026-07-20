package detection

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/taliove/proxyhub/internal/subscription"
)

// cloudflareTraceURL Cloudflare 通用 trace 端点:经节点 client 请求后解析 loc= 得到出口国家码。
// 各 AI 判定器复用它填充 Result.Region;trace 失败不影响主判定(见 runUnlockCheck)。
const cloudflareTraceURL = "https://www.cloudflare.com/cdn-cgi/trace"

// unlockUserAgent 探测请求统一 UA:部分平台对空 UA 直接 403,伪装成浏览器避免误判。
const unlockUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 " +
	"(KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

// probeBodyLimit 探测正文最大读取字节数:标记都在头部,截断足够且防超大响应拖垮检测。
const probeBodyLimit = 64 * 1024

// unlockVerdict 解锁判定三态:仅用于内部分类,再由 runUnlockCheck 映射到 Result。
type unlockVerdict int

const (
	// verdictInconclusive 无法判定(裸 403 / 5xx / 429 等异常):走 error 路径,绝不误判为 blocked。
	verdictInconclusive unlockVerdict = iota
	// verdictBlocked 明确被区域封锁(403 + 平台特定标记)。
	verdictBlocked
	// verdictAvailable 正常可用(2xx)。
	verdictAvailable
)

// classifyByMarkers 通用判定:403 且正文命中任一标记 => blocked;2xx => available;其余 => inconclusive。
// 标记大小写不敏感。裸 403(无标记)刻意归 inconclusive,避免把非区域原因的 403 误判为 blocked。
func classifyByMarkers(status int, body string, markers []string) unlockVerdict {
	if status == http.StatusForbidden && containsAnyFold(body, markers) {
		return verdictBlocked
	}
	if status >= 200 && status < 300 {
		return verdictAvailable
	}
	return verdictInconclusive
}

// containsAnyFold 大小写不敏感地判断 body 是否包含 markers 中任一子串。
func containsAnyFold(body string, markers []string) bool {
	lower := strings.ToLower(body)
	for _, m := range markers {
		if m != "" && strings.Contains(lower, strings.ToLower(m)) {
			return true
		}
	}
	return false
}

// runUnlockCheck AI 解锁判定通用流程:经 client 请求 probeURL,分类后组装 Result。
// available/blocked 顺带经同一 client 请求 traceURL 填 Region(失败留空);error 路径不触 trace。
func runUnlockCheck(
	ctx context.Context,
	client *http.Client,
	node *subscription.Node,
	target Target,
	probeURL, traceURL string,
	classify func(status int, body string) unlockVerdict,
) Result {
	res := Result{NodeKey: node.NodeKey(), TargetName: target.Name}

	start := time.Now()
	status, body, err := getProbe(ctx, client, probeURL)
	res.Latency = int(time.Since(start).Milliseconds())
	if err != nil {
		res.Error = fmt.Sprintf("request failed: %v", err)
		return res
	}

	switch classify(status, body) {
	case verdictAvailable:
		res.Available = true
		res.Level = LevelFull
		res.Region = fetchTraceLoc(ctx, client, traceURL)
	case verdictBlocked:
		res.Level = LevelBlocked
		res.Region = fetchTraceLoc(ctx, client, traceURL)
	default:
		res.Error = fmt.Sprintf("unexpected response: status %d", status)
	}
	return res
}

// getProbe 经 client 发 GET 请求,返回状态码与截断后的正文(标记匹配够用)。
func getProbe(ctx context.Context, client *http.Client, url string) (int, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("User-Agent", unlockUserAgent)

	resp, err := client.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, probeBodyLimit))
	if err != nil {
		return resp.StatusCode, "", err
	}
	return resp.StatusCode, string(data), nil
}

// fetchTraceLoc 经 client 请求 trace 端点并解析 loc=;任何失败(错误/非 200/无 loc)返回空,不影响主判定。
func fetchTraceLoc(ctx context.Context, client *http.Client, url string) string {
	status, body, err := getProbe(ctx, client, url)
	if err != nil || status != http.StatusOK {
		return ""
	}
	return parseTraceLoc(body)
}

// parseTraceLoc 从 Cloudflare trace 纯文本正文(每行 key=value)取 loc= 的值,缺失返回空。
func parseTraceLoc(body string) string {
	for _, line := range strings.Split(body, "\n") {
		if v, ok := strings.CutPrefix(line, "loc="); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
