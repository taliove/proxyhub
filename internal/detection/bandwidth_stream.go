package detection

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/taliove/proxyhub/internal/subscription"
)

// sampleInterval 采样最小间隔:每累计 ≥ 此时长回调一次瞬时速率。
const sampleInterval = 300 * time.Millisecond

// bandwidthUserAgent 带宽测试请求 UA。Cloudflare 等对默认 Go UA(Go-http-client)返 403,
// 用浏览器 UA 规避(与 mihomo.go 的 requestViaProxy 一致)。
const bandwidthUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"

// downloadFallbackURLs 下行测速内置回退点:当节点出口 IP 被某测速点风控(如 Cloudflare
// 对机房 IP 返 403)时,依次尝试后续点。配置的 DownURL 排在最前,这些作兜底。
// 各点均为实测可用的大文件(≥100MB)源,保证快节点也能采样够时长。
// 注意:某点即使返 200 也可能内容极小(死链占位),由 minValidDownloadBytes 校验拦截并触发回退。
var downloadFallbackURLs = []string{
	"https://speed.cloudflare.com/__down?bytes=104857600",        // 100MB
	"https://speedtest.tokyo2.linode.com/100MB-tokyo2.bin",       // 100MB,支持 range
	"https://speedtest.singapore.linode.com/100MB-singapore.bin", // 100MB
	"https://speedtest.frankfurt.linode.com/100MB-frankfurt.bin", // 100MB
}

// minValidDownloadBytes 有效下行的最小字节数:低于此值(且非超时)判为死链/占位响应,触发回退。
const minValidDownloadBytes = 512 * 1024 // 512KB

// Sample 一个带宽采样点(瞬时速率,非累计平均)。
type Sample struct {
	Phase     string  `json:"phase"`      // "download" / "upload"
	Mbps      float64 `json:"mbps"`       // 本采样区间的瞬时速率
	ElapsedMs int     `json:"elapsed_ms"` // 自该方向开始的累计毫秒
}

// sampleReader 包裹 io.Reader,累计读取字节,每 sampleInterval 回调一次区间瞬时速率。
// 用于下行(包裹 resp.Body)和上行(包裹随机数据 reader)的实时采样。
type sampleReader struct {
	r        io.Reader
	phase    string
	onSample func(Sample)

	start       time.Time // 该方向开始时间(算 ElapsedMs)
	windowStart time.Time // 当前采样窗口起点
	windowBytes int64     // 当前采样窗口累计字节
	totalBytes  int64     // 全程累计字节
}

// newSampleReader 创建采样读取器。start 为该方向起始时刻。
func newSampleReader(r io.Reader, phase string, start time.Time, onSample func(Sample)) *sampleReader {
	return &sampleReader{
		r:           r,
		phase:       phase,
		onSample:    onSample,
		start:       start,
		windowStart: start,
	}
}

// setSource 切换底层数据源(下行续传:一个请求体读完后换新请求),
// 采样累计(totalBytes/窗口)跨源保持,使固定时长内的速率连续。
func (s *sampleReader) setSource(r io.Reader) { s.r = r }

// Read 实现 io.Reader:透传读取,累计字节,达到采样间隔就回调一次区间速率。
func (s *sampleReader) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	if n > 0 {
		s.windowBytes += int64(n)
		s.totalBytes += int64(n)

		elapsed := time.Since(s.windowStart)
		if elapsed >= sampleInterval && s.onSample != nil {
			mbps := float64(s.windowBytes*8) / elapsed.Seconds() / 1e6
			s.onSample(Sample{
				Phase:     s.phase,
				Mbps:      mbps,
				ElapsedMs: int(time.Since(s.start).Milliseconds()),
			})
			// 重置采样窗口
			s.windowStart = time.Now()
			s.windowBytes = 0
		}
	}
	return n, err
}

// TotalBytes 返回全程累计字节(供方向超时后算平均速率)。
func (s *sampleReader) TotalBytes() int64 { return s.totalBytes }

