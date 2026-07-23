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

// 测速端点 URL（Cloudflare speed test）
const (
	defaultLatencyURL  = "https://speed.cloudflare.com/__down?bytes=1000"
	defaultDownloadURL = "https://speed.cloudflare.com/__down?bytes=100000000" // 100MB
	defaultUploadURL   = "https://speed.cloudflare.com/__up"
)

// 默认参数
const (
	defaultSamples             = 8
	defaultDownloadDurationMs  = 10000
	defaultUploadDurationMs    = 10000
	defaultUploadChunkSize     = 256 * 1024 // 256KB
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

// measureLatencyViaProxy 通过代理测量延迟（发送 samples 次小请求）
func measureLatencyViaProxy(ctx context.Context, client *http.Client, url string, samples int) (*LatencyMetrics, error) {
	rtts := make([]float64, 0, samples)

	for i := 0; i < samples; i++ {
		start := time.Now()
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("create latency request: %w", err)
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("latency probe %d: %w", i, err)
		}

		// 读取完整响应体
		_, err = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("latency probe %d: HTTP %d", i, resp.StatusCode)
		}
		if err != nil {
			return nil, fmt.Errorf("read latency response %d: %w", i, err)
		}

		rtt := float64(time.Since(start).Milliseconds())
		rtts = append(rtts, rtt)
	}

	metrics := computeLatencyMetrics(rtts)
	return &metrics, nil
}

// measureDownloadViaProxy 通过代理测量下行带宽
func measureDownloadViaProxy(ctx context.Context, client *http.Client, url string, durationMs int) (float64, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return 0, fmt.Errorf("create download request: %w", err)
	}

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("download request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}

	// 流式读取，计时计字节
	var totalBytes int64
	buf := make([]byte, 32*1024)
	deadline := start.Add(time.Duration(durationMs) * time.Millisecond)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		default:
		}

		n, err := resp.Body.Read(buf)
		totalBytes += int64(n)
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

	// 转换为 Mbps
	mbps := float64(totalBytes) * 8 / 1_000_000 / elapsed
	return mbps, nil
}

// measureUploadViaProxy 通过代理测量上行带宽
func measureUploadViaProxy(ctx context.Context, client *http.Client, url string, durationMs int) (float64, error) {
	// 创建随机数据流
	pr, pw := io.Pipe()
	var sentBytes int64

	go func() {
		defer pw.Close()
		chunk := make([]byte, defaultUploadChunkSize)
		deadline := time.Now().Add(time.Duration(durationMs) * time.Millisecond)

		for time.Now().Before(deadline) {
			select {
			case <-ctx.Done():
				return
			default:
			}

			// 生成随机数据
			if _, err := rand.Read(chunk); err != nil {
				return
			}
			n, err := pw.Write(chunk)
			sentBytes += int64(n)
			if err != nil {
				return
			}
		}
	}()

	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, "POST", url, pr)
	if err != nil {
		return 0, fmt.Errorf("create upload request: %w", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")

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
	if err := json.Unmarshal(body, &result); err != nil {
		// 如果解析失败，使用发送字节数
		result.Bytes = sentBytes
	}

	elapsed := time.Since(start).Seconds()
	if elapsed == 0 {
		return 0, fmt.Errorf("elapsed time is zero")
	}

	// 以服务端实收字节为准
	actualBytes := result.Bytes
	if actualBytes == 0 {
		actualBytes = sentBytes
	}

	// 转换为 Mbps
	mbps := float64(actualBytes) * 8 / 1_000_000 / elapsed
	return mbps, nil
}

// RunProxyTest 通过节点代理执行测速。
//
// node 非 nil 时,经 detection.NewProxyAdapter(node) 构造代理拨号器,
// 所有测速请求经该节点出口访问 Cloudflare 测速端点;node 为 nil 时直连
// (不经节点),作为对比基线。节点连接失败(拨号超时/refused)原样上抛,
// 由调用方转成 HTTP 错误响应。
func RunProxyTest(ctx context.Context, req ProxyTestRequest, node *subscription.Node) (*ProxyTestResult, error) {
	client, err := buildHTTPClient(ctx, node)
	if err != nil {
		return nil, err
	}
	return runProxyTestWithEndpoints(ctx, req, client, defaultLatencyURL, defaultDownloadURL, defaultUploadURL)
}

// buildHTTPClient 按是否指定节点构造 HTTP client。
// node 为 nil = 直连模式,使用标准 transport;非 nil = 经节点代理,
// 复用 detection.NewProxyAdapter 构造 mihomo adapter,其 DialContext 注入
// 自定义 Transport,覆盖到节点服务器的全部 outbound 连接。
func buildHTTPClient(ctx context.Context, node *subscription.Node) (*http.Client, error) {
	if node == nil {
		return &http.Client{Timeout: 30 * time.Second}, nil
	}
	adapter, err := detection.NewProxyAdapter(node)
	if err != nil {
		return nil, fmt.Errorf("create proxy adapter: %w", err)
	}
	return &http.Client{
		Transport: &http.Transport{
			DialContext:       adapter.DialContext,
			DisableKeepAlives:  true,
		},
		Timeout: 30 * time.Second,
	}, nil
}

// runProxyTestWithEndpoints 内部测试辅助函数，允许自定义端点 URL
func runProxyTestWithEndpoints(ctx context.Context, req ProxyTestRequest, client *http.Client, latencyURL, downloadURL, uploadURL string) (*ProxyTestResult, error) {
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

	// 2. 测下行（mode = download 或 full）
	if req.Mode == "download" || req.Mode == "full" {
		downMbps, err := measureDownloadViaProxy(ctx, client, downloadURL, req.DownloadDurationMs)
		if err != nil {
			return nil, fmt.Errorf("measure download: %w", err)
		}
		result.DownMbps = downMbps
	}

	// 3. 测上行（mode = upload 或 full）
	if req.Mode == "upload" || req.Mode == "full" {
		upMbps, err := measureUploadViaProxy(ctx, client, uploadURL, req.UploadDurationMs)
		if err != nil {
			return nil, fmt.Errorf("measure upload: %w", err)
		}
		result.UpMbps = upMbps
	}

	result.ElapsedMs = int(time.Since(start).Milliseconds())
	return result, nil
}
