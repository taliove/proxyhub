package detection

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/taliove/proxyhub/internal/subscription"
)

// Detector 节点解锁检测服务(两级:TCP 快筛 + 真实代理请求)
type Detector struct {
	nodeConcurrency int           // 节点级并发数
	tcpTimeout      time.Duration // TCP 快筛超时
	requestTimeout  time.Duration // 真实代理请求超时

	// bandwidthConfig 返回带宽测试配置(nil 时用默认值)。
	// 用函数注入而非固定字段:每次测试实时读取(如从 settings),且线程安全(无共享可变状态)。
	bandwidthConfig func() BandwidthConfig

	// examConfig 返回深度体检配置(nil 时用默认值),注入方式同 bandwidthConfig。
	examConfig func() ExamConfig

	// stabilityProbeFactory 为一次体检构造稳定性探测器(默认经 mihomo 会话请求 generate_204)。
	// 用工厂而非固定实现:每场体检独立会话,且测试可注入假探测器(不触真实网络)。
	stabilityProbeFactory func(*subscription.Node) (StabilityProbe, error)

	// regionSpeedProbeFactory 为一次体检构造多地域测速器(默认经 mihomo 会话下载各区 Linode 切片)。
	// 与稳定性同构:每场体检独立会话,测试可注入假探测器绕过真实网络。
	regionSpeedProbeFactory func(*subscription.Node) (RegionSpeedProbe, error)

	// unlockProbeFactory 为一次体检构造解锁探测器(默认经 mihomo 会话 + 注册表分发判定 6 个目标)。
	// 与稳定性/多地域同构:每场体检独立会话,测试可注入假探测器绕过真实网络。
	unlockProbeFactory func(*subscription.Node) (UnlockProbe, error)
}

// BandwidthConfig 带宽测试配置(可由 settings 覆盖)
type BandwidthConfig struct {
	DownURL         string  // 下行探测 URL(大小由 URL 的 bytes= 参数控制,作为数据上限)
	UpURL           string  // 上行探测 URL
	UpBytes         int     // 上行数据大小上限(字节),固定时长内的数据预算
	TestDurationSec int     // 单方向固定测速时长(秒):下行/上行都跑满这个时长,保证两条曲线等长
	TimeoutSec      int     // 单次带宽测试整体超时(秒)
	DirTimeoutSec   int     // 单方向硬超时上限(秒),防卡死;正常应先到 TestDurationSec
	MinDownMbps     float64 // 下行合格阈值
	MinUpMbps       float64 // 上行合格阈值
}

// DefaultBandwidthConfig 带宽测试默认配置。
// 固定时长 10s/方向:下行、上行都跑满 10s,两条曲线天然等长(横轴 0→10s)。
// 数据量(下行 URL 的 bytes=、上行 UpBytes)只是上限,须足够大以免快节点在 10s 内提前传完。
// DirTimeoutSec 是硬超时上限(防链路卡死),正常应先到 TestDurationSec 自然结束。
func DefaultBandwidthConfig() BandwidthConfig {
	return BandwidthConfig{
		DownURL:         "https://speed.cloudflare.com/__down?bytes=1073741824", // 1GB 上限(固定时长内基本不会传完)
		UpURL:           "https://speed.cloudflare.com/__up",
		UpBytes:         1024 * 1024 * 1024, // 1GB 上限
		TestDurationSec: 10,
		TimeoutSec:      60,
		DirTimeoutSec:   20,
		MinDownMbps:     1.0,
		MinUpMbps:       1.0,
	}
}

// NewDetector 创建检测服务
func NewDetector(nodeConcurrency int, tcpTimeout, requestTimeout time.Duration) *Detector {
	d := &Detector{
		nodeConcurrency: nodeConcurrency,
		tcpTimeout:      tcpTimeout,
		requestTimeout:  requestTimeout,
	}
	d.stabilityProbeFactory = d.defaultStabilityProbe
	d.regionSpeedProbeFactory = d.defaultRegionSpeedProbe
	d.unlockProbeFactory = d.defaultUnlockProbe
	return d
}