// TestBandwidthStream 流式带宽测试:下行+上行各自采样,通过 onSample 实时回调瞬时速率。
// 返回最终聚合 TestResult(与 testBandwidth 判定逻辑一致)。
// 各方向独立 DirTimeoutSec 超时;方向超时但已下够数据时用已采样字节算平均速率视为完成。
func (d *Detector) TestBandwidthStream(ctx context.Context, node *subscription.Node, onSample func(Sample)) TestResult {
	if !d.tcpQuickCheck(ctx, node) {
		return TestResult{Available: false, Mode: "bandwidth", Error: "TCP connection failed"}
	}

	adapter, err := d.newProxyAdapter(node)
	if err != nil {
		return TestResult{Available: false, Mode: "bandwidth", Error: fmt.Sprintf("create proxy adapter: %v", err)}
	}

	cfg := d.resolveBandwidthConfig()

	// 固定测速时长:两个方向都跑满这个时长 → 曲线等长。DirTimeout 作硬上限防卡死。
	testDur := time.Duration(cfg.TestDurationSec) * time.Second
	if testDur <= 0 {
		testDur = 10 * time.Second
	}
	dirTimeout := time.Duration(cfg.DirTimeoutSec) * time.Second
	if dirTimeout < testDur {
		dirTimeout = testDur + 10*time.Second // 硬上限至少比测速时长多 10s
	}

	start := time.Now()
	elapsedMs := func() int { return int(time.Since(start).Milliseconds()) }

	// 下行:配置的 URL 优先,后接内置回退点(节点出口被 Cloudflare 风控时自动换点)
	downURLs := append([]string{cfg.DownURL}, downloadFallbackURLs...)
	downMbps, err := d.streamDownload(ctx, adapter, downURLs, testDur, dirTimeout, onSample)
	if err != nil {
		return TestResult{
			Available: false, Mode: "bandwidth", Error: fmt.Sprintf("下行测试失败: %v", err),
			ElapsedMs: elapsedMs(), MinDownMbps: cfg.MinDownMbps, MinUpMbps: cfg.MinUpMbps,
		}
	}

	// 上行
	upMbps, err := d.streamUpload(ctx, adapter, cfg.UpURL, cfg.UpBytes, testDur, dirTimeout, onSample)
	if err != nil {
		return TestResult{
			Available: false, Mode: "bandwidth", Error: fmt.Sprintf("上行测试失败: %v", err),
			DownMbps: downMbps, ElapsedMs: elapsedMs(), MinDownMbps: cfg.MinDownMbps, MinUpMbps: cfg.MinUpMbps,
		}
	}

	available := downMbps >= cfg.MinDownMbps && upMbps >= cfg.MinUpMbps
	var errMsg string
	if !available {
		errMsg = fmt.Sprintf("带宽低于阈值: down=%.2f (>= %.2f) up=%.2f (>= %.2f)",
			downMbps, cfg.MinDownMbps, upMbps, cfg.MinUpMbps)
	}

	return TestResult{
		Available:   available,
		Latency:     0,
		Mode:        "bandwidth",
		DownMbps:    downMbps,
		UpMbps:      upMbps,
		ElapsedMs:   elapsedMs(),
		MinDownMbps: cfg.MinDownMbps,
		MinUpMbps:   cfg.MinUpMbps,
		Error:       errMsg,
	}
}

// streamClient 流式测速专用 HTTP 客户端:不设 Timeout,全靠 ctx 控制(避免与单方向超时打架)。
func streamClient(adapter *ProxyAdapter) *http.Client {
	return &http.Client{
		Transport: &http.Transport{DialContext: adapter.DialContext},
		// Timeout 留 0:流式下载可能持续数十秒,靠 request 的 ctx 单方向超时兜底
	}
}

// streamDownload 下行固定时长测速:先探测出一个可用测速点(遇 403/死链回退),
// 然后从它持续读取 testDur 时长(数据源 EOF 就重新请求续传,保证快节点也填满整段时长)。
// 速率 = 该时长内读取的总字节 / 实际耗时。dirHardTimeout 是防卡死的硬上限。
func (d *Detector) streamDownload(ctx context.Context, adapter *ProxyAdapter, urls []string, testDur, dirHardTimeout time.Duration, onSample func(Sample)) (float64, error) {
	client := streamClient(adapter)

	// dirHardTimeout 作硬上限兜底(含探测耗时),防链路卡死
	dctx, cancel := context.WithTimeout(ctx, dirHardTimeout)
	defer cancel()

	// 探测阶段:找到第一个能建立 200 且内容有效的测速点
	goodURL, firstBody, err := d.probeDownloadURL(dctx, client, urls)
	if err != nil {
		return 0, err
	}

	// deadline 锚定到探测完成后(而非探测前),否则探测耗时(经代理约 1-2s)会吃掉
	// 采样时长,导致下行实际只跑 testDur - 探测耗时(上行无探测,故曾出现下行 8s / 上行 10s)
	start := time.Now()
	deadline := start.Add(testDur)
	// 包 deadlineReader:到 testDur 时 Read 返回 EOF,io.Copy 干净停在时长处(否则会读完整个响应体才停)
	sr := newSampleReader(&deadlineReader{r: firstBody, deadline: deadline}, "download", start, onSample)

	bodies := []io.ReadCloser{firstBody} // 记录所有打开的响应体,末尾统一关闭
	for time.Now().Before(deadline) {
		_, copyErr := io.Copy(io.Discard, sr)
		if !time.Now().Before(deadline) || dctx.Err() != nil {
			break // 到点/超时,正常结束
		}
		if copyErr != nil && copyErr != io.EOF {
			break // 真实读取错误(非 EOF),停止(已采样字节仍有效)
		}
		// 数据源读完但时长没到 → 重新请求续传(仍受同一 deadline 约束)
		body, rErr := d.openDownload(dctx, client, goodURL)
		if rErr != nil {
			break // 续传失败,用已读字节算结果
		}
		bodies = append(bodies, body)
		sr.setSource(&deadlineReader{r: body, deadline: deadline})
	}
	for _, b := range bodies {
		b.Close()
	}

	elapsed := time.Since(start).Seconds()
	if elapsed <= 0 {
		elapsed = 0.001
	}
	total := sr.TotalBytes()
	if total < minValidDownloadBytes {
		return 0, fmt.Errorf("下行数据不足(%d 字节)", total)
	}
	return float64(total*8) / elapsed / 1e6, nil
}

