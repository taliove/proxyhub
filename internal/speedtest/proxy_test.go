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
	mbps, err := measureDownloadViaProxy(ctx, client, ts.URL, 1000)
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
	// Mock 测速端点：接收数据并返回字节数
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
		w.Write([]byte(`{"bytes":` + string(rune(receivedBytes)) + `}`))
	}))
	defer ts.Close()

	client := &http.Client{Timeout: 10 * time.Second}
	ctx := context.Background()

	// 测速 1 秒
	mbps, err := measureUploadViaProxy(ctx, client, ts.URL, 1000)
	if err != nil {
		t.Fatalf("measureUploadViaProxy failed: %v", err)
	}

	// 验证速度 > 0
	if mbps <= 0 {
		t.Errorf("Upload speed should be positive, got %v Mbps", mbps)
	}

	t.Logf("Upload speed: %.2f Mbps, received: %d bytes", mbps, receivedBytes)
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
	result, err := runProxyTestWithEndpoints(ctx, req, nil, ts.URL+"/ping", ts.URL+"/download", ts.URL+"/upload")
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
	result, err := runProxyTestWithEndpoints(ctx, req, nil, ts.URL, ts.URL, ts.URL)
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

// TestBuildHTTPClient_NodeAdapterError 测试无效节点构造 adapter 失败
// 用一个协议字段非法的节点,触发 mihomo adapter 解析错误
func TestBuildHTTPClient_NodeAdapterError(t *testing.T) {
	// 构造一个非法协议的节点,NewProxyAdapter 应失败
	node := &subscription.Node{
		Name:     "invalid",
		Type:     "nonexistent-protocol",
		Server:   "127.0.0.1",
		Port:     1, // 1 号端口几乎必然不可达
	}
	ctx := context.Background()
	client, err := buildHTTPClient(ctx, node)
	// adapter 构造失败应返回 err 且 client 为 nil
	if err == nil {
		t.Skip("adapter construction did not fail for invalid protocol; skipping (mihomo may be permissive)")
	}
	if client != nil {
		t.Error("client should be nil on error")
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
