package detection

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/taliove/proxyhub/internal/subscription"
)

// withBaselineURLs 临时把下行回退点换成测试服务器,测完恢复(包级 var,同包测试可换)。
func withBaselineURLs(t *testing.T, urls []string) {
	t.Helper()
	old := downloadFallbackURLs
	downloadFallbackURLs = urls
	t.Cleanup(func() { downloadFallbackURLs = old })
}

// bigBodyHandler 返回足够字节(> minValidDownloadBytes)的测速响应。
func bigBodyHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(http.StatusOK)
	chunk := make([]byte, 64*1024)
	for written := 0; written < minValidDownloadBytes+64*1024; written += len(chunk) {
		if _, err := w.Write(chunk); err != nil {
			return
		}
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}
}

// TestMeasureBaselineDown_Success verifies the baseline downlink probe against a
// local server:成功时 Code=baseline、DownMbps>0、无 Error,且只测下行(不碰上行)。
func TestMeasureBaselineDown_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(bigBodyHandler))
	defer srv.Close()
	withBaselineURLs(t, []string{srv.URL})

	res := measureBaselineDown(context.Background(), srv.Client())

	if res.Code != "baseline" {
		t.Errorf("Code = %q, want %q", res.Code, "baseline")
	}
	if res.Error != "" {
		t.Errorf("Error = %q, want empty", res.Error)
	}
	if res.DownMbps <= 0 {
		t.Errorf("DownMbps = %v, want > 0", res.DownMbps)
	}
	if res.UpMbps != 0 {
		t.Errorf("UpMbps = %v, want 0 (downlink-only probe)", res.UpMbps)
	}
}

// TestMeasureBaselineDown_AllFail verifies failure classification when all
// fallback URLs fail(403 → HTTP 状态分类)。
func TestMeasureBaselineDown_AllFail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	withBaselineURLs(t, []string{srv.URL})

	res := measureBaselineDown(context.Background(), srv.Client())

	if res.Error == "" {
		t.Fatal("Error empty, want classified failure")
	}
	if !strings.Contains(res.Error, "403") {
		t.Errorf("Error = %q, want HTTP 403 classification", res.Error)
	}
	if res.DownMbps != 0 {
		t.Errorf("DownMbps = %v, want 0 on failure", res.DownMbps)
	}
}

// TestMeasureBaselineDown_RetriesOnce verifies single-shot retry parity with the
// exam baseline row:首次失败自动重试,第二次成功则整体成功。
func TestMeasureBaselineDown_RetriesOnce(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		bigBodyHandler(w, r)
	}))
	defer srv.Close()
	withBaselineURLs(t, []string{srv.URL})

	res := measureBaselineDown(context.Background(), srv.Client())

	if res.Error != "" {
		t.Errorf("Error = %q, want empty after retry", res.Error)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("server calls = %d, want 2 (first failure + one retry)", got)
	}
}

// tcpPassNode 起一个本地 TCP 监听,让 TCP 快筛通过(不触真实网络)。
func tcpPassNode(t *testing.T) *subscription.Node {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	port := ln.Addr().(*net.TCPAddr).Port
	return &subscription.Node{
		Name:   "speed-node",
		Type:   "vless",
		Server: "127.0.0.1",
		Port:   port,
		UUID:   "00000000-0000-0000-0000-000000000000",
		Source: "airport",
	}
}

// TestDetector_TestBaselineDown_Success verifies the batch tier with an injected
// baseline probe:成功测量 → Available 跟随阈值,Mode 写回类为 bandwidth,只含下行。
func TestDetector_TestBaselineDown_Success(t *testing.T) {
	node := tcpPassNode(t)
	d := NewDetector(4, time.Second, time.Second)
	d.SetBaselineDownProbeFactory(func(n *subscription.Node) (BaselineDownProbe, error) {
		return func(ctx context.Context) RegionResult {
			return RegionResult{Code: "baseline", Name: "基准", TTFBms: 120, DownMbps: 55.5}
		}, nil
	})

	res := d.TestBaselineDown(context.Background(), node)

	if !res.Available {
		t.Errorf("Available = false, want true (55.5 >= default min 1.0); Error=%q", res.Error)
	}
	if res.Mode != "bandwidth" {
		t.Errorf("Mode = %q, want %q (写回节点视图带宽字段走 bandwidth 类)", res.Mode, "bandwidth")
	}
	if res.DownMbps != 55.5 {
		t.Errorf("DownMbps = %v, want 55.5", res.DownMbps)
	}
	if res.UpMbps != 0 {
		t.Errorf("UpMbps = %v, want 0 (批量档仅基准下行)", res.UpMbps)
	}
}

// TestDetector_TestBaselineDown_BelowThreshold verifies low bandwidth → unavailable.
func TestDetector_TestBaselineDown_BelowThreshold(t *testing.T) {
	node := tcpPassNode(t)
	d := NewDetector(4, time.Second, time.Second)
	d.SetBaselineDownProbeFactory(func(n *subscription.Node) (BaselineDownProbe, error) {
		return func(ctx context.Context) RegionResult {
			return RegionResult{Code: "baseline", Name: "基准", DownMbps: 0.3}
		}, nil
	})

	res := d.TestBaselineDown(context.Background(), node)

	if res.Available {
		t.Error("Available = true, want false (0.3 < default min 1.0)")
	}
	if res.DownMbps != 0.3 {
		t.Errorf("DownMbps = %v, want 0.3 (实测值保留供列表展示)", res.DownMbps)
	}
	if res.Error == "" {
		t.Error("Error empty, want threshold message")
	}
}

