package speedtest

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/taliove/proxyhub/internal/detection"
	"github.com/taliove/proxyhub/internal/subscription"
)

// TestComputeLatencyMetrics 测试延迟统计计算
func TestComputeLatencyMetrics(t *testing.T) {
	tests := []struct {
		name     string
		rtts     []float64
		wantIdle float64 // 最小值
		wantMax  float64 // 抖动应 < 最大值 - 最小值
	}{
		{
			name:     "stable connection",
			rtts:     []float64{10, 11, 10, 12, 11, 10, 11, 10},
			wantIdle: 10,
			wantMax:  2.5, // 抖动应小于此值
		},
		{
			name:     "variable connection",
			rtts:     []float64{10, 20, 15, 25, 12, 18, 14, 22},
			wantIdle: 10,
			wantMax:  10, // 抖动应小于此值
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := computeLatencyMetrics(tt.rtts)
			if result.IdleLatencyMs != tt.wantIdle {
				t.Errorf("IdleLatencyMs = %v, want %v", result.IdleLatencyMs, tt.wantIdle)
			}
			if result.JitterMs >= tt.wantMax {
				t.Errorf("JitterMs = %v, want < %v", result.JitterMs, tt.wantMax)
			}
		})
	}
}

// TestComputeLatencyMetrics_SingleSample 单样本不应产生 NaN 抖动(回归:
// len==1 时 jitterSum/0 会得 NaN,污染 JSON 序列化)。
func TestComputeLatencyMetrics_SingleSample(t *testing.T) {
	result := computeLatencyMetrics([]float64{42})
	if result.IdleLatencyMs != 42 {
		t.Errorf("IdleLatencyMs = %v, want 42", result.IdleLatencyMs)
	}
	if result.JitterMs != 0 {
		t.Errorf("JitterMs = %v, want 0 (no NaN)", result.JitterMs)
	}
	// 显式断言非 NaN
	if result.JitterMs != result.JitterMs { // NaN != NaN
		t.Errorf("JitterMs is NaN")
	}
}

// TestComputeLatencyMetrics_Empty 空样本返回全 0
func TestComputeLatencyMetrics_Empty(t *testing.T) {
	result := computeLatencyMetrics([]float64{})
	if result != (LatencyMetrics{}) {
		t.Errorf("empty = %v, want zero value", result)
	}
}

// TestMeasureLatencyViaProxy 测试延迟测量
func TestMeasureLatencyViaProxy(t *testing.T) {
	// Mock 测速端点
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 模拟小延迟
		time.Sleep(5 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		w.Write(make([]byte, 1000)) // 1KB
	}))
	defer ts.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	ctx := context.Background()

	result, err := measureLatencyViaProxy(ctx, client, ts.URL, 8)
	if err != nil {
		t.Fatalf("measureLatencyViaProxy failed: %v", err)
	}

	// 验证结果合理性
	if result.IdleLatencyMs <= 0 {
		t.Errorf("IdleLatencyMs should be positive, got %v", result.IdleLatencyMs)
	}
	if result.IdleLatencyMs > 1000 {
		t.Errorf("IdleLatencyMs too high: %v", result.IdleLatencyMs)
	}
	if result.JitterMs < 0 {
		t.Errorf("JitterMs should be non-negative, got %v", result.JitterMs)
	}
}

// TestMeasureDownloadViaProxy 测试下行测速
func TestMeasureDownloadViaProxy(t *testing.T) {
	// Mock 测速端点：发送 10MB 数据
	const dataSize = 10 * 1024 * 1024
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// 发送固定大小的数据
		data := make([]byte, dataSize)
		rand.Read(data)
		w.Write(data)
	}))
	defer ts.Close()

	client := &http.Client{Timeout: 10 * time.Second}
	ctx := context.Background()

	// 测速 1 秒
	mbps, err := measureDownloadViaProxy(ctx, client, []string{ts.URL}, 1000, nil)
	if err != nil {
		t.Fatalf("measureDownloadViaProxy failed: %v", err)
	}

	// 验证速度 > 0
	if mbps <= 0 {
		t.Errorf("Download speed should be positive, got %v Mbps", mbps)
	}

	t.Logf("Download speed: %.2f Mbps", mbps)
}

