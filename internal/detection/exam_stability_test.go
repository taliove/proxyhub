package detection

import (
	"context"
	"math"
	"testing"
	"time"
)

func floatEq(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

// okSamples 构造全成功样本(latency 序列),供指标测试复用。
func okSamples(latencies ...int) []StabilitySample {
	out := make([]StabilitySample, len(latencies))
	for i, l := range latencies {
		out[i] = StabilitySample{Seq: i, ElapsedMs: i * 1000, LatencyMs: l, OK: true}
	}
	return out
}

func TestComputeStabilityMetrics_ZeroSamples(t *testing.T) {
	m := computeStabilityMetrics(nil)
	if m.Total != 0 || m.Succeeded != 0 {
		t.Fatalf("counts = %d/%d, want 0/0", m.Succeeded, m.Total)
	}
	if m.Score != 0 {
		t.Errorf("score = %d, want 0 for zero samples", m.Score)
	}
	if m.LossRate != 0 {
		t.Errorf("loss = %v, want 0 for zero samples", m.LossRate)
	}
}

func TestComputeStabilityMetrics_AllLoss(t *testing.T) {
	samples := []StabilitySample{
		{Seq: 0, OK: false},
		{Seq: 1, OK: false},
		{Seq: 2, OK: false},
	}
	m := computeStabilityMetrics(samples)
	if m.Total != 3 || m.Succeeded != 0 {
		t.Fatalf("counts = %d/%d, want 0/3", m.Succeeded, m.Total)
	}
	if !floatEq(m.LossRate, 1.0) {
		t.Errorf("loss = %v, want 1.0", m.LossRate)
	}
	if m.Score != 0 {
		t.Errorf("score = %d, want 0 for all-loss", m.Score)
	}
	if m.P95Ms != 0 || m.JitterMs != 0 {
		t.Errorf("latency stats should be 0 when no success, got p95=%v jitter=%v", m.P95Ms, m.JitterMs)
	}
}

func TestComputeStabilityMetrics_UniformLatency(t *testing.T) {
	// 10 个样本全成功,延迟恒定 50ms:丢包 0、抖动 0、P95=50。
	m := computeStabilityMetrics(okSamples(50, 50, 50, 50, 50, 50, 50, 50, 50, 50))
	if !floatEq(m.MeanMs, 50) || !floatEq(m.MedianMs, 50) {
		t.Errorf("mean/median = %v/%v, want 50/50", m.MeanMs, m.MedianMs)
	}
	if !floatEq(m.P95Ms, 50) || !floatEq(m.P99Ms, 50) {
		t.Errorf("p95/p99 = %v/%v, want 50/50", m.P95Ms, m.P99Ms)
	}
	if !floatEq(m.JitterMs, 0) {
		t.Errorf("jitter = %v, want 0", m.JitterMs)
	}
	// score = round(100*(0.5*1 + 0.25*1 + 0.25*(1-50/800))) = round(98.4375) = 98
	if m.Score != 98 {
		t.Errorf("score = %d, want 98", m.Score)
	}
}

func TestComputeStabilityMetrics_Percentiles(t *testing.T) {
	// 100 个样本延迟 1..100,全成功。
	lat := make([]int, 100)
	for i := range lat {
		lat[i] = i + 1
	}
	m := computeStabilityMetrics(okSamples(lat...))
	// 最近秩法:P95 -> ceil(0.95*100)=95 -> 第 95 个值 = 95;P99 -> 99。
	if !floatEq(m.P95Ms, 95) {
		t.Errorf("p95 = %v, want 95", m.P95Ms)
	}
	if !floatEq(m.P99Ms, 99) {
		t.Errorf("p99 = %v, want 99", m.P99Ms)
	}
	// 偶数个中位数 = (50+51)/2
	if !floatEq(m.MedianMs, 50.5) {
		t.Errorf("median = %v, want 50.5", m.MedianMs)
	}
	// 相邻差恒为 1 -> 抖动 1
	if !floatEq(m.JitterMs, 1) {
		t.Errorf("jitter = %v, want 1", m.JitterMs)
	}
}

func TestComputeStabilityMetrics_Jitter(t *testing.T) {
	// 成功延迟 [10,20,15]:|20-10|+|15-20| = 15,均值 7.5。
	m := computeStabilityMetrics(okSamples(10, 20, 15))
	if !floatEq(m.JitterMs, 7.5) {
		t.Errorf("jitter = %v, want 7.5", m.JitterMs)
	}
}

func TestComputeStabilityMetrics_PartialLoss(t *testing.T) {
	// 4 个样本,1 个丢包 -> 丢包率 0.25;统计只在成功样本上。
	samples := []StabilitySample{
		{Seq: 0, LatencyMs: 40, OK: true},
		{Seq: 1, OK: false},
		{Seq: 2, LatencyMs: 60, OK: true},
		{Seq: 3, LatencyMs: 50, OK: true},
	}
	m := computeStabilityMetrics(samples)
	if !floatEq(m.LossRate, 0.25) {
		t.Errorf("loss = %v, want 0.25", m.LossRate)
	}
	if m.Succeeded != 3 || m.Total != 4 {
		t.Errorf("counts = %d/%d, want 3/4", m.Succeeded, m.Total)
	}
	if !floatEq(m.MeanMs, 50) {
		t.Errorf("mean = %v, want 50", m.MeanMs)
	}
	// 抖动只跨成功样本子序列:|60-40|+|50-60| = 30,均值 15。
	if !floatEq(m.JitterMs, 15) {
		t.Errorf("jitter = %v, want 15", m.JitterMs)
	}
}

// fakeClock 虚拟时钟:now 返回当前,sleep 推进时间(不真正阻塞)。
type fakeClock struct{ cur time.Time }

func (c *fakeClock) now() time.Time        { return c.cur }
func (c *fakeClock) sleep(d time.Duration) { c.cur = c.cur.Add(d) }

// clock 构造采样器时钟:wait 推进虚拟时间;ctx 已取消则不推进并返回 false。
func (c *fakeClock) clock() samplerClock {
	return samplerClock{
		now: c.now,
		wait: func(ctx context.Context, d time.Duration) bool {
			if ctx.Err() != nil {
				return false
			}
			c.sleep(d)
			return true
		},
	}
}

func TestRunStabilitySampler_VirtualClock(t *testing.T) {
	fc := &fakeClock{cur: time.Unix(0, 0)}
	interval := time.Second

	// 假 HTTP 探测:按 canned 顺序返回,探测本身推进虚拟时钟(模拟 RTT / 超时)。
	canned := []struct {
		lat int
		ok  bool
	}{
		{50, true},
		{60, true},
		{300, false}, // 丢包(耗时 300ms 后判失败)
		{40, true},
	}
	idx := 0
	probe := func(_ context.Context) (int, bool) {
		c := canned[idx]
		idx++
		fc.sleep(time.Duration(c.lat) * time.Millisecond)
		if c.ok {
			return c.lat, true
		}
		return 0, false
	}

	var emitted []StabilitySample
	samples := runStabilitySampler(context.Background(), len(canned), interval,
		fc.clock(), probe,
		func(s StabilitySample) { emitted = append(emitted, s) })

	if len(samples) != 4 {
		t.Fatalf("samples = %d, want 4", len(samples))
	}
	if len(emitted) != 4 {
		t.Errorf("emitted = %d, want 4 (one callback per sample)", len(emitted))
	}
	wantElapsed := []int{50, 1060, 2300, 3040}
	wantOK := []bool{true, true, false, true}
	for i, s := range samples {
		if s.Seq != i {
			t.Errorf("sample[%d].Seq = %d, want %d", i, s.Seq, i)
		}
		if s.ElapsedMs != wantElapsed[i] {
			t.Errorf("sample[%d].ElapsedMs = %d, want %d", i, s.ElapsedMs, wantElapsed[i])
		}
		if s.OK != wantOK[i] {
			t.Errorf("sample[%d].OK = %v, want %v", i, s.OK, wantOK[i])
		}
	}
}

func TestRunStabilitySampler_ContextCancel(t *testing.T) {
	fc := &fakeClock{cur: time.Unix(0, 0)}
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	probe := func(_ context.Context) (int, bool) {
		calls++
		if calls == 2 {
			cancel() // 第二次探测后取消
		}
		return 10, true
	}
	samples := runStabilitySampler(ctx, 10, time.Second,
		fc.clock(), probe, nil)
	if len(samples) > 3 {
		t.Errorf("samples = %d, want <=3 after cancel", len(samples))
	}
}
