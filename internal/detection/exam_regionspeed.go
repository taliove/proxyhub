package detection

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"time"

	"github.com/taliove/proxyhub/internal/subscription"
)

// 多地域测速段默认参数。每区下载一个约 5s 的切片测下行速率,单区硬超时防卡死。
const (
	regionSliceDurationSec = 5 // 每区下载切片时长(秒)
	regionHardTimeoutSec   = 8 // 单区硬超时上限(秒):含建连 + TTFB + 切片,防链路卡死
	// examRegionMaxRetries 单区(含基准)测速失败后的最大重试次数。
	// 高延迟链路(4s+)上单次探测易偶发超时,多区一半被误判为失败;失败后自动重试这么多次,
	// 仍失败才标 error(重试期间不 emit 中间失败态,见 withRegionRetry)。区域探测无判定结论,
	// 任一失败(Error 非空)皆重试。改此值即调整重试力度。
	examRegionMaxRetries = 1
)

// baselineRegion 基准对照行(多地域段第一行):经节点打 Cloudflare 就近 POP(speed.cloudflare.com/__down),
// 与 8 个区域行同一探针形态(TTFB + 下行切片),等价于 bench.sh 里 Speedtest.net 自动选最近服务器的对照。
// 它就是一个特殊区域行,复用区域探针实现,不另起测量路径。URL 复用 bandwidth 段下行首选点(唯一事实源)。
var baselineRegion = Region{
	Code: "baseline",
	Name: "基准",
	URL:  downloadFallbackURLs[0], // Cloudflare 100MB 下行点(与 bandwidth 段一致)
}

// examRegionsWithBaseline 多地域段实际测量序列:基准对照行在前,其后为 8 个固定区域。
// 返回新切片,不修改 examRegions(不可变)。
func examRegionsWithBaseline() []Region {
	out := make([]Region, 0, len(examRegions)+1)
	out = append(out, baselineRegion)
	out = append(out, examRegions...)
	return out
}

// withRegionRetry 包裹区域探针,叠加单区(含基准)失败重试:任一探测失败(Error 非空)自动重试
// 至多 examRegionMaxRetries 次,仅返回最后一次结果(重试期间不 emit 中间失败态);ctx 取消则不再重试。
// 重试在单次探针调用内同步完成,对 runRegionSpeedSampler 透明,不破坏串行独占与单区硬超时
//(每次 attempt 由 measureRegionSpeed 各自套用 hard 超时)。
func withRegionRetry(probe RegionSpeedProbe) RegionSpeedProbe {
	return func(ctx context.Context, r Region) RegionResult {
		return retryResult(ctx, examRegionMaxRetries,
			func() RegionResult { return probe(ctx, r) },
			func(res RegionResult) bool { return res.Error != "" },
		)
	}
}

// Region 一个固定测速区域。URL 指向该区域 Linode 数据中心的测速文件。
type Region struct {
	Code string // 区域代码(稳定标识,前端 key / 展示用)
	Name string // 中文展示名
	URL  string // 该区 Linode DC 测速文件 URL(下行切片来源)
}

// examRegions 8 个固定测速区域(Linode 各数据中心测速文件)。
// 命名约定 https://speedtest.<dc>.linode.com/100MB-<dc>.bin(与 bandwidth_stream 回退点一致)。
// 注意:各 DC 子域随 Linode 机房调整会变化,具体域名以 Linode 实测为准,此表按当前实测填写。
var examRegions = []Region{
	// 美西 - Fremont, CA
	{Code: "us_west", Name: "美西", URL: "https://speedtest.fremont.linode.com/100MB-fremont.bin"},
	// 美东 - Newark, NJ
	{Code: "us_east", Name: "美东", URL: "https://speedtest.newark.linode.com/100MB-newark.bin"},
	// 欧洲 - Frankfurt, DE
	{Code: "eu_frankfurt", Name: "法兰克福", URL: "https://speedtest.frankfurt.linode.com/100MB-frankfurt.bin"},
	// 东南亚 - Singapore
	{Code: "sg", Name: "新加坡", URL: "https://speedtest.singapore.linode.com/100MB-singapore.bin"},
	// 东亚 - Tokyo 2, JP
	{Code: "jp_tokyo", Name: "东京", URL: "https://speedtest.tokyo2.linode.com/100MB-tokyo2.bin"},
	// 大洋洲 - Sydney, AU
	{Code: "au_sydney", Name: "悉尼", URL: "https://speedtest.syd1.linode.com/100MB-syd1.bin"},
	// 北美 - Toronto, CA
	{Code: "ca_toronto", Name: "多伦多", URL: "https://speedtest.toronto1.linode.com/100MB-toronto1.bin"},
	// 南亚 - Mumbai, IN
	{Code: "in_mumbai", Name: "孟买", URL: "https://speedtest.mumbai1.linode.com/100MB-mumbai1.bin"},
}

// RegionResult 单区测速结果:成功时含 TTFB 与下行速率,失败时仅 Error 非空。
type RegionResult struct {
	Code     string  `json:"code"`
	Name     string  `json:"name"`
	TTFBms   int     `json:"ttfb_ms"`
	DownMbps float64 `json:"down_mbps"`
	Error    string  `json:"error,omitempty"`
}

// RegionSpeedMetrics 多地域测速段聚合结果(逐区一行)。
type RegionSpeedMetrics struct {
	Regions []RegionResult `json:"regions"`
}

// RegionSpeedProbe 测量单区的 TTFB 与下行速率(单区失败经 RegionResult.Error 返回,不影响其余区)。
type RegionSpeedProbe func(ctx context.Context, r Region) RegionResult

