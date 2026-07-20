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

// ExamReport 体检报告。稳定性段 + 多地域测速段 + 解锁段 + 出网信息段。
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
// 三段串行(稳定性 -> 多地域测速 -> 解锁)的结构在此预留,当前仅注册稳定性段。
type ExamOrchestrator struct {
	stages []examStage
}

// Run 串行执行各段(ctx 取消即停),完成后推 done 事件并返回报告。
func (o *ExamOrchestrator) Run(ctx context.Context, emit func(ExamEvent)) ExamReport {
	var report ExamReport
	for _, s := range o.stages {
		if ctx.Err() != nil {
			break
		}
		s.run(ctx, emit, &report)
	}
	emit(ExamEvent{Phase: "done", Report: &report})
	return report
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

	stages := []examStage{stabilityStage(cfg, realClock(), probe)}

	// 多地域测速段:独立节点会话串行测 8 区(解锁段位置保留,后续票据)。
	// 探测器工厂失败时走降级段(逐区 error 行),不静默跳过、也不发全局 error 打断前端。
	if rprobe, rerr := d.regionSpeedProbeFactory(node); rerr == nil {
		stages = append(stages, regionSpeedStage(examRegions, rprobe))
	} else {
		stages = append(stages, regionSpeedErrorStage(examRegions, rerr))
	}

	// 第三段:解锁判定(6 目标)与出网信息探测(IPv4/IPv6/DNS)同段并行,皆为小请求。
	// 各自独立节点会话;工厂失败时走各自降级段(逐项 error 行),与前两段同构,不静默跳过。
	// 合成一个并发段并入编排:从编排器看仍是稳定性段之后的单个串行段,不破坏稳定性段独占。
	var third []examStage
	if uprobe, uerr := d.unlockProbeFactory(node); uerr == nil {
		third = append(third, unlockStage(DefaultUnlockTargets(), uprobe, unlockSegmentTimeout))
	} else {
		third = append(third, unlockErrorStage(DefaultUnlockTargets(), uerr))
	}
	if eprobe, eerr := d.egressProbeFactory(node); eerr == nil {
		third = append(third, egressStage(eprobe, egressSegmentTimeout))
	} else {
		third = append(third, egressErrorStage(eerr))
	}
	stages = append(stages, concurrentStages("unlock_egress", third...))

	orch := &ExamOrchestrator{stages: stages}
	return orch.Run(ctx, emit)
}

// defaultStabilityProbe 生产探测器:从 node 构造一次 mihomo 会话(整场体检复用),
// 每次探测经该会话请求 generate_204,返回 RTT 与是否命中 204。
func (d *Detector) defaultStabilityProbe(node *subscription.Node) (StabilityProbe, error) {
	adapter, err := NewProxyAdapter(node)
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
