// Package nodemon 订阅节点监控(ADR 0047 / issue #99):对"用户订阅实际下发的
// 节点集合"做周期 TCP 探活,打点落库,供告警状态机(issue #100)与前端展示
// (issue #103)消费。
//
// 设计要点:
//   - 纯 TCP 连接探测(无 ICMP,免 CAP_NET_RAW 部署变更),与现有健康检查同源;
//   - 监控集合由 TargetProvider(server)现算:各订阅地址实际下发节点的并集,
//     排除可用性过滤(否则节点一宕就掉出集合,永远等不到恢复探测);
//   - 跨用户按 node_key 物理去重,同一节点只探一次;
//   - 总开关 subscription_monitor_enabled 默认关(零回归),间隔
//     monitor_interval_sec 默认 300s,均每轮重读,免重启生效。
package nodemon

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// Target 一个待探测节点(物理身份 = node_key;UserID 记录归属,告警按用户分发用)
type Target struct {
	UserID  int64
	NodeKey string
	Server  string
	Port    int
}

// Sample 一条探测打点
type Sample struct {
	NodeKey   string
	OK        bool
	LatencyMs int
	CheckedAt time.Time
}

// TargetProvider 监控集合提供者(由 server 实现,现算下发并集)
type TargetProvider interface {
	MonitorTargets() []Target
}

// SampleSink 打点落库(由 store 实现)
type SampleSink interface {
	SaveMonitorSample(nodeKey string, ok bool, latencyMs int, checkedAt time.Time) error
	// PruneMonitorSamples 清理早于 before 的打点(每轮调用,保留窗口外数据)
	PruneMonitorSamples(before time.Time) error
}

// Alerter 告警出口(复用 alert.Alerter 的飞书通道)
type Alerter interface {
	Alert(title, content string) error
}

// SettingsSource 动态读取全局设置(开关/间隔,免重启)
type SettingsSource interface {
	GetSetting(key string) (string, error)
}

// Listener 探测结果订阅(issue #100 告警状态机挂这里);nil 安全
type Listener interface {
	OnProbeResult(t Target, s Sample)
}

const (
	// DefaultIntervalSec 默认探测间隔(5 分钟)
	DefaultIntervalSec = 300
	// probeTimeoutSec 单节点 TCP 连接超时
	probeTimeoutSec = 5
	// fuseThreshold 监控集合保险丝:超过即飞书提示一次(不静默截断,照常探测)
	fuseThreshold = 200
	// sampleRetention 打点保留窗口
	sampleRetention = 7 * 24 * time.Hour
	// probeWorkers 并发探测上限
	probeWorkers = 16
)

// Monitor 监控调度器
type Monitor struct {
	settings SettingsSource
	provider TargetProvider
	sink     SampleSink
	alerter  Alerter
	logger   *slog.Logger

	listener atomic.Value // Listener(issue #100 挂载点);存 *Listener,nil 安全

	fuseNotified bool // 保险丝已提示(集合回落后复位,可再次提示)
}

// New 创建监控器。provider/listener 后补(SetProvider/SetListener),
// 便于 main.go 按构造顺序接线(server 依赖先构造);两者都应在 Run 之前挂好。
func New(settings SettingsSource, sink SampleSink, alerter Alerter, logger *slog.Logger) *Monitor {
	return &Monitor{settings: settings, sink: sink, alerter: alerter, logger: logger}
}

// SetProvider 注入监控集合提供者。必须在 Run 之前调用(无锁,runRound 裸读)。
func (m *Monitor) SetProvider(p TargetProvider) { m.provider = p }

// SetListener 挂载探测结果订阅(issue #100 告警状态机)。atomic.Value 保护,
// Run 前后挂载均安全;但语义上应在 Run 之前挂好,否则前几轮结果不分发。
func (m *Monitor) SetListener(l Listener) { m.listener.Store(&l) }

// enabled 读总开关;未配置/读失败 = 关(零回归)
func (m *Monitor) enabled() bool {
	if m.settings == nil {
		return false
	}
	v, err := m.settings.GetSetting("subscription_monitor_enabled")
	return err == nil && v == "true"
}

// interval 读探测间隔;非法/缺失回退默认 300s,低于 30s 收敛到 30s 下限。
func (m *Monitor) interval() time.Duration {
	if m.settings != nil {
		if v, err := m.settings.GetSetting("monitor_interval_sec"); err == nil {
			if n, perr := strconv.Atoi(v); perr == nil && n > 0 {
				if n < 30 {
					n = 30 // 下限:更密的探测对机场侧无意义
				}
				return time.Duration(n) * time.Second
			}
		}
	}
	return DefaultIntervalSec * time.Second
}