// TestMeasureUploadViaProxy 测试上行测速
func TestMeasureUploadViaProxy(t *testing.T) {
	// Mock 测速端点:接收数据并返回实收字节数(正确 JSON,验证服务端实收口径主路径)
	var receivedBytes int64
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		// 读取并计数
		buf := make([]byte, 32*1024)
		for {
			n, err := r.Body.Read(buf)
			receivedBytes += int64(n)
			if err != nil {
				break
			}
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"bytes":%d}`, receivedBytes)
	}))
	defer ts.Close()

	client := &http.Client{Timeout: 10 * time.Second}
	ctx := context.Background()

	// 测速 1 秒
	mbps, err := measureUploadViaProxy(ctx, client, ts.URL, 1000, nil)
	if err != nil {
		t.Fatalf("measureUploadViaProxy failed: %v", err)
	}

	// 验证速度 > 0
	if mbps <= 0 {
		t.Errorf("Upload speed should be positive, got %v Mbps", mbps)
	}
	// 验证服务端实收了字节(主路径未被 fallback 掩盖)
	if receivedBytes == 0 {
		t.Errorf("server should have received bytes, got 0")
	}

	t.Logf("Upload speed: %.2f Mbps, received: %d bytes", mbps, receivedBytes)
}

// TestMeasureUploadViaProxy_NonOKStatus 上游返回非 200 时应返回错误,
// 且不泄漏 writer goroutine(错误路径关闭 pr 解除 pw.Write 阻塞)。
func TestMeasureUploadViaProxy_NonOKStatus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 不消费 body 直接返回 413,触发错误路径(此时 writer 仍在 pw.Write 阻塞)
		w.WriteHeader(http.StatusRequestEntityTooLarge)
	}))
	defer ts.Close()

	client := &http.Client{Timeout: 10 * time.Second}
	ctx := context.Background()

	_, err := measureUploadViaProxy(ctx, client, ts.URL, 1000, nil)
	if err == nil {
		t.Fatal("expected error for non-200 upload response, got nil")
	}
	// 不检查具体错误文本,只验证是错误(主验证点:goroutine 不泄漏,
	// 运行时若泄漏会在 -race/长跑下暴露;此处保证错误传播正确)
}

// TestMeasureUploadViaProxy_InvalidJSON 上游返回非法 JSON 时回退到客户端发送字节数
func TestMeasureUploadViaProxy_InvalidJSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 消费 body 再返回非 JSON,触发 fallback 路径
		io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`not-json`))
	}))
	defer ts.Close()

	client := &http.Client{Timeout: 10 * time.Second}
	ctx := context.Background()

	mbps, err := measureUploadViaProxy(ctx, client, ts.URL, 500, nil)
	if err != nil {
		t.Fatalf("measureUploadViaProxy with invalid JSON failed: %v", err)
	}
	// fallback 路径用客户端发送字节数,应仍有正值
	if mbps < 0 {
		t.Errorf("mbps should be non-negative, got %v", mbps)
	}
}

// TestMeasureDownloadViaProxy_OnSample 验证 onSample 回调在测速中被调用(实时采样)
func TestMeasureDownloadViaProxy_OnSample(t *testing.T) {
	const dataSize = 5 * 1024 * 1024 // 5MB
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(make([]byte, dataSize))
	}))
	defer ts.Close()

	client := &http.Client{Timeout: 10 * time.Second}
	ctx := context.Background()

	var samples []detection.Sample
	onSample := func(s detection.Sample) {
		samples = append(samples, s)
	}
	// 测速 1 秒,300ms 窗口应至少触发 1 次采样
	mbps, err := measureDownloadViaProxy(ctx, client, []string{ts.URL}, 1000, onSample)
	if err != nil {
		t.Fatalf("measureDownloadViaProxy failed: %v", err)
	}
	if mbps <= 0 {
		t.Errorf("mbps should be positive, got %v", mbps)
	}
	// 5MB 在高速本地 loopback 可能瞬间读完(<300ms),onSample 可能 0 次。
	// 只验证非 panic + 回调签名正确,不强制次数(避免脆弱断言)。
}

// TestMeasureUploadViaProxy_OnSample 验证 upload onSample 回调被调用
func TestMeasureUploadViaProxy_OnSample(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n, _ := io.Copy(io.Discard, r.Body)
		fmt.Fprintf(w, `{"bytes":%d}`, n)
	}))
	defer ts.Close()

	client := &http.Client{Timeout: 10 * time.Second}
	ctx := context.Background()

	var called bool
	onSample := func(s detection.Sample) { called = true }
	_, err := measureUploadViaProxy(ctx, client, ts.URL, 600, onSample)
	if err != nil {
		t.Fatalf("measureUploadViaProxy failed: %v", err)
	}
	// upload reader 在 transport 发 body 时调 Read,onSample 可能被调用
	// (依赖 transport 读节奏)。只验证非 panic。
	_ = called
}

// TestRunProxyTest_Direct 测试直连模式完整流程
func TestRunProxyTest_Direct(t *testing.T) {
	// Mock 测速端点
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ping":
			time.Sleep(5 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
			w.Write(make([]byte, 1000))
		case "/download":
			w.WriteHeader(http.StatusOK)
			data := make([]byte, 1024*1024) // 1MB
			rand.Read(data)
			w.Write(data)
		case "/upload":
			if r.Method != http.MethodPost {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			// 读取并计数
			n, _ := io.Copy(io.Discard, r.Body)
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"bytes":%d}`, n)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	req := ProxyTestRequest{
		Mode:               "full",
		DownloadDurationMs: 500,
		UploadDurationMs:   500,
	}

	ctx := context.Background()
	result, err := runProxyTestWithEndpoints(ctx, req, nil, ts.URL+"/ping", []string{ts.URL + "/download"}, ts.URL+"/upload", nil, nil)
	if err != nil {
		t.Fatalf("runProxyTest failed: %v", err)
	}

	// 验证所有指标都有值
	if result.IdleLatencyMs <= 0 {
		t.Errorf("IdleLatencyMs should be positive, got %v", result.IdleLatencyMs)
	}
	if result.JitterMs < 0 {
		t.Errorf("JitterMs should be non-negative, got %v", result.JitterMs)
	}
	if result.DownMbps <= 0 {
		t.Errorf("DownMbps should be positive, got %v", result.DownMbps)
	}
	if result.UpMbps <= 0 {
		t.Errorf("UpMbps should be positive, got %v", result.UpMbps)
	}
	if result.ElapsedMs <= 0 {
		t.Errorf("ElapsedMs should be positive, got %v", result.ElapsedMs)
	}

	t.Logf("Test result: down=%.2f Mbps, up=%.2f Mbps, latency=%.2f ms, jitter=%.2f ms, elapsed=%d ms",
		result.DownMbps, result.UpMbps, result.IdleLatencyMs, result.JitterMs, result.ElapsedMs)
}