// SetBandwidthConfigProvider 注入带宽配置提供函数(每次带宽测试调用,返回实时配置)
func (d *Detector) SetBandwidthConfigProvider(provider func() BandwidthConfig) {
	d.bandwidthConfig = provider
}

// SetExamConfigProvider 注入体检配置提供函数(每场体检调用,返回实时配置)
func (d *Detector) SetExamConfigProvider(provider func() ExamConfig) {
	d.examConfig = provider
}

// SetStabilityProbeFactory 覆盖稳定性探测器工厂(测试用:注入假探测器绕过真实网络)。
func (d *Detector) SetStabilityProbeFactory(factory func(*subscription.Node) (StabilityProbe, error)) {
	d.stabilityProbeFactory = factory
}

// SetRegionSpeedProbeFactory 覆盖多地域测速器工厂(测试用:注入假探测器绕过真实网络)。
func (d *Detector) SetRegionSpeedProbeFactory(factory func(*subscription.Node) (RegionSpeedProbe, error)) {
	d.regionSpeedProbeFactory = factory
}

// SetUnlockProbeFactory 覆盖解锁探测器工厂(测试用:注入假探测器绕过真实网络)。
func (d *Detector) SetUnlockProbeFactory(factory func(*subscription.Node) (UnlockProbe, error)) {
	d.unlockProbeFactory = factory
}

// resolveBandwidthConfig 取带宽配置:有 provider 用之,否则用默认
func (d *Detector) resolveBandwidthConfig() BandwidthConfig {
	if d.bandwidthConfig != nil {
		return d.bandwidthConfig()
	}
	return DefaultBandwidthConfig()
}

// DetectAll 检测所有节点对所有目标的解锁情况
// 返回 map[nodeKey][]Result,每个节点一组结果(每个目标一条)
func (d *Detector) DetectAll(ctx context.Context, nodes []*subscription.Node, targets []Target) map[string][]Result {
	results := make(map[string][]Result)
	var mu sync.Mutex

	// 节点级并发控制
	sem := make(chan struct{}, d.nodeConcurrency)
	var wg sync.WaitGroup

loop:
	for _, node := range nodes {
		// 检查 context 是否已取消(label break 才能真正跳出 for)
		select {
		case <-ctx.Done():
			break loop
		default:
		}

		wg.Add(1)
		sem <- struct{}{}

		go func(n *subscription.Node) {
			defer wg.Done()
			defer func() { <-sem }()

			nodeResults := d.detectNode(ctx, n, targets)
			mu.Lock()
			results[n.NodeKey()] = nodeResults
			mu.Unlock()
		}(node)
	}

	wg.Wait()
	return results
}

// detectNode 检测单个节点对所有目标(串行,复用 adapter)
func (d *Detector) detectNode(ctx context.Context, node *subscription.Node, targets []Target) []Result {
	// 第一级:TCP 快筛
	if !d.tcpQuickCheck(ctx, node) {
		// TCP 不通,所有目标标记为不可用
		results := make([]Result, len(targets))
		for i, t := range targets {
			results[i] = Result{
				NodeKey:    node.NodeKey(),
				TargetName: t.Name,
				Available:  false,
				Latency:    0,
				Error:      "TCP connection failed",
			}
		}
		return results
	}

	// 构造 mihomo adapter(任务内复用)
	proxyAdapter, err := NewProxyAdapter(node)
	if err != nil {
		// adapter 构造失败,所有目标标记为不可用
		results := make([]Result, len(targets))
		for i, t := range targets {
			results[i] = Result{
				NodeKey:    node.NodeKey(),
				TargetName: t.Name,
				Available:  false,
				Latency:    0,
				Error:      fmt.Sprintf("create proxy adapter: %v", err),
			}
		}
		return results
	}

	// 第二级:真实代理请求(目标串行)
	results := make([]Result, len(targets))
	for i, target := range targets {
		// 检查 context 是否已取消
		select {
		case <-ctx.Done():
			results[i] = Result{
				NodeKey:    node.NodeKey(),
				TargetName: target.Name,
				Available:  false,
				Error:      "detection cancelled",
			}
			continue
		default:
		}

		results[i] = d.detectTarget(ctx, proxyAdapter, node, target)
	}

	return results
}

