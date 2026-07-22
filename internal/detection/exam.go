package detection

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/taliove/proxyhub/internal/subscription"
)

// 深度体检默认参数。稳定性段:30s × 1Hz 探测 generate_204。
const (
	defaultStabilityDurationSec = 30
	defaultStabilityIntervalMs  = 1000
	defaultExamProbeURL         = "https://www.gstatic.com/generate_204"
	defaultExamProbeTimeoutSec  = 5
)

// ExamConfig 深度体检配置(稳定性段时长经 settings 可配,其余用默认)。
type ExamConfig struct {
	StabilityDurationSec int    // 稳定性采样时长(秒)
	StabilityIntervalMs  int    // 采样间隔(毫秒),1000 = 1Hz
	ProbeURL             string // 稳定性探测 URL
	ProbeTimeoutSec      int    // 单次探测超时(秒)
}

// DefaultExamConfig 体检默认配置。
func DefaultExamConfig() ExamConfig {
	return ExamConfig{
		StabilityDurationSec: defaultStabilityDurationSec,
		StabilityIntervalMs:  defaultStabilityIntervalMs,
		ProbeURL:             defaultExamProbeURL,
		ProbeTimeoutSec:      defaultExamProbeTimeoutSec,
	}
}

// stabilitySampleCount 由时长/间隔推出探测次数(至少 1)。
func (c ExamConfig) stabilitySampleCount() int {
	if c.StabilityIntervalMs <= 0 {
		return 1
	}
	n := c.StabilityDurationSec * 1000 / c.StabilityIntervalMs
	if n < 1 {
		n = 1
	}
	return n
}

// ExamReport 体检报告。出网信息段 + 稳定性段 + 多地域测速段 + 解锁段。
type ExamReport struct {
	Stability   *StabilityMetrics   `json:"stability,omitempty"`
	RegionSpeed *RegionSpeedMetrics `json:"region_speed,omitempty"`
	Unlock      *UnlockMetrics      `json:"unlock,omitempty"`
	Egress      *EgressMetrics      `json:"egress,omitempty"`
}

// ExamEvent 体检 SSE 事件(分段推送:sample / region / unlock / section_done / done / error)。
type ExamEvent struct {
	Phase        string              `json:"phase"`
	Section      string              `json:"section,omitempty"`
	Sample       *StabilitySample    `json:"sample,omitempty"`
	Metrics      *StabilityMetrics   `json:"metrics,omitempty"`
	Region       *RegionResult       `json:"region,omitempty"`
	RegionSpeed  *RegionSpeedMetrics `json:"region_speed,omitempty"`
	UnlockResult *Result             `json:"unlock_result,omitempty"`
	Unlock       *UnlockMetrics      `json:"unlock,omitempty"`
	Egress       *EgressMetrics      `json:"egress,omitempty"`
	Report       *ExamReport         `json:"report,omitempty"`
	Error        string              `json:"error,omitempty"`
}

// examStage 体检中的一个段:串行运行,独占会话(采样期间无并发段)。
type examStage struct {
	name string
	run  func(ctx context.Context, emit func(ExamEvent), report *ExamReport)
}

// ExamOrchestrator 体检编排器:按注册顺序串行执行各段,末尾推 done。
// 四段串行(出网信息 -> 稳定性 -> 多地域测速 -> 解锁),各段独占会话不重叠。
type ExamOrchestrator struct {
	stages []examStage
}

// Run 串行执行各段(ctx 取消即停),完成后推 done 事件并返回报告。
// 出网段全失败时短路:跳过后续段,推 error 终态帧,体检以"节点不可用"收口。
func (o *ExamOrchestrator) Run(ctx context.Context, emit func(ExamEvent)) ExamReport {
	var report ExamReport
	for _, s := range o.stages {
		if ctx.Err() != nil {
			break
		}
		s.run(ctx, emit, &report)

		// 出网段收口检查:三项全 error → 节点根本出不去,短路后续段。
		if s.name == "egress" && egressAllFailed(report.Egress) {
			emit(ExamEvent{Phase: "error", Error: "出网全失败: 节点不可用"})
			break
		}
	}
	emit(ExamEvent{Phase: "done", Report: &report})
	return report
}

// egressAllFailed 判定出网三项是否全部失败(节点根本出不去):IPv4/IPv6/DNS 三项皆 error。
// 注意:IPv6 不可达(Available=false, Error="")是明确负判定,不算失败。
func egressAllFailed(egress *EgressMetrics) bool {
	if egress == nil {
		return false
	}
	// IPv4 失败:Error 非空。
	ipv4Fail := egress.IPv4 != nil && egress.IPv4.Error != ""
	// IPv6 失败:不可达(Available=false)但 Error 非空才算失败;不可达但 Error 空是明确负判定,非失败。
	ipv6Fail := egress.IPv6 != nil && !egress.IPv6.Available && egress.IPv6.Error != ""
	// DNS 失败:Error 非空。
	dnsFail := egress.DNS != nil && egress.DNS.Error != ""

	return ipv4Fail && ipv6Fail && dnsFail
}