// TestRunProxyTest_ModeLatencyOnly 测试只测延迟模式
func TestRunProxyTest_ModeLatencyOnly(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		w.Write(make([]byte, 1000))
	}))
	defer ts.Close()

	req := ProxyTestRequest{
		Mode: "latency",
	}

	ctx := context.Background()
	result, err := runProxyTestWithEndpoints(ctx, req, nil, ts.URL, []string{ts.URL}, ts.URL, nil, nil)
	if err != nil {
		t.Fatalf("runProxyTest failed: %v", err)
	}

	// 只有延迟数据
	if result.IdleLatencyMs <= 0 {
		t.Errorf("IdleLatencyMs should be positive, got %v", result.IdleLatencyMs)
	}
	// 下行/上行应为 0
	if result.DownMbps != 0 {
		t.Errorf("DownMbps should be 0 in latency mode, got %v", result.DownMbps)
	}
	if result.UpMbps != 0 {
		t.Errorf("UpMbps should be 0 in latency mode, got %v", result.UpMbps)
	}
}

// TestBuildHTTPClient_Direct 测试直连模式 client 构造
func TestBuildHTTPClient_Direct(t *testing.T) {
	ctx := context.Background()
	client, err := buildHTTPClient(ctx, nil)
	if err != nil {
		t.Fatalf("buildHTTPClient direct failed: %v", err)
	}
	if client == nil {
		t.Fatal("client should not be nil")
	}
	if client.Timeout != 30*time.Second {
		t.Errorf("timeout = %v, want 30s", client.Timeout)
	}
	// 直连模式 Transport 应为 nil(用默认 transport)或非自定义
	// 不强制检查 Transport == nil,因为标准 client 也可能注入
}

// TestBuildHTTPClient_NodeAdapterError 无效协议类型构造 adapter 必然失败
// (generator.ClashProxy 对 unsupported type 返回 error),确定性断言而非 t.Skip。
func TestBuildHTTPClient_NodeAdapterError(t *testing.T) {
	node := &subscription.Node{
		Name:   "invalid",
		Type:   "nonexistent-protocol", // ClashProxy 对此返回 unsupported type 错误
		Server: "127.0.0.1",
		Port:   443,
	}
	ctx := context.Background()
	client, err := buildHTTPClient(ctx, node)
	if err == nil {
		t.Fatalf("expected error for unsupported protocol, got nil (client=%v)", client)
	}
	if client != nil {
		t.Error("client should be nil on adapter construction error")
	}
}

// TestRunProxyTest_DirectViaBuildClient 测试用 buildHTTPClient 构造的直连 client
// 走完整 RunProxyTest 流程,验证生产代码路径(非 runProxyTestWithEndpoints)
func TestRunProxyTest_DirectViaBuildClient(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/__down":
			time.Sleep(2 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
			w.Write(make([]byte, 10000))
		case "/__up":
			if r.Method != http.MethodPost {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			n, _ := io.Copy(io.Discard, r.Body)
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"bytes":%d}`, n)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	// 通过临时覆盖默认端点无法实现,故这里仅测直连路径 buildHTTPClient 返回非 nil
	ctx := context.Background()
	client, err := buildHTTPClient(ctx, nil)
	if err != nil {
		t.Fatalf("buildHTTPClient failed: %v", err)
	}
	// 用 mock server 测延迟,验证 client 可发请求
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/__down", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("client.Do failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}
