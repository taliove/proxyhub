package detection

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/taliove/proxyhub/internal/subscription"
)

// 快速测速(检查动作 3)实现:基准下行口径,与体检基准行同一探针(不另造轮子)。
// 三档入口:
//   - TestBaselineDown     批量档:仅基准下行(控成本),供 batch_speedtest kind 逐节点调用。
//   - TestSpeedtest        单节点档:基准下行 + 保留上行(bandwidth_up_mbps 字段数据源不断)。
//   - TestSpeedtestStream  单节点 SSE 档:基准端点 + 既有采样流(UX 不变)。
//
// 写回约定:三档返回的 TestResult.Mode 均为 "bandwidth"——结果写回节点视图带宽字段
// (node_health target_name="bandwidth" + 内存池 BandwidthDown/UpMbps),与 handleTestNode
// 既有写回路径完全同轨,不新增存储分支。

// BaselineDownProbe 基准下行探针:单点测量(Cloudflare 就近 Anycast POP),仅下行。
type BaselineDownProbe func(ctx context.Context) RegionResult

// SetBaselineDownProbeFactory 覆盖基准下行探针工厂(测试用:注入假探针绕过真实网络)。
func (d *Detector) SetBaselineDownProbeFactory(factory func(*subscription.Node) (BaselineDownProbe, error)) {
	d.baselineDownProbeFactory = factory
}

// measureBaselineDown 基准下行测量(仅下行):复用体检基准行同一实现——
// measureRegionSpeedWithFallback + downloadFallbackURLs(Cloudflare 100MB 首选)+
// 单区切片/硬超时参数 + withRegionRetry 单次重试。与体检基准行数字可对照。
func measureBaselineDown(ctx context.Context, client *http.Client) RegionResult {
	probe := withRegionRetry(func(ctx context.Context, r Region) RegionResult {
		return measureRegionSpeedWithFallback(ctx, client, r,
			time.Duration(regionSliceDurationSec)*time.Second,
			time.Duration(regionHardTimeoutSec)*time.Second,
			downloadFallbackURLs)
	})
	return probe(ctx, baselineRegion)
}

// defaultBaselineDownProbe 生产基准下行探针:从 node 另建一条 mihomo 会话,仅测下行。
func (d *Detector) defaultBaselineDownProbe(node *subscription.Node) (BaselineDownProbe, error) {
	adapter, err := d.newProxyAdapter(node)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Transport: &http.Transport{DialContext: adapter.DialContext}}
	return func(ctx context.Context) RegionResult {
		return measureBaselineDown(ctx, client)
	}, nil
}

// TestBaselineDown 批量快速测速档:TCP 快筛 fail-fast + 仅基准下行。
// Available 判定:测量成功且下行 >= MinDownMbps(与 bandwidth 档同一阈值源)。
func (d *Detector) TestBaselineDown(ctx context.Context, node *subscription.Node) TestResult {
	if err := d.tcpQuickCheckErr(ctx, node); err != nil {
		return TestResult{
			Available: false, Mode: "bandwidth",
			Error:      fmt.Sprintf("TCP connection failed: %v", err),
			FailReason: ClassifyFailure(err),
		}
	}

	probe, err := d.baselineDownProbeFactory(node)
	if err != nil {
		return TestResult{
			Available: false, Mode: "bandwidth",
			Error:      fmt.Sprintf("create proxy adapter: %v", err),
			FailReason: FailReasonProtocol,
		}
	}

	cfg := d.resolveBandwidthConfig()
	ctx, cancel := context.WithTimeout(ctx, time.Duration(cfg.TimeoutSec)*time.Second)
	defer cancel()

	start := time.Now()
	res := probe(ctx)
	elapsedMs := int(time.Since(start).Milliseconds())

	if res.Error != "" {
		return TestResult{
			Available: false, Mode: "bandwidth", Error: res.Error,
			DownMbps: res.DownMbps, ElapsedMs: elapsedMs,
			MinDownMbps: cfg.MinDownMbps, MinUpMbps: cfg.MinUpMbps,
		}
	}

	available := res.DownMbps >= cfg.MinDownMbps
	var errMsg string
	if !available {
		errMsg = fmt.Sprintf("带宽低于阈值: down=%.2f (>= %.2f)", res.DownMbps, cfg.MinDownMbps)
	}
	return TestResult{
		Available:   available,
		Mode:        "bandwidth",
		DownMbps:    res.DownMbps,
		ElapsedMs:   elapsedMs,
		MinDownMbps: cfg.MinDownMbps,
		MinUpMbps:   cfg.MinUpMbps,
		Error:       errMsg,
	}
}

