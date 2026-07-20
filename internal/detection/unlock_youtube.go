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

// YouTube Premium 解锁判定。
//
// 判定逻辑对齐社区久经验证的做法(lmc999/RegionRestrictionCheck):
// 先排除封锁态,再确认可用态,两者都失配则判为页面改版并保守报错(绝不误判为可用)。
//   - 命中 google.cn 重定向 -> 中国大陆封锁
//   - 命中"Premium is not available in your country" -> 封锁
//   - 排除封锁后仍含"YouTube Premium"品牌短语(且状态 200) -> 可用
//   - 以上全部失配 -> 报错(页面可能改版)
//
// 地区从响应体解析 gl(INNERTUBE_CONTEXT_GL / gl / countryCode),解析不到留空。
const (
	ytProbeURL        = "https://www.youtube.com/premium"
	ytBlockedMarker   = "Premium is not available in your country"
	ytCNMarker        = "www.google.cn"
	ytAvailableMarker = "YouTube Premium" // 用完整品牌短语而非裸 "Premium",降低软封锁/验证页误判
	ytMaxBody         = 2 << 20           // 2MB:premium 页较大,cap 防止读爆内存
)

// ytRegionRe 匹配 gl 地区码;仅两位值经 strings.ToUpper 归一化,键名本身大小写敏感。
var ytRegionRe = regexp.MustCompile(`"(?:INNERTUBE_CONTEXT_GL|gl|countryCode)"\s*:\s*"([A-Za-z]{2})"`)

func init() {
	RegisterUnlockChecker(KindYouTubePremium, checkYouTubePremium)
}

// checkYouTubePremium 经节点代理请求 premium 页并判定,填充完整 Result。
func checkYouTubePremium(ctx context.Context, client *http.Client, node *subscription.Node, target Target) Result {
	res := Result{NodeKey: node.NodeKey(), TargetName: target.Name}

	start := time.Now()
	status, body, err := youtubeFetch(ctx, client)
	res.Latency = int(time.Since(start).Milliseconds())
	if err != nil {
		res.Error = fmt.Sprintf("request failed: %v", err)
		return res
	}

	level, region, perr := parseYouTubePremium(status, body)
	if perr != nil {
		res.Error = perr.Error()
		return res
	}
	res.Level = level
	res.Region = region
	res.Available = level == LevelFull
	return res
}

// parseYouTubePremium 纯判定:输入状态码 + 正文,输出解锁级别与地区。
// 关键字全部失配时返回 error(保守路径),调用方据此报错而非误判为可用。
func parseYouTubePremium(status int, body string) (level, region string, err error) {
	if strings.Contains(body, ytCNMarker) {
		return LevelBlocked, "CN", nil
	}
	if strings.Contains(body, ytBlockedMarker) {
		return LevelBlocked, parseGLRegion(body), nil
	}
	if status == http.StatusOK && strings.Contains(body, ytAvailableMarker) {
		return LevelFull, parseGLRegion(body), nil
	}
	return "", "", fmt.Errorf("youtube premium markers not found (status=%d, page may have changed)", status)
}

// parseGLRegion 从响应体解析 gl 地区码,解析不到返回空串。
func parseGLRegion(body string) string {
	m := ytRegionRe.FindStringSubmatch(body)
	if len(m) < 2 {
		return ""
	}
	return strings.ToUpper(m[1])
}

// youtubeFetch 经给定 client(已配好代理与超时)GET premium 页,返回状态码与截断后的正文。
// 刻意与 disney 各自持有独立 fetch:并行开发下不共享包级泛化助手,规避合并期符号冲突。
func youtubeFetch(ctx context.Context, client *http.Client) (int, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ytProbeURL, nil)
	if err != nil {
		return 0, "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, ytMaxBody))
	if err != nil {
		return resp.StatusCode, "", err
	}
	return resp.StatusCode, string(data), nil
}