// runRegionSpeedSampler 串行遍历各区(任一时刻只有一路测量),逐区回调 onResult。
// 单区失败(probe 结果带 Error)不中断整段;ctx 取消则提前返回已测区。
func runRegionSpeedSampler(
	ctx context.Context,
	regions []Region,
	probe RegionSpeedProbe,
	onResult func(RegionResult),
) []RegionResult {
	results := make([]RegionResult, 0, len(regions))
	for _, r := range regions {
		if ctx.Err() != nil {
			return results
		}
		res := probe(ctx, r)
		results = append(results, res)
		if onResult != nil {
			onResult(res)
		}
	}
	return results
}

// regionSpeedStage 构造多地域测速段:逐区串行测量,每区推 region 事件,段末推 section_done + 指标。
func regionSpeedStage(regions []Region, probe RegionSpeedProbe) examStage {
	return examStage{
		name: "region_speed",
		run: func(ctx context.Context, emit func(ExamEvent), report *ExamReport) {
			results := runRegionSpeedSampler(ctx, regions, probe, func(r RegionResult) {
				rc := r
				emit(ExamEvent{Phase: "region", Section: "region_speed", Region: &rc})
			})
			metrics := RegionSpeedMetrics{Regions: results}
			report.RegionSpeed = &metrics
			mc := metrics
			emit(ExamEvent{Phase: "section_done", Section: "region_speed", RegionSpeed: &mc})
		},
	}
}

// regionSpeedErrorStage 降级段:测速器构造失败(如无法建立节点会话)时,
// 逐区推一行 error 而非静默跳过整段,使段的缺失对用户可解释(段末仍推 section_done)。
func regionSpeedErrorStage(regions []Region, cause error) examStage {
	msg := fmt.Sprintf("多地域测速初始化失败: %v", cause)
	probe := func(_ context.Context, r Region) RegionResult {
		return RegionResult{Code: r.Code, Name: r.Name, Error: msg}
	}
	return regionSpeedStage(regions, probe)
}

// defaultRegionSpeedProbe 生产测速器:从 node 另建一条 mihomo 会话(独立于稳定性段),整段 8 区复用该会话串行测量。
// 无 client.Timeout:每区硬超时经 ctx 控制(见 measureRegionSpeed)。
func (d *Detector) defaultRegionSpeedProbe(node *subscription.Node) (RegionSpeedProbe, error) {
	adapter, err := NewProxyAdapter(node)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Transport: &http.Transport{DialContext: adapter.DialContext}}
	slice := time.Duration(regionSliceDurationSec) * time.Second
	hard := time.Duration(regionHardTimeoutSec) * time.Second
	return func(ctx context.Context, r Region) RegionResult {
		return measureRegionSpeed(ctx, client, r, slice, hard)
	}, nil
}

// measureRegionSpeed 测量单区:建连取 TTFB,再下载 slice 时长的切片算下行速率。
// 单区独立硬超时(hard)防卡死;任何失败返回带 Error 的结果,不 panic。
func measureRegionSpeed(ctx context.Context, client *http.Client, r Region, slice, hard time.Duration) RegionResult {
	rctx, cancel := context.WithTimeout(ctx, hard)
	defer cancel()

	ttfb, body, err := openRegionDownload(rctx, client, r.URL)
	if err != nil {
		return RegionResult{Code: r.Code, Name: r.Name, Error: err.Error()}
	}
	defer body.Close()

	mbps, n, drainErr := drainForDuration(body, slice)
	return regionDownloadResult(r, ttfb, mbps, n, drainErr)
}

// regionDownloadResult 由下行读取结果构造单区结果:
// 读取中途真实失败(非 deadline 的干净 EOF)且样本不足 -> 判失败;
// 否则按已读字节算速率视为成功(硬超时切断但样本已足够时仍给出部分速率,与上行测速对称)。
func regionDownloadResult(r Region, ttfb time.Duration, mbps float64, n int64, err error) RegionResult {
	if err != nil && n < minValidDownloadBytes {
		return RegionResult{Code: r.Code, Name: r.Name, Error: fmt.Sprintf("下行读取失败: %v", err)}
	}
	return RegionResult{
		Code:     r.Code,
		Name:     r.Name,
		TTFBms:   int(ttfb.Milliseconds()),
		DownMbps: mbps,
	}
}

// openRegionDownload 发起 GET 并经 httptrace 采到首字节时间(TTFB);非 200 视为错误。
func openRegionDownload(ctx context.Context, client *http.Client, url string) (time.Duration, io.ReadCloser, error) {
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("User-Agent", bandwidthUserAgent)

	var ttfb time.Duration
	trace := &httptrace.ClientTrace{
		GotFirstResponseByte: func() { ttfb = time.Since(start) },
	}
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))

	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return 0, nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	if ttfb == 0 {
		ttfb = time.Since(start)
	}
	return ttfb, resp.Body, nil
}

// drainForDuration 从 body 读取至多 slice 时长(到点即停,靠 deadlineReader),
// 返回下行速率(Mbps)、已读字节、读取错误。deadlineReader 到点返回干净 io.EOF,
// io.Copy 随之以 nil 错误结束;真实的中途读取失败或硬超时 ctx 取消则返回非 nil 错误,
// 供上层区分"正常读满时长"与"链路中断",避免把断流误报成 0 速率的成功行。
func drainForDuration(body io.Reader, slice time.Duration) (float64, int64, error) {
	start := time.Now()
	dr := &deadlineReader{r: body, deadline: start.Add(slice)}
	n, err := io.Copy(io.Discard, dr)
	elapsed := time.Since(start).Seconds()
	if elapsed <= 0 {
		elapsed = 0.001
	}
	return float64(n*8) / elapsed / 1e6, n, err
}
