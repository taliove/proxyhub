package speedtest

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"

	"github.com/taliove/proxyhub/internal/detection"
	"github.com/taliove/proxyhub/internal/subscription"
)

// ProxyTestRequest 后端代理测速请求
type ProxyTestRequest struct {
	NodeKey            string `json:"node_key"`             // 节点 key（server:port）
	SelfNodeID         *int   `json:"self_node_id"`         // 或自建节点 ID
	Mode               string `json:"mode"`                 // "latency" | "download" | "upload" | "full"
	DownloadDurationMs int    `json:"download_duration_ms"` // 下行测速时长（毫秒）
	UploadDurationMs   int    `json:"upload_duration_ms"`   // 上行测速时长（毫秒）
}

// ProxyTestResult 后端代理测速结果
type ProxyTestResult struct {
	DownMbps        float64 `json:"down_mbps"`
	UpMbps          float64 `json:"up_mbps"`
	IdleLatencyMs   float64 `json:"idle_latency_ms"`
	JitterMs        float64 `json:"jitter_ms"`
	ElapsedMs       int     `json:"elapsed_ms"`
}

// LatencyMetrics 延迟统计指标
type LatencyMetrics struct {
	IdleLatencyMs float64 // 最小 RTT（空闲延迟）
	JitterMs      float64 // 抖动（相邻 RTT 差值平均）
}

// 测速端点:主用 Cloudflare speed test(detection 同口径,经多数节点可达);
// 50MB 下行规避直连 100MB 的 403。Linode 作 fallback(部分节点对 Cloudflare
// reset 时兜底)。UA 必须设浏览器值,否则 Cloudflare 对默认 Go-http-client 返 403
// (与 detection.bandwidthUserAgent 同口径)。
const (
	browserUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"
	defaultLatencyURL  = "https://speed.cloudflare.com/__down?bytes=1000"
	defaultDownloadURL = "https://speed.cloudflare.com/__down?bytes=52428800" // 50MB(直连 100MB 返 403)
	defaultUploadURL   = "https://speed.cloudflare.com/__up"
	// defaultDownloadFallbackURL 下行 fallback:主端点被节点 reset/风控时兜底。
	defaultDownloadFallbackURL = "https://speedtest.tokyo2.linode.com/100MB-tokyo2.bin"
)

// 默认参数
const (
	defaultSamples             = 8
	defaultDownloadDurationMs  = 10000
	defaultUploadDurationMs    = 10000
	defaultUploadChunkSize     = 256 * 1024 // 256KB
	// sampleInterval 采样最小间隔:每累计 ≥ 此时长回调一次瞬时速率(对齐
	// detection.sampleReader 语义),供 SSE 实时数字滚动。
	sampleInterval = 300 * time.Millisecond
)

// computeLatencyMetrics 从 RTT 样本计算延迟统计
func computeLatencyMetrics(rtts []float64) LatencyMetrics {
	if len(rtts) == 0 {
		return LatencyMetrics{}
	}

	// 最小 RTT = 空闲延迟
	sorted := make([]float64, len(rtts))
	copy(sorted, rtts)
	sort.Float64s(sorted)
	idleLatency := sorted[0]

	// 单样本无相邻差值,抖动为 0(避免 len-1=0 除零产出 NaN 污染 JSON 序列化)
	if len(rtts) == 1 {
		return LatencyMetrics{IdleLatencyMs: idleLatency, JitterMs: 0}
	}

	// 抖动 = 相邻 RTT 差值的平均
	var jitterSum float64
	for i := 1; i < len(rtts); i++ {
		diff := rtts[i] - rtts[i-1]
		if diff < 0 {
			diff = -diff
		}
		jitterSum += diff
	}
	jitter := jitterSum / float64(len(rtts)-1)

	return LatencyMetrics{
		IdleLatencyMs: idleLatency,
		JitterMs:      jitter,
	}
}

