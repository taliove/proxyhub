package detection

import (
	"context"
	"math"
	"sort"
	"time"
)

// 稳定性评分权重与归一上限(评分越接近满分链路越稳)。
// 评分 = 100 * (wLoss*丢包分 + wJitter*抖动分 + wLatency*P95分),各分量归一到 0..1。
const (
	scoreWeightLoss    = 0.5
	scoreWeightJitter  = 0.25
	scoreWeightLatency = 0.25

	jitterNormMs  = 100.0 // 抖动 >= 此值抖动分归零
	latencyNormMs = 800.0 // P95 >= 此值延迟分归零
)

// StabilitySample 一次稳定性探测采样点。
type StabilitySample struct {
	Seq       int  `json:"seq"`        // 序号(从 0 起)
	ElapsedMs int  `json:"elapsed_ms"` // 自采样开始的累计毫秒
	LatencyMs int  `json:"latency_ms"` // 成功时的往返延迟;失败为 0
	OK        bool `json:"ok"`         // 探测是否成功(用于丢包率)
}

// StabilityMetrics 稳定性段聚合指标。
type StabilityMetrics struct {
	Total     int     `json:"total"`     // 总探测次数
	Succeeded int     `json:"succeeded"` // 成功次数
	LossRate  float64 `json:"loss_rate"` // 丢包率 0..1
	MeanMs    float64 `json:"mean_ms"`   // 成功样本平均延迟
	MedianMs  float64 `json:"median_ms"` // 成功样本中位延迟
	P95Ms     float64 `json:"p95_ms"`    // 成功样本 P95 延迟
	P99Ms     float64 `json:"p99_ms"`    // 成功样本 P99 延迟
	JitterMs  float64 `json:"jitter_ms"` // 相邻成功样本延迟差的平均绝对值
	Score     int     `json:"score"`     // 0..100 稳定性评分
}

// computeStabilityMetrics 从采样序列计算稳定性指标(纯函数,无 IO/时钟依赖)。
// 边界:零样本 -> 全零、评分 0;全丢包 -> 丢包率 1、评分 0、延迟统计 0。
func computeStabilityMetrics(samples []StabilitySample) StabilityMetrics {
	m := StabilityMetrics{Total: len(samples)}
	if m.Total == 0 {
		return m
	}

	// 成功样本延迟,保持原始顺序(抖动依赖相邻顺序)。
	ordered := make([]float64, 0, m.Total)
	for _, s := range samples {
		if s.OK {
			ordered = append(ordered, float64(s.LatencyMs))
		}
	}
	m.Succeeded = len(ordered)
	m.LossRate = float64(m.Total-m.Succeeded) / float64(m.Total)

	if m.Succeeded == 0 {
		return m // 全丢包:延迟统计与评分保持 0
	}

	m.MeanMs = mean(ordered)
	m.JitterMs = jitter(ordered)

	sorted := append([]float64(nil), ordered...)
	sort.Float64s(sorted)
	m.MedianMs = median(sorted)
	m.P95Ms = percentile(sorted, 95)
	m.P99Ms = percentile(sorted, 99)

	m.Score = stabilityScore(m)
	return m
}

// stabilityScore 由丢包/抖动/P95 三分量加权得 0..100 评分。
// 无成功样本时评分 0(由调用方保证 Succeeded>0 才进入)。
func stabilityScore(m StabilityMetrics) int {
	lossScore := 1 - m.LossRate
	jitterScore := clamp01(1 - m.JitterMs/jitterNormMs)
	latencyScore := clamp01(1 - m.P95Ms/latencyNormMs)
	raw := 100 * (scoreWeightLoss*lossScore + scoreWeightJitter*jitterScore + scoreWeightLatency*latencyScore)
	return int(math.Round(raw))
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func mean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	var sum float64
	for _, x := range xs {
		sum += x
	}
	return sum / float64(len(xs))
}

// median 对已排序切片求中位数(偶数个取中间两数均值)。
func median(sorted []float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2
}

// percentile 最近秩法:对已排序切片求 p 分位(idx = ceil(p/100*n)-1,钳位)。
func percentile(sorted []float64, p float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	idx := int(math.Ceil(p/100*float64(n))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= n {
		idx = n - 1
	}
	return sorted[idx]
}

// jitter 相邻元素差的平均绝对值(元素须按采样顺序)。
func jitter(ordered []float64) float64 {
	if len(ordered) < 2 {
		return 0
	}
	var sum float64
	for i := 1; i < len(ordered); i++ {
		sum += math.Abs(ordered[i] - ordered[i-1])
	}
	return sum / float64(len(ordered)-1)
}

// StabilityProbe 执行一次稳定性探测,返回往返延迟(毫秒)与是否成功。
type StabilityProbe func(ctx context.Context) (latencyMs int, ok bool)

// samplerClock 采样器的时钟依赖(生产用真实时钟,测试注入虚拟时钟)。
// wait 等待 d(或到 ctx 取消)返回:true 表示等满,false 表示被取消。
type samplerClock struct {
	now  func() time.Time
	wait func(ctx context.Context, d time.Duration) bool
}

func realClock() samplerClock {
	return samplerClock{
		now: time.Now,
		wait: func(ctx context.Context, d time.Duration) bool {
			t := time.NewTimer(d)
			defer t.Stop()
			select {
			case <-ctx.Done():
				return false
			case <-t.C:
				return true
			}
		},
	}
}

// runStabilitySampler 以 interval 间隔串行执行 count 次探测,每次采样即回调 onSample。
// 时钟与探测均可注入,便于虚拟时钟 + 假 HTTP client 单测。ctx 取消则提前返回已采样点。
func runStabilitySampler(
	ctx context.Context,
	count int,
	interval time.Duration,
	clk samplerClock,
	probe StabilityProbe,
	onSample func(StabilitySample),
) []StabilitySample {
	samples := make([]StabilitySample, 0, count)
	start := clk.now()

	for seq := 0; seq < count; seq++ {
		select {
		case <-ctx.Done():
			return samples
		default:
		}

		probeStart := clk.now()
		lat, ok := probe(ctx)
		if !ok {
			lat = 0
		}
		s := StabilitySample{
			Seq:       seq,
			ElapsedMs: int(clk.now().Sub(start).Milliseconds()),
			LatencyMs: lat,
			OK:        ok,
		}
		samples = append(samples, s)
		if onSample != nil {
			onSample(s)
		}

		// 对齐到下一个采样边界:补足本次探测未用满的间隔(最后一次不再等待)。
		// 等待期间响应 ctx 取消,避免客户端断连后仍空等满一个间隔。
		if seq < count-1 {
			if remain := interval - clk.now().Sub(probeStart); remain > 0 {
				if !clk.wait(ctx, remain) {
					return samples
				}
			}
		}
	}
	return samples
}