// TestSpeedtest 单节点快速测速档:基准下行 + 保留上行。
// 探针直接复用 regionSpeedProbeFactory(体检基准行同一路径:withRegionRetry + 基准行),
// 上行测量随之保留(RegionResult.UpMbps),bandwidth_up_mbps 字段数据源不断。
func (d *Detector) TestSpeedtest(ctx context.Context, node *subscription.Node) TestResult {
	if err := d.tcpQuickCheckErr(ctx, node); err != nil {
		return TestResult{
			Available: false, Mode: "bandwidth",
			Error:      fmt.Sprintf("TCP connection failed: %v", err),
			FailReason: ClassifyFailure(err),
		}
	}

	probe, err := d.regionSpeedProbeFactory(node)
	if err != nil {
		return TestResult{
			Available: false, Mode: "bandwidth",
			Error:      fmt.Sprintf("create proxy adapter: %v", err),
			FailReason: FailReasonProtocol,
		}
	}

	cfg := d.resolveBandwidthConfig()
	ctx, cancel := context.WithTimeout(ctx, time.Duration(cfg.TimeoutSec)*time.Second)
	defer cancel()

	start := time.Now()
	// 只测基准行(examRegionsWithBaseline 首元素):下行 + 上行,与体检基准行同口径。
	res := withRegionRetry(probe)(ctx, baselineRegion)
	elapsedMs := int(time.Since(start).Milliseconds())

	if res.Error != "" {
		return TestResult{
			Available: false, Mode: "bandwidth", Error: res.Error,
			DownMbps: res.DownMbps, UpMbps: res.UpMbps, ElapsedMs: elapsedMs,
			MinDownMbps: cfg.MinDownMbps, MinUpMbps: cfg.MinUpMbps,
		}
	}

	available := res.DownMbps >= cfg.MinDownMbps && res.UpMbps >= cfg.MinUpMbps
	var errMsg string
	if !available {
		errMsg = fmt.Sprintf("带宽低于阈值: down=%.2f (>= %.2f) up=%.2f (>= %.2f)",
			res.DownMbps, cfg.MinDownMbps, res.UpMbps, cfg.MinUpMbps)
	}
	return TestResult{
		Available:   available,
		Mode:        "bandwidth",
		DownMbps:    res.DownMbps,
		UpMbps:      res.UpMbps,
		ElapsedMs:   elapsedMs,
		MinDownMbps: cfg.MinDownMbps,
		MinUpMbps:   cfg.MinUpMbps,
		Error:       errMsg,
	}
}

// TestSpeedtestStream 单节点快速测速 SSE 档:基准端点(Cloudflare __down/__up 就近 POP)
// + 既有固定时长采样流(Sample 帧契约与 TestBandwidthStream 完全一致,弹框 UX 不变)。
// 与 legacy 档的差异仅在端点选择:legacy 优先配置 URL,本档锁死基准点(唯一事实源)。
func (d *Detector) TestSpeedtestStream(ctx context.Context, node *subscription.Node, onSample func(Sample)) TestResult {
	return d.streamBandwidthTest(ctx, node, downloadFallbackURLs, upstreamUploadURL, onSample)
}
