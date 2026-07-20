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

// Disney+ 解锁判定。
//
// 区分正常落地页与区域限制跳转:
//   - 命中 unavailable 标记(区域限制页 / /unavailable 跳转) -> 封锁
//   - 排除封锁后命中落地页标记(且状态 200) -> 可用
//   - 两者都失配 -> 报错(页面可能改版),保守而非误判为可用
//
// 地区尽力从响应体解析(region / countryCode),解析不到留空。
const (
	disneyProbeURL = "https://www.disneyplus.com/"
	disneyMaxBody  = 2 << 20 // 2MB
)

// disneyUnavailableMarkers 区域限制标记(小写匹配,命中任一即封锁)。
var disneyUnavailableMarkers = []string{
	"not available in your region",
	"not available in your country",
	"/unavailable",
}

// disneyLandingMarkers 正常落地页标记(小写匹配,排除封锁后命中任一即可用)。
// 用带域的 canonical/og URL 作为落地信号,比裸品牌词更能与软封锁/验证页区分。
var disneyLandingMarkers = []string{
	"disneyplus.com",
}

// disneyRegionRe 尽力解析地区码;仅两位值经 strings.ToUpper 归一化,键名本身大小写敏感。
var disneyRegionRe = regexp.MustCompile(`"(?:region|countryCode)"\s*:\s*"([A-Za-z]{2})"`)

func init() {
	RegisterUnlockChecker(KindDisneyPlus, checkDisneyPlus)
}

// checkDisneyPlus 经节点代理请求 Disney+ 首页并判定,填充完整 Result。
func checkDisneyPlus(ctx context.Context, client *http.Client, node *subscription.Node, target Target) Result {
	res := Result{NodeKey: node.NodeKey(), TargetName: target.Name}

	start := time.Now()
	status, body, err := disneyFetch(ctx, client)
	res.Latency = int(time.Since(start).Milliseconds())
	if err != nil {
		res.Error = fmt.Sprintf("request failed: %v", err)
		return res
	}

	level, region, perr := parseDisneyPlus(status, body)
	if perr != nil {
		res.Error = perr.Error()
		return res
	}
	res.Level = level
	res.Region = region
	res.Available = level == LevelFull
	return res
}

// parseDisneyPlus 纯判定:输入状态码 + 正文,输出解锁级别与地区。
// 关键字全部失配时返回 error(保守路径),调用方据此报错而非误判为可用。
func parseDisneyPlus(status int, body string) (level, region string, err error) {
	lower := strings.ToLower(body)
	for _, m := range disneyUnavailableMarkers {
		if strings.Contains(lower, m) {
			return LevelBlocked, parseDisneyRegion(body), nil
		}
	}
	if status == http.StatusOK {
		for _, m := range disneyLandingMarkers {
			if strings.Contains(lower, m) {
				return LevelFull, parseDisneyRegion(body), nil
			}
		}
	}
	return "", "", fmt.Errorf("disney+ markers not found (status=%d, page may have changed)", status)
}

// parseDisneyRegion 尽力从响应体解析地区码,解析不到返回空串。
func parseDisneyRegion(body string) string {
	m := disneyRegionRe.FindStringSubmatch(body)
	if len(m) < 2 {
		return ""
	}
	return strings.ToUpper(m[1])
}

// disneyFetch 经给定 client(已配好代理与超时)GET Disney+ 首页,返回状态码与截断后的正文。
// 刻意与 youtube 各自持有独立 fetch:并行开发下不共享包级泛化助手,规避合并期符号冲突。
func disneyFetch(ctx context.Context, client *http.Client) (int, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, disneyProbeURL, nil)
	if err != nil {
		return 0, "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, disneyMaxBody))
	if err != nil {
		return resp.StatusCode, "", err
	}
	return resp.StatusCode, string(data), nil
}
