package detection

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/taliove/proxyhub/internal/subscription"
)

// init 注册 Netflix 专用解锁判定器(新增判定器只加新文件,不改共享代码)。
func init() {
	RegisterUnlockChecker(KindNetflix, netflixChecker)
}

// Netflix title ID 常量:判定逻辑用这两部片,而非 target.URL(后者仅展示元数据)。
// 片会随片库调整下架/变更授权,失效时直接换 ID 即可,无需改动判定逻辑。
const (
	// netflixNonOriginalTitle 一部非自制(许可)剧:仅在获得授权的地区可看,
	// 出口无授权时返回 404。可看 => full(全解锁)。
	netflixNonOriginalTitle = "81280792"
	// netflixOriginalTitle 一部 Netflix 自制剧:凡有 Netflix 服务的地区均可看。
	// 仅它可看 => originals_only(仅自制)。
	netflixOriginalTitle = "80018499"
)

// netflixTitleURL 拼装 title 页面 URL(内部常量驱动,不依赖 target.URL)。
func netflixTitleURL(titleID string) string {
	return "https://www.netflix.com/title/" + titleID
}

// nfPlay 单部影片在当前出口的可看状态(三级判定的中间量)。
type nfPlay int

const (
	nfPlayYes     nfPlay = iota // 可看(200)
	nfPlayNo                    // 该片在此地区不可看,但地区本身有 Netflix(404)
	nfPlayBlocked               // 整个地区被封 / 代理被识别(403 或 NSEZ-403)
	nfPlayUnknown               // 非预期状态码,无法判定
)

// netflixProxyDetected 是 Netflix "检测到代理/解锁器" 的稳定错误码,正文出现即视为地区被封。
const netflixProxyDetected = "NSEZ-403"

// classifyNetflixTitle 依据状态码 + 正文判定单部影片的可看状态。
// 小而不脆:主要看状态码,正文只用一个稳定错误码加固(代理被识别)。
func classifyNetflixTitle(status int, body string) nfPlay {
	if strings.Contains(body, netflixProxyDetected) {
		return nfPlayBlocked
	}
	switch status {
	case http.StatusOK:
		return nfPlayYes
	case http.StatusNotFound:
		return nfPlayNo
	case http.StatusForbidden:
		return nfPlayBlocked
	default:
		return nfPlayUnknown
	}
}

// decideNetflixLevel 由非自制/自制两片的可看状态聚合三级判定。
// 非自制可看 => full;仅自制可看 => originals_only;任一 403(地区级封锁)或两片皆 404 => blocked。
// 其余(含 nfPlayUnknown,即非预期状态码)返回空串:无法判定,由调用方按错误处理,
// 绝不把"测不准"伪装成"确认被封"。
func decideNetflixLevel(nonOriginal, original nfPlay) string {
	switch {
	case nonOriginal == nfPlayYes:
		return LevelFull
	case original == nfPlayYes:
		return LevelOriginalsOnly
	case nonOriginal == nfPlayBlocked || original == nfPlayBlocked:
		return LevelBlocked // 403 / 代理识别是地区级信号,单侧命中即判被封
	case nonOriginal == nfPlayNo && original == nfPlayNo:
		return LevelBlocked // 两片均 404:地区可达但无任何可看内容
	default:
		return "" // 含未知状态,信号不足以判定
	}
}

// netflixRegionPatterns 地区解析的有序候选模式(先命中先用),均要求两位大写国家码。
// 对应 Netflix 页面 reactContext 里的常见字段,失效时可增删模式而不动判定链路。
var netflixRegionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`"requestCountry":\{[^{}]*"id":"([A-Z]{2})"`),
	regexp.MustCompile(`"countryOfSignup":"([A-Z]{2})"`),
}

// parseNetflixRegion 尽力从正文解析出口国家码;解析不到返回空字符串(绝不报错,不拖垮判定)。
func parseNetflixRegion(body string) string {
	for _, re := range netflixRegionPatterns {
		if m := re.FindStringSubmatch(body); len(m) == 2 {
			return m[1]
		}
	}
	return ""
}

// netflixChecker 经节点代理请求非自制/自制两部片,聚合三级判定并尽力解析地区。
// 任一请求失败即判定失败:Available=false、Level 留空、Error 说明原因。
func netflixChecker(ctx context.Context, client *http.Client, node *subscription.Node, target Target) Result {
	result := Result{NodeKey: node.NodeKey(), TargetName: target.Name}
	start := time.Now()

	nonStatus, nonBody, err := fetchNetflixTitle(ctx, client, netflixNonOriginalTitle)
	if err != nil {
		// 传输失败:透传结构化 cause,文本 Error 保留原文案。
		result.Error = fmt.Sprintf("request non-original title failed: %v", err)
		result.cause = err
		return result
	}
	origStatus, origBody, err := fetchNetflixTitle(ctx, client, netflixOriginalTitle)
	if err != nil {
		result.Error = fmt.Sprintf("request original title failed: %v", err)
		result.cause = err
		return result
	}

	result.Latency = int(time.Since(start).Milliseconds())
	level := decideNetflixLevel(classifyNetflixTitle(nonStatus, nonBody), classifyNetflixTitle(origStatus, origBody))
	if level == "" {
		// 非预期响应,无法判定:写入 error 字段(见票据),不臆断 blocked。
		result.Error = fmt.Sprintf("netflix classification inconclusive (status %d/%d)", nonStatus, origStatus)
		return result
	}
	result.Level = level
	result.Available = level == LevelFull || level == LevelOriginalsOnly
	result.Region = parseNetflixRegion(nonBody)
	if result.Region == "" {
		result.Region = parseNetflixRegion(origBody)
	}
	return result
}

// netflixBodyLimit 读取正文的上限(仅需解析状态码与地区标记,无需整页)。
const netflixBodyLimit = 512 * 1024

// 浏览器化请求头:Netflix 对非浏览器 UA 常返回机器人挑战/拦截页,污染状态码判定。
// 用常见桌面 UA + Accept-Language 降低误判(仍是 best-effort,不保证绕过所有风控)。
const (
	netflixUserAgent      = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	netflixAcceptLanguage = "en-US,en;q=0.9"
)

// fetchNetflixTitle 经 client 请求单部片,返回状态码与(截断的)正文。
// 判定依赖 Netflix 对未授权片返回 404(而非 302 跳登录/首页);client 由 detector 注入,
// 沿用其默认重定向策略。浏览器化请求头用于减少被重定向到挑战页的概率。
func fetchNetflixTitle(ctx context.Context, client *http.Client, titleID string) (int, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, netflixTitleURL(titleID), nil)
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("User-Agent", netflixUserAgent)
	req.Header.Set("Accept-Language", netflixAcceptLanguage)
	resp, err := client.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, netflixBodyLimit))
	if err != nil {
		return 0, "", err
	}
	return resp.StatusCode, string(body), nil
}