// TestDetector_TestBaselineDown_ProbeFailure verifies probe failure classification.
func TestDetector_TestBaselineDown_ProbeFailure(t *testing.T) {
	node := tcpPassNode(t)
	d := NewDetector(4, time.Second, time.Second)
	d.SetBaselineDownProbeFactory(func(n *subscription.Node) (BaselineDownProbe, error) {
		return func(ctx context.Context) RegionResult {
			return RegionResult{Code: "baseline", Name: "基准", Error: "timeout: 连接超时"}
		}, nil
	})

	res := d.TestBaselineDown(context.Background(), node)

	if res.Available {
		t.Error("Available = true, want false")
	}
	if res.Error != "timeout: 连接超时" {
		t.Errorf("Error = %q, want probe error propagated", res.Error)
	}
}

// TestDetector_TestBaselineDown_FactoryError verifies factory failure → protocol failure.
func TestDetector_TestBaselineDown_FactoryError(t *testing.T) {
	node := tcpPassNode(t)
	d := NewDetector(4, time.Second, time.Second)
	d.SetBaselineDownProbeFactory(func(n *subscription.Node) (BaselineDownProbe, error) {
		return nil, fmt.Errorf("create proxy session: boom")
	})

	res := d.TestBaselineDown(context.Background(), node)

	if res.Available {
		t.Error("Available = true, want false")
	}
	if res.FailReason != FailReasonProtocol {
		t.Errorf("FailReason = %q, want %q", res.FailReason, FailReasonProtocol)
	}
}

// TestDetector_TestBaselineDown_TCPFail verifies dead node fail-fast via TCP precheck.
func TestDetector_TestBaselineDown_TCPFail(t *testing.T) {
	node := &subscription.Node{
		Name: "dead", Type: "vless", Server: "127.0.0.1", Port: 1,
		UUID: "00000000-0000-0000-0000-000000000000", Source: "airport",
	}
	d := NewDetector(4, time.Second, time.Second)
	d.SetBaselineDownProbeFactory(func(n *subscription.Node) (BaselineDownProbe, error) {
		t.Fatal("probe factory should not run: TCP precheck must fail first")
		return nil, nil
	})

	res := d.TestBaselineDown(context.Background(), node)

	if res.Available {
		t.Error("Available = true, want false")
	}
	if !strings.Contains(res.Error, "TCP") {
		t.Errorf("Error = %q, want TCP failure", res.Error)
	}
}

// TestDetector_TestNode_Speedtest verifies the single-node tier via TestNode dispatch:
// 基准下行 + 保留上行(regionSpeedProbeFactory 同体检基准行口径)。
func TestDetector_TestNode_Speedtest(t *testing.T) {
	node := tcpPassNode(t)
	d := NewDetector(4, time.Second, time.Second)
	var probedRegion string
	d.SetRegionSpeedProbeFactory(func(n *subscription.Node) (RegionSpeedProbe, error) {
		return func(ctx context.Context, r Region) RegionResult {
			probedRegion = r.Code
			return RegionResult{Code: r.Code, Name: r.Name, TTFBms: 100, DownMbps: 80, UpMbps: 20}
		}, nil
	})

	res := d.TestNode(context.Background(), node, "speedtest")

	if probedRegion != "baseline" {
		t.Errorf("probed region = %q, want %q (快速测速只测基准行)", probedRegion, "baseline")
	}
	if !res.Available {
		t.Errorf("Available = false, want true; Error=%q", res.Error)
	}
	if res.Mode != "bandwidth" {
		t.Errorf("Mode = %q, want %q", res.Mode, "bandwidth")
	}
	if res.DownMbps != 80 {
		t.Errorf("DownMbps = %v, want 80", res.DownMbps)
	}
	if res.UpMbps != 20 {
		t.Errorf("UpMbps = %v, want 20 (单节点档保留上行)", res.UpMbps)
	}
}

// TestDetector_TestNode_Speedtest_UpFailure verifies uplink failure keeps the
// downlink value but marks the result unavailable with the uplink error.
func TestDetector_TestNode_Speedtest_UpFailure(t *testing.T) {
	node := tcpPassNode(t)
	d := NewDetector(4, time.Second, time.Second)
	d.SetRegionSpeedProbeFactory(func(n *subscription.Node) (RegionSpeedProbe, error) {
		return func(ctx context.Context, r Region) RegionResult {
			return RegionResult{Code: r.Code, Name: r.Name, DownMbps: 80, Error: "uplink: timeout: 连接超时"}
		}, nil
	})

	res := d.TestNode(context.Background(), node, "speedtest")

	if res.Available {
		t.Error("Available = true, want false (uplink failed)")
	}
	if res.DownMbps != 80 {
		t.Errorf("DownMbps = %v, want 80 (下行成功值保留)", res.DownMbps)
	}
	if !strings.Contains(res.Error, "uplink") {
		t.Errorf("Error = %q, want uplink failure", res.Error)
	}
}

// TestDetector_TestNode_Speedtest_DownFailure verifies downlink failure → unavailable.
func TestDetector_TestNode_Speedtest_DownFailure(t *testing.T) {
	node := tcpPassNode(t)
	d := NewDetector(4, time.Second, time.Second)
	d.SetRegionSpeedProbeFactory(func(n *subscription.Node) (RegionSpeedProbe, error) {
		return func(ctx context.Context, r Region) RegionResult {
			return RegionResult{Code: r.Code, Name: r.Name, Error: "timeout: 连接超时"}
		}, nil
	})

	res := d.TestNode(context.Background(), node, "speedtest")

	if res.Available {
		t.Error("Available = true, want false")
	}
	if res.Error == "" {
		t.Error("Error empty, want downlink failure detail")
	}
}