// stabilityStage 构造稳定性采样段:1Hz 探测,逐样本推 sample,段末推 section_done + 指标。
func stabilityStage(cfg ExamConfig, clk samplerClock, probe StabilityProbe) examStage {
	return examStage{
		name: "stability",
		run: func(ctx context.Context, emit func(ExamEvent), report *ExamReport) {
			count := cfg.stabilitySampleCount()
			interval := time.Duration(cfg.StabilityIntervalMs) * time.Millisecond

			samples := runStabilitySampler(ctx, count, interval, clk, probe, func(s StabilitySample) {
				sc := s
				emit(ExamEvent{Phase: "sample", Section: "stability", Sample: &sc})
			})

			metrics := computeStabilityMetrics(samples)
			report.Stability = &metrics
			mc := metrics
			emit(ExamEvent{Phase: "section_done", Section: "stability", Metrics: &mc})
		},
	}
}

// ExamStream 单节点深度体检(流式):复用一个代理会话,串行执行各段,通过 emit 实时推送事件。
func (d *Detector) ExamStream(ctx context.Context, node *subscription.Node, emit func(ExamEvent)) ExamReport {
	cfg := d.resolveExamConfig()

	probe, err := d.stabilityProbeFactory(node)
	if err != nil {
		emit(ExamEvent{Phase: "error", Error: fmt.Sprintf("create proxy session: %v", err)})
		return ExamReport{}
	}

	var stages []examStage

	// 第一段:出网信息(IPv4/IPv6/DNS)。前置以便用户 ~5s 内看到出口 IP/地区/DNS,死节点 fail-fast。
	// 出网段独占串行:跑完才进稳定性采样,不破坏采样独占。真实探针叠加网络类失败重试(withEgressRetry);
	// 工厂失败时走降级段(逐类 error 行),不静默跳过。
	if eprobe, eerr := d.egressProbeFactory(node); eerr == nil {
		stages = append(stages, egressStage(withEgressRetry(eprobe), egressSegmentTimeout))
	} else {
		stages = append(stages, egressErrorStage(eerr))
	}

	// 第二段:稳定性采样(1Hz 探测 generate_204),独占会话。
	stages = append(stages, stabilityStage(cfg, realClock(), probe))

	// 第三段:多地域测速。独立节点会话串行测 [基准 + 8 区]。基准行(Cloudflare 就近 POP)为第一行对照。
	// 真实探针叠加单区(含基准)失败重试(withRegionRetry);探测器工厂失败时走降级段(逐区 error 行),
	// 不静默跳过、也不发全局 error 打断前端。
	regions := examRegionsWithBaseline()
	if rprobe, rerr := d.regionSpeedProbeFactory(node); rerr == nil {
		stages = append(stages, regionSpeedStage(regions, withRegionRetry(rprobe)))
	} else {
		stages = append(stages, regionSpeedErrorStage(regions, rerr))
	}

	// 第四段:解锁判定(6 目标)。独立节点会话并行判定各目标。真实探针叠加网络类失败重试
	// (withUnlockRetry):传输抖动重试一次,判定结论绝不重试;工厂失败时走降级段(逐项 error 行,不重试)。
	if uprobe, uerr := d.unlockProbeFactory(node); uerr == nil {
		stages = append(stages, unlockStage(DefaultUnlockTargets(), withUnlockRetry(uprobe), unlockSegmentTimeout))
	} else {
		stages = append(stages, unlockErrorStage(DefaultUnlockTargets(), uerr))
	}

	orch := &ExamOrchestrator{stages: stages}
	return orch.Run(ctx, emit)
}

// defaultStabilityProbe 生产探测器:从 node 构造一次 mihomo 会话(整场体检复用),
// 每次探测经该会话请求 generate_204,返回 RTT 与是否命中 204。
func (d *Detector) defaultStabilityProbe(node *subscription.Node) (StabilityProbe, error) {
	adapter, err := d.newProxyAdapter(node)
	if err != nil {
		return nil, err
	}

	cfg := d.resolveExamConfig()
	timeout := time.Duration(cfg.ProbeTimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = defaultExamProbeTimeoutSec * time.Second
	}
	url := cfg.ProbeURL
	if url == "" {
		url = defaultExamProbeURL
	}

	// 单个 client 复用会话(连接复用),整场稳定性采样共用。
	client := &http.Client{
		Transport: &http.Transport{DialContext: adapter.DialContext},
		Timeout:   timeout,
	}

	return func(ctx context.Context) (int, bool) {
		start := time.Now()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return 0, false
		}
		req.Header.Set("User-Agent", bandwidthUserAgent)
		resp, err := client.Do(req)
		if err != nil {
			return 0, false
		}
		defer resp.Body.Close()
		io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		lat := int(time.Since(start).Milliseconds())
		return lat, resp.StatusCode == http.StatusNoContent
	}, nil
}

// resolveExamConfig 取体检配置:有 provider 用之,否则用默认。
func (d *Detector) resolveExamConfig() ExamConfig {
	if d.examConfig != nil {
		return d.examConfig()
	}
	return DefaultExamConfig()
}