// tcpQuickCheck TCP 快筛:端口能否连通
func (d *Detector) tcpQuickCheck(ctx context.Context, node *subscription.Node) bool {
	ctx, cancel := context.WithTimeout(ctx, d.tcpTimeout)
	defer cancel()

	addr := fmt.Sprintf("%s:%d", node.Server, node.Port)
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// detectTarget 通过节点代理访问目标,判定解锁状态
func (d *Detector) detectTarget(ctx context.Context, proxyAdapter *ProxyAdapter, node *subscription.Node, target Target) Result {
	// 按 kind 分发:未知 kind 与未注册的专用 kind 在此拦截并明确报错(拒绝静默降级);
	// 已注册专用 kind 走对应 UnlockChecker;generic/空 kind 落到下方通用流程,行为与历史完全一致。
	kind, err := target.resolveKind()
	if err != nil {
		return targetErrorResult(node, target, err.Error())
	}
	if kind != KindGeneric {
		checker, err := specializedChecker(kind)
		if err != nil {
			return targetErrorResult(node, target, err.Error())
		}
		ctx, cancel := context.WithTimeout(ctx, d.requestTimeout)
		defer cancel()
		client := &http.Client{
			Transport: &http.Transport{DialContext: proxyAdapter.DialContext},
			Timeout:   d.requestTimeout,
		}
		return checker(ctx, client, node, target)
	}

	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, d.requestTimeout)
	defer cancel()

	result := Result{
		NodeKey:    node.NodeKey(),
		TargetName: target.Name,
		Available:  false,
		Latency:    0,
	}

	// 通过代理发送请求
	resp, err := d.requestViaProxy(ctx, proxyAdapter, target)
	if err != nil {
		result.Error = fmt.Sprintf("request failed: %v", err)
		return result
	}
	defer resp.Body.Close()

	latency := int(time.Since(start).Milliseconds())
	result.Latency = latency

	// 判定解锁状态
	unlocked, errMsg := checkUnlockStatus(resp, target)
	if !unlocked {
		result.Error = errMsg
		return result
	}

	// 通过所有检查
	result.Available = true
	result.Error = ""
	return result
}

// connectivityTarget 真实测试用的默认探测点(等价 detection_targets 里的 connectivity)。
var connectivityTarget = Target{
	Name:         "connectivity",
	URL:          "http://www.gstatic.com/generate_204",
	Method:       "GET",
	ExpectStatus: []int{204},
}

// TestNode 对单个节点做即时测试,供聚合检查/手动测试共用。
// mode="quick":仅 TCP 快筛 + 测延迟;mode="real":构 mihomo adapter 经代理请求 connectivity 目标;
// mode="bandwidth":测下行+上行带宽。
func (d *Detector) TestNode(ctx context.Context, node *subscription.Node, mode string) TestResult {
	switch mode {
	case "bandwidth":
		return d.testBandwidth(ctx, node)
	case "real":
		return d.testReal(ctx, node)
	default:
		return d.testQuick(ctx, node)
	}
}

func (d *Detector) testQuick(ctx context.Context, node *subscription.Node) TestResult {
	start := time.Now()
	if !d.tcpQuickCheck(ctx, node) {
		return TestResult{Available: false, Mode: "quick", Error: "TCP connection failed"}
	}
	latency := int(time.Since(start).Milliseconds())
	if latency == 0 {
		latency = 1
	}
	return TestResult{Available: true, Latency: latency, Mode: "quick"}
}

func (d *Detector) testReal(ctx context.Context, node *subscription.Node) TestResult {
	if !d.tcpQuickCheck(ctx, node) {
		return TestResult{Available: false, Mode: "real", Error: "TCP connection failed"}
	}
	adapter, err := NewProxyAdapter(node)
	if err != nil {
		return TestResult{Available: false, Mode: "real", Error: fmt.Sprintf("create proxy adapter: %v", err)}
	}
	res := d.detectTarget(ctx, adapter, node, connectivityTarget)
	return TestResult{Available: res.Available, Latency: res.Latency, Mode: "real", Error: res.Error}
}