// measureLatencyViaProxy 通过代理测量延迟（发送 samples 次小请求）。
// 用 Cloudflare 1KB 小请求测 RTT(不设 Range:经部分节点 Range 请求会触发 reset,
// 对齐 detection.openDownload 的普通 GET)。设浏览器 UA 规避 Cloudflare 对默认
// Go UA 的 403(与 detection 同口径)。
func measureLatencyViaProxy(ctx context.Context, client *http.Client, url string, samples int) (*LatencyMetrics, error) {
	rtts := make([]float64, 0, samples)

	for i := 0; i < samples; i++ {
		start := time.Now()
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("create latency request: %w", err)
		}
		req.Header.Set("User-Agent", browserUserAgent)

		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("latency probe %d: %w", i, err)
		}

		// 读取完整响应体(1KB),丢弃。
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("latency probe %d: HTTP %d", i, resp.StatusCode)
		}

		rtt := float64(time.Since(start).Milliseconds())
		rtts = append(rtts, rtt)
	}

	metrics := computeLatencyMetrics(rtts)
	return &metrics, nil
}

// measureDownloadViaProxy 通过代理测量下行带宽。
// urls 为主端点 + fallback:依次尝试,第一个返回 200/206 且能读出数据的用于测速。
// 每端点单独探测超时(probeTimeout),避免某端点对节点 IP 挂起吃满整个测速时长
// 导致后续 fallback 点无机会(如 Cloudflare 对部分机场 IP 段 reset 不立即失败)。
// 测速阶段流式读取至 deadline 或 EOF;EOF(固定文件读完)即结束,速率=字节/耗时。
// onSample 非 nil 时,每累计 ≥ sampleInterval 回调一次瞬时速率(供 SSE 实时数字)。
func measureDownloadViaProxy(ctx context.Context, client *http.Client, urls []string, durationMs int, onSample func(detection.Sample)) (float64, error) {
	const probeTimeout = 8 * time.Second
	const minValidBytes = 512 * 1024 // 512KB:低于此值判为死链/占位

	// 探测:找到第一个可用端点
	var goodURL string
	var lastErr error
	for _, u := range urls {
		if u == "" {
			continue
		}
		pctx, cancel := context.WithTimeout(ctx, probeTimeout)
		ok, err := probeDownloadEndpoint(pctx, client, u)
		cancel()
		if err != nil {
			lastErr = err
			continue
		}
		if !ok {
			lastErr = fmt.Errorf("endpoint %s: dead link", u)
			continue
		}
		goodURL = u
		break
	}
	if goodURL == "" {
		if lastErr == nil {
			lastErr = fmt.Errorf("no usable download endpoint")
		}
		return 0, fmt.Errorf("probe download endpoints: %w", lastErr)
	}

	// 测速:流式读取选中端点
	req, err := http.NewRequestWithContext(ctx, "GET", goodURL, nil)
	if err != nil {
		return 0, fmt.Errorf("create download request: %w", err)
	}
	req.Header.Set("User-Agent", browserUserAgent)

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("download request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return 0, fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}

	var totalBytes int64
	buf := make([]byte, 32*1024)
	deadline := start.Add(time.Duration(durationMs) * time.Millisecond)
	// 采样窗口(对齐 detection.sampleReader:累计字节 + 窗口时长,到点回调瞬时速率)
	winStart, winBytes := start, int64(0)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		default:
		}

		n, err := resp.Body.Read(buf)
		totalBytes += int64(n)
		winBytes += int64(n)
		if onSample != nil && n > 0 {
			winElapsed := time.Since(winStart)
			if winElapsed >= sampleInterval {
				mbps := float64(winBytes*8) / winElapsed.Seconds() / 1e6
				onSample(detection.Sample{Phase: "download", Mbps: mbps, ElapsedMs: int(time.Since(start).Milliseconds())})
				winStart, winBytes = time.Now(), 0
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, fmt.Errorf("read download response: %w", err)
		}
	}

	elapsed := time.Since(start).Seconds()
	if elapsed == 0 {
		return 0, fmt.Errorf("elapsed time is zero")
	}

	mbps := float64(totalBytes) * 8 / 1_000_000 / elapsed
	return mbps, nil
}