// probeDownloadURL 依次尝试候选 URL,返回首个建立 200 连接的 URL 及其响应体。
// 用于下行测速起步;后续续传复用返回的 goodURL。
func (d *Detector) probeDownloadURL(ctx context.Context, client *http.Client, urls []string) (string, io.ReadCloser, error) {
	var lastErr error
	for _, url := range urls {
		if url == "" {
			continue
		}
		body, err := d.openDownload(ctx, client, url)
		if err != nil {
			lastErr = err
			continue // 连接失败/403 → 试下一个
		}
		return url, body, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("无可用下行测速点")
	}
	return "", nil, lastErr
}

// openDownload 对单个 URL 发起 GET,返回 200 响应体(非 200 视为错误)。
func (d *Detector) openDownload(ctx context.Context, client *http.Client, url string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", bandwidthUserAgent) // 避免 Cloudflare 对默认 Go UA 返 403
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		resp.Body.Close()
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	return resp.Body, nil
}

// streamUpload 上行固定时长测速:持续 POST 数据流 testDur 时长,速率 = 已写字节 / 实际耗时。
// 请求体到 testDur(或 maxBytes 上限)自动 EOF,POST 随之结束。不设固定 ContentLength
// (用 chunked 传输),避免服务端苦等永不到来的字节。dirHardTimeout 是防卡死的硬上限。
func (d *Detector) streamUpload(ctx context.Context, adapter *ProxyAdapter, url string, maxBytes int, testDur, dirHardTimeout time.Duration, onSample func(Sample)) (float64, error) {
	uctx, cancel := context.WithTimeout(ctx, dirHardTimeout)
	defer cancel()

	start := time.Now()
	// 数据源:到 testDur 或 maxBytes 上限即 EOF(时长优先,快节点也只跑 testDur)
	dataSrc := &durationReader{deadline: start.Add(testDur), remaining: int64(maxBytes)}
	sr := newSampleReader(dataSrc, "upload", start, onSample)

	client := streamClient(adapter)
	req, err := http.NewRequestWithContext(uctx, "POST", url, sr)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", bandwidthUserAgent)
	req.Header.Set("Content-Type", "application/octet-stream")
	// 不设 ContentLength → Go 用 chunked 传输,body EOF 时请求自然结束

	resp, err := client.Do(req)
	elapsed := time.Since(start).Seconds()
	if elapsed <= 0 {
		elapsed = 0.001
	}
	total := sr.TotalBytes()
	if err != nil {
		// 硬超时但已上传够数据 → 用已写字节算平均速率
		if uctx.Err() == context.DeadlineExceeded && total > minValidDownloadBytes {
			return float64(total*8) / elapsed / 1e6, nil
		}
		return 0, err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if total < minValidDownloadBytes {
		return 0, fmt.Errorf("上行数据不足(%d 字节)", total)
	}
	return float64(total*8) / elapsed / 1e6, nil
}

// deadlineReader 包裹下行响应体,到 deadline 即返回 EOF,让 io.Copy 在测速时长处干净停下。
// (下行需要:io.Copy 只在返回后才检查 deadline,不包这层会一路读到响应体 EOF 才停,
// 导致下行跑超时长。与上行 durationReader 的内部 deadline 检查对称。)
type deadlineReader struct {
	r        io.Reader
	deadline time.Time
}

func (r *deadlineReader) Read(p []byte) (int, error) {
	if !time.Now().Before(r.deadline) {
		return 0, io.EOF
	}
	return r.r.Read(p)
}

// durationReader 生成非压缩字节,到 deadline 或 remaining 耗尽即返回 EOF。
// 用于上行固定时长测速:时长到点或达数据上限就停,POST body 随之结束。
type durationReader struct {
	deadline  time.Time
	remaining int64
	i         byte
}

func (r *durationReader) Read(p []byte) (int, error) {
	if r.remaining <= 0 || !time.Now().Before(r.deadline) {
		return 0, io.EOF
	}
	n := len(p)
	if int64(n) > r.remaining {
		n = int(r.remaining)
	}
	for i := 0; i < n; i++ {
		p[i] = r.i
		r.i++
	}
	r.remaining -= int64(n)
	return n, nil
}