// testBandwidth 带宽测试：下行+上行，任一方向失败或低于阈值则不可用。
// 配置来自 settings（缺省用 DefaultBandwidthConfig）。
func (d *Detector) testBandwidth(ctx context.Context, node *subscription.Node) TestResult {
	if !d.tcpQuickCheck(ctx, node) {
		return TestResult{Available: false, Mode: "bandwidth", Error: "TCP connection failed"}
	}

	adapter, err := NewProxyAdapter(node)
	if err != nil {
		return TestResult{Available: false, Mode: "bandwidth", Error: fmt.Sprintf("create proxy adapter: %v", err)}
	}

	cfg := d.resolveBandwidthConfig()

	// 带宽测试超时放宽（下行+上行需更长）
	ctx, cancel := context.WithTimeout(ctx, time.Duration(cfg.TimeoutSec)*time.Second)
	defer cancel()

	start := time.Now()
	elapsedMs := func() int { return int(time.Since(start).Milliseconds()) }

	// 下行测试
	downMbps, err := d.testDownload(ctx, adapter, cfg.DownURL)
	if err != nil {
		return TestResult{
			Available: false, Mode: "bandwidth", Error: fmt.Sprintf("下行测试失败: %v", err),
			ElapsedMs: elapsedMs(), MinDownMbps: cfg.MinDownMbps, MinUpMbps: cfg.MinUpMbps,
		}
	}

	// 上行测试
	upMbps, err := d.testUpload(ctx, adapter, cfg.UpURL, cfg.UpBytes)
	if err != nil {
		return TestResult{
			Available: false, Mode: "bandwidth", Error: fmt.Sprintf("上行测试失败: %v", err),
			DownMbps: downMbps, ElapsedMs: elapsedMs(), MinDownMbps: cfg.MinDownMbps, MinUpMbps: cfg.MinUpMbps,
		}
	}

	// 判定：两方向都 >= 阈值才可用
	available := downMbps >= cfg.MinDownMbps && upMbps >= cfg.MinUpMbps
	var errMsg string
	if !available {
		errMsg = fmt.Sprintf("带宽低于阈值: down=%.2f (>= %.2f) up=%.2f (>= %.2f)",
			downMbps, cfg.MinDownMbps, upMbps, cfg.MinUpMbps)
	}

	return TestResult{
		Available:   available,
		Latency:     0, // 带宽测试不测延迟
		Mode:        "bandwidth",
		DownMbps:    downMbps,
		UpMbps:      upMbps,
		ElapsedMs:   elapsedMs(),
		MinDownMbps: cfg.MinDownMbps,
		MinUpMbps:   cfg.MinUpMbps,
		Error:       errMsg,
	}
}

// testDownload 下行测试：经代理下载固定大小文件，计算 Mbps
func (d *Detector) testDownload(ctx context.Context, adapter *ProxyAdapter, url string) (float64, error) {
	start := time.Now()

	client := d.newProxyClient(adapter)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return 0, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("status %d", resp.StatusCode)
	}

	// 读取全部内容并计时
	written, err := io.Copy(io.Discard, resp.Body)
	if err != nil {
		return 0, err
	}

	elapsed := time.Since(start).Seconds()
	if elapsed == 0 {
		elapsed = 0.001 // 避免除零
	}

	mbps := float64(written*8) / elapsed / 1e6
	return mbps, nil
}

// testUpload 上行测试：经代理 POST 固定大小数据，计算 Mbps
func (d *Detector) testUpload(ctx context.Context, adapter *ProxyAdapter, url string, size int) (float64, error) {
	start := time.Now()

	// 生成随机数据（避免压缩干扰）
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i % 256)
	}

	client := d.newProxyClient(adapter)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
	if err != nil {
		return 0, err
	}
	req.ContentLength = int64(size)

	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	elapsed := time.Since(start).Seconds()
	if elapsed == 0 {
		elapsed = 0.001
	}

	mbps := float64(size*8) / elapsed / 1e6
	return mbps, nil
}

// newProxyClient 构造经代理的 HTTP 客户端（带宽测试专用，超时较长）
func (d *Detector) newProxyClient(adapter *ProxyAdapter) *http.Client {
	transport := &http.Transport{
		DialContext: adapter.DialContext,
	}
	return &http.Client{
		Transport: transport,
		Timeout:   60 * time.Second,
	}
}