// Run 主循环:每轮重读开关与间隔(免重启)。开关关闭时按默认间隔空转待命。
func (m *Monitor) Run(ctx context.Context) {
	for {
		interval := m.interval()
		if m.enabled() {
			m.runRound(ctx)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}

// runRound 单轮:取集合 → 物理去重探测 → 打点(物理维度一次)→ 按 (用户,节点)
// 扇出分发 listener(同一节点多用户各自走状态机/告警,issue #100)。
func (m *Monitor) runRound(ctx context.Context) {
	if m.provider == nil || m.sink == nil {
		return
	}
	all := m.provider.MonitorTargets()
	targets := dedupeTargets(all)
	if len(targets) == 0 {
		return
	}

	// 保险丝:集合过大提示一次(照常探测,不截断),回落后复位
	if len(targets) > fuseThreshold && !m.fuseNotified {
		m.fuseNotified = true
		m.alert("订阅节点监控集合过大",
			fmt.Sprintf("当前监控节点 %d 个，超过阈值 %d。请检查订阅地址的筛选配置（未精选的地址会把整个池纳入监控）。", len(targets), fuseThreshold))
	} else if len(targets) <= fuseThreshold {
		m.fuseNotified = false
	}

	results := probeAll(ctx, targets, probeTimeoutSec*time.Second)
	sampleByKey := make(map[string]Sample, len(results))

	// 打点落库失败聚合成一条日志(持久 DB 故障时避免每轮 200+ 条 Warn 刷屏)
	saveFailures := 0
	for _, r := range results {
		sampleByKey[r.target.NodeKey] = r.sample
		// 用探测时刻(而非轮次开始时刻)落库,与 listener 收到的时钟一致
		if err := m.sink.SaveMonitorSample(r.target.NodeKey, r.sample.OK, r.sample.LatencyMs, r.sample.CheckedAt); err != nil {
			saveFailures++
		}
	}
	if saveFailures > 0 {
		m.logger.Warn("save monitor samples failed", "failures", saveFailures, "total", len(results))
	}
	if err := m.sink.PruneMonitorSamples(time.Now().Add(-sampleRetention)); err != nil {
		m.logger.Warn("prune monitor samples failed", "error", err)
	}

	var lis Listener
	if v := m.listener.Load(); v != nil {
		lis = *v.(*Listener)
	}
	if lis != nil {
		// 按 (用户,节点) 去重再分发:同用户多个地址命中同一节点时一轮只计
		// 一次探测结果——否则一轮失败即计入 N 次,状态机"连续 3 败"形同虚设
		for _, t := range dedupeByUserKey(all) {
			if smp, ok := sampleByKey[t.NodeKey]; ok {
				lis.OnProbeResult(t, smp)
			}
		}
	}
	m.logger.Info("monitor round finished", "targets", len(targets),
		"up", countOK(results), "down", len(results)-countOK(results))
}

func (m *Monitor) alert(title, content string) {
	if m.alerter == nil {
		return
	}
	if err := m.alerter.Alert(title, content); err != nil {
		m.logger.Warn("send monitor alert failed", "title", title, "error", err)
	}
}

// dedupeTargets 跨用户物理去重:同 node_key 只探一次(保留首个归属;
// 按用户的告警分发在 issue #100 另行按归属展开)
func dedupeTargets(targets []Target) []Target {
	seen := make(map[string]bool, len(targets))
	out := make([]Target, 0, len(targets))
	for _, t := range targets {
		if seen[t.NodeKey] {
			continue
		}
		seen[t.NodeKey] = true
		out = append(out, t)
	}
	return out
}

// dedupeByUserKey 按 (用户,节点) 去重(listener 扇出用):物理探测按 node_key
// 去重(dedupeTargets),告警分发按归属展开,但同用户同节点只算一份。
func dedupeByUserKey(targets []Target) []Target {
	type uk struct {
		u int64
		k string
	}
	seen := make(map[uk]bool, len(targets))
	out := make([]Target, 0, len(targets))
	for _, t := range targets {
		key := uk{t.UserID, t.NodeKey}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, t)
	}
	return out
}

type probeResult struct {
	target Target
	sample Sample
}

func countOK(results []probeResult) int {
	n := 0
	for _, r := range results {
		if r.sample.OK {
			n++
		}
	}
	return n
}

// probeAll 并发 TCP 探测(worker 池,带 ctx 取消)
func probeAll(ctx context.Context, targets []Target, timeout time.Duration) []probeResult {
	jobs := make(chan Target)
	results := make(chan probeResult, len(targets))
	var wg sync.WaitGroup
	for i := 0; i < probeWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for t := range jobs {
				ok, latency := probeTCP(ctx, t.Server, t.Port, timeout)
				results <- probeResult{target: t, sample: Sample{
					NodeKey: t.NodeKey, OK: ok, LatencyMs: latency, CheckedAt: time.Now(),
				}}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, t := range targets {
			select {
			case <-ctx.Done():
				return
			case jobs <- t:
			}
		}
	}()
	wg.Wait()
	close(results)

	out := make([]probeResult, 0, len(targets))
	for r := range results {
		out = append(out, r)
	}
	return out
}

// probeTCP 单次 TCP 连接探测:连通即成功,记录握手耗时
func probeTCP(ctx context.Context, server string, port int, timeout time.Duration) (bool, int) {
	d := net.Dialer{Timeout: timeout}
	start := time.Now()
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(server, strconv.Itoa(port)))
	if err != nil {
		return false, 0
	}
	_ = conn.Close()
	return true, int(time.Since(start).Milliseconds())
}