// probeDownloadEndpoint 探测单端点是否可用:返回 200/206 且首读 ≥ minValidBytes。
// 用于 fallback 阶段快速剔除死链/风控端点(不消费整个测速时长)。
func probeDownloadEndpoint(ctx context.Context, client *http.Client, url string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("User-Agent", browserUserAgent)
	req.Header.Set("Range", "bytes=0-65535") // 只取首 64KB 探测,省流量

	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return false, fmt.Errorf("status %d", resp.StatusCode)
	}
	// 读一小段确认非死链占位
	buf := make([]byte, 4096)
	n, _ := io.ReadFull(resp.Body, buf)
	return n >= 1024, nil // 至少 1KB 真实数据
}

// measureUploadViaProxy 通过代理测量上行带宽。
// 用 uploadReader 作为 POST body:transport 发 body 时调 Read,Read 内生成随机
// 数据 + 累计 + 采样回调(对齐 detection.sampleReader 语义)。到 deadline 返回
// EOF 停流。无后台 goroutine,天然无泄漏/竞争(go-reviewer 之前的 pipe+channel
// 逻辑已废弃)。
func measureUploadViaProxy(ctx context.Context, client *http.Client, url string, durationMs int, onSample func(detection.Sample)) (float64, error) {
	start := time.Now()
	r := newUploadReader(ctx, start, time.Duration(durationMs)*time.Millisecond, onSample)

	req, err := http.NewRequestWithContext(ctx, "POST", url, r)
	if err != nil {
		return 0, fmt.Errorf("create upload request: %w", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("User-Agent", browserUserAgent)

	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("upload request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("upload failed: HTTP %d", resp.StatusCode)
	}

	// 读取服务端返回的实际接收字节数
	var result struct {
		Bytes int64 `json:"bytes"`
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("read upload response: %w", err)
	}
	serverBytes := int64(0)
	if err := json.Unmarshal(body, &result); err == nil {
		serverBytes = result.Bytes
	}

	elapsed := time.Since(start).Seconds()
	if elapsed == 0 {
		return 0, fmt.Errorf("elapsed time is zero")
	}

	// 以服务端实收为准,缺失回退客户端发送字节
	actualBytes := serverBytes
	if actualBytes == 0 {
		actualBytes = r.sent
	}

	mbps := float64(actualBytes) * 8 / 1_000_000 / elapsed
	return mbps, nil
}

// uploadReader 上行测速的 POST body:Read 时填随机数据,到 deadline 返回 EOF;
// 每累计 ≥ sampleInterval 回调一次瞬时速率。无 goroutine,数据竞争安全。
type uploadReader struct {
	ctx      context.Context
	deadline time.Time
	chunk    []byte // 预生成随机块(循环复用,块远大于 gzip 滑窗不可压缩)
	sent     int64
	start    time.Time
	winStart time.Time
	winBytes int64
	onSample func(detection.Sample)
}

func newUploadReader(ctx context.Context, start time.Time, dur time.Duration, onSample func(detection.Sample)) *uploadReader {
	chunk := make([]byte, defaultUploadChunkSize)
	if _, err := rand.Read(chunk); err != nil {
		// 极少失败;回退全零块(仍可测速,只是可压缩,速率可能虚高,但仅 RNG 失败时)
		chunk = make([]byte, defaultUploadChunkSize)
	}
	return &uploadReader{
		ctx:      ctx,
		deadline: start.Add(dur),
		chunk:    chunk,
		start:    start,
		winStart: start,
		onSample: onSample,
	}
}

func (r *uploadReader) Read(p []byte) (int, error) {
	if time.Now().After(r.deadline) || r.ctx.Err() != nil {
		return 0, io.EOF
	}
	// 用预生成 chunk 循环填 p(块远大于 32KB 滑窗,循环复用不产生可匹配前缀)
	n := copy(p, r.chunk)
	if n < len(p) {
		// p 大于 chunk:重复填(实测 p 通常 ≤ 32KB,chunk 256KB,一般一次填够)
		for off := n; off < len(p); {
			n2 := copy(p[off:], r.chunk)
			off += n2
		}
		n = len(p)
	}
	r.sent += int64(n)
	r.winBytes += int64(n)
	if r.onSample != nil {
		winElapsed := time.Since(r.winStart)
		if winElapsed >= sampleInterval {
			mbps := float64(r.winBytes*8) / winElapsed.Seconds() / 1e6
			r.onSample(detection.Sample{Phase: "upload", Mbps: mbps, ElapsedMs: int(time.Since(r.start).Milliseconds())})
			r.winStart = time.Now()
			r.winBytes = 0
		}
	}
	return n, nil
}

// RunProxyTest 通过节点代理执行测速。
//
// node 非 nil 时,经 detection.NewProxyAdapter(node) 构造代理拨号器,
// 所有测速请求经该节点出口访问 Cloudflare 测速端点;node 为 nil 时直连
// (不经节点),作为对比基线。节点连接失败(拨号超时/refused)原样上抛,
// 由调用方转成 HTTP 错误响应。
func RunProxyTest(ctx context.Context, req ProxyTestRequest, node *subscription.Node, onLatency func(latencyMs, jitterMs float64), onSample func(detection.Sample)) (*ProxyTestResult, error) {
	client, err := buildHTTPClient(ctx, node)
	if err != nil {
		return nil, err
	}
	return runProxyTestWithEndpoints(ctx, req, client,
		defaultLatencyURL,
		[]string{defaultDownloadURL, defaultDownloadFallbackURL},
		defaultUploadURL,
		onLatency, onSample)
}

// buildHTTPClient 按是否指定节点构造 HTTP client。
// node 为 nil = 直连模式,标准 transport + 30s 超时;非 nil = 经节点代理,
// 复用 detection.NewProxyAdapter 构造 mihomo adapter,其 DialContext 注入
// 自定义 Transport。经节点分支对齐 detection.streamClient:不设 client.Timeout
// (靠 handler 的 ctx deadline)、不设 DisableKeepAlives,避免与 detection
// 经节点测速行为分叉。
func buildHTTPClient(ctx context.Context, node *subscription.Node) (*http.Client, error) {
	if node == nil {
		return &http.Client{Timeout: 30 * time.Second}, nil
	}
	adapter, err := detection.NewProxyAdapter(node)
	if err != nil {
		return nil, fmt.Errorf("create proxy adapter: %w", err)
	}
	return &http.Client{
		Transport: &http.Transport{DialContext: adapter.DialContext},
	}, nil
}

// runProxyTestWithEndpoints 内部测试辅助函数，允许自定义端点 URL
func runProxyTestWithEndpoints(ctx context.Context, req ProxyTestRequest, client *http.Client, latencyURL string, downloadURLs []string, uploadURL string, onLatency func(latencyMs, jitterMs float64), onSample func(detection.Sample)) (*ProxyTestResult, error) {
	start := time.Now()

	// 默认值
	if req.Mode == "" {
		req.Mode = "full"
	}
	if req.DownloadDurationMs == 0 {
		req.DownloadDurationMs = defaultDownloadDurationMs
	}
	if req.UploadDurationMs == 0 {
		req.UploadDurationMs = defaultUploadDurationMs
	}

	// 使用标准 HTTP client（直连模式）
	if client == nil {
		client = &http.Client{
			Timeout: 30 * time.Second,
		}
	}

	result := &ProxyTestResult{}

	// 1. 测延迟（所有模式都需要）
	latency, err := measureLatencyViaProxy(ctx, client, latencyURL, defaultSamples)
	if err != nil {
		return nil, fmt.Errorf("measure latency: %w", err)
	}
	result.IdleLatencyMs = latency.IdleLatencyMs
	result.JitterMs = latency.JitterMs
	if onLatency != nil {
		onLatency(latency.IdleLatencyMs, latency.JitterMs)
	}

	// 2. 测下行（mode = download 或 full）
	if req.Mode == "download" || req.Mode == "full" {
		downMbps, err := measureDownloadViaProxy(ctx, client, downloadURLs, req.DownloadDurationMs, onSample)
		if err != nil {
			return nil, fmt.Errorf("measure download: %w", err)
		}
		result.DownMbps = downMbps
	}

	// 3. 测上行（mode = upload 或 full）
	if req.Mode == "upload" || req.Mode == "full" {
		upMbps, err := measureUploadViaProxy(ctx, client, uploadURL, req.UploadDurationMs, onSample)
		if err != nil {
			return nil, fmt.Errorf("measure upload: %w", err)
		}
		result.UpMbps = upMbps
	}

	result.ElapsedMs = int(time.Since(start).Milliseconds())
	return result, nil
}
