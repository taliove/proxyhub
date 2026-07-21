package aggregator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/taliove/proxyhub/internal/config"
	"github.com/taliove/proxyhub/internal/filter"
	"github.com/taliove/proxyhub/internal/healthcheck"
	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

// ErrRefreshInProgress 已有一轮刷新在进行中
var ErrRefreshInProgress = errors.New("refresh already in progress")

// 刷新事件级别
const (
	levelInfo  = "info"
	levelWarn  = "warn"
	levelError = "error"
)

// 刷新流水线阶段
const (
	stageFetch  = "fetch"
	stageCheck  = "check"
	stageFilter = "filter"
	stageDone   = "done"
)

// Notifier 告警接口（便于测试注入）
type Notifier interface {
	AlertAirportDown(airportName string, totalNodes int) error
	AlertLowAvailability(available, threshold int) error
}

// Aggregator 订阅聚合调度器：拉取 → 检查 → 过滤 → 更新节点池 → 告警
type Aggregator struct {
	cfg        *config.Config
	fetcher    *subscription.Fetcher
	checker    *healthcheck.Checker
	filt       *filter.Filter
	alerter    Notifier
	st         *store.Store
	logger     *slog.Logger
	recognizer *store.RegionRecognizer // 地区识别器

	mu         sync.RWMutex
	nodes      []*subscription.Node
	lastUpdate time.Time

	// refreshing 保证同一时刻只有一轮刷新在跑
	refreshing atomic.Bool

	// 告警冷却：同一问题只告警一次，恢复后清除
	alerted map[string]bool
}

// New 创建聚合器。会从库里回填上一次成功聚合的节点池快照，
// 使进程重启后立即有节点可用，无需等待一轮刷新（见 ADR 0008）。
func New(cfg *config.Config, alerter Notifier, st *store.Store, logger *slog.Logger) *Aggregator {
	// 创建地区识别器
	recognizer, err := st.NewRegionRecognizer()
	if err != nil {
		logger.Warn("failed to load region recognizer, region recognition disabled", "error", err)
	}

	a := &Aggregator{
		cfg:     cfg,
		fetcher: subscription.NewFetcher(30 * time.Second),
		checker: healthcheck.NewChecker(
			cfg.HealthCheck.Timeout.Latency,
			cfg.HealthCheck.Timeout.Request,
			cfg.HealthCheck.TestURL,
			cfg.HealthCheck.Concurrent,
		),
		filt:       filter.NewFilter(cfg.Filter.NodesPerRegion, cfg.Filter.Deduplicate),
		alerter:    alerter,
		st:         st,
		logger:     logger,
		recognizer: recognizer,
		alerted:    make(map[string]bool),
	}
	a.restoreNodePool()
	return a
}

// restoreNodePool 用库里持久化的快照回填内存节点池。
// 读取失败或快照为空都不算错误——大不了等一次刷新，与旧行为一致。
func (a *Aggregator) restoreNodePool() {
	nodes, err := a.st.LoadNodePool()
	if err != nil {
		a.logger.Warn("restore node pool failed, starting empty", "error", err)
		return
	}
	if len(nodes) == 0 {
		return
	}
	a.mu.Lock()
	a.nodes = nodes
	a.mu.Unlock()
	a.logger.Info("node pool restored from snapshot", "count", len(nodes))
}

// Nodes 返回当前可用节点（含自建节点）
func (a *Aggregator) Nodes() []*subscription.Node {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.nodes
}

// LastUpdate 返回最近一次成功更新时间
func (a *Aggregator) LastUpdate() time.Time {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.lastUpdate
}

// UpdateNodeTestResult 将单节点即时测试结果写回内存池（按 NodeKey 匹配）。
// quick/real 更新 Available/Latency/DetectionLastCheck；bandwidth 更新带宽字段。
// 找到并更新返回 true；池中无此节点（如自建节点未入池）返回 false。
func (a *Aggregator) UpdateNodeTestResult(nodeKey, mode string, available bool, latency int, downMbps, upMbps float64) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	for _, n := range a.nodes {
		if n.NodeKey() != nodeKey {
			continue
		}
		if mode == "bandwidth" {
			n.BandwidthDownMbps = downMbps
			n.BandwidthUpMbps = upMbps
			n.BandwidthCheck = now
		} else {
			n.Available = available
			n.Latency = latency
			n.DetectionLastCheck = now
		}
		return true
	}
	return false
}

// UpdateNodeIdentity 按 NodeKey 更新内存池中节点的身份字段(名称/地区),
// 使重命名与地区回写立即反映在 /nodes 列表,不必等下一轮聚合刷新。
// name/region 为空表示"本次不改该字段"(region 回写只带 region,rename 只带 name)。
// 不可变语义:命中后替换切片中的节点对象(浅拷贝再改),不原地改写旧指针,
// 与 Nodes() 返回引用的并发读者隔离。找到并更新返回 true;池中无此节点返回 false。
func (a *Aggregator) UpdateNodeIdentity(nodeKey, name, region string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for i, n := range a.nodes {
		if n.NodeKey() != nodeKey {
			continue
		}
		updated := *n // 浅拷贝,避免原地改写旧对象
		if name != "" {
			updated.Name = name
		}
		if region != "" {
			updated.Region = region
		}
		a.nodes[i] = &updated
		return true
	}
	return false
}

// Run 定时执行聚合流水线
func (a *Aggregator) Run(ctx context.Context) {
	// 启动时立即跑一轮（除非“定时刷新”已关闭，见 ADR 0004）
	if a.autoRefreshEnabled() {
		if err := a.RunOnce(ctx, store.RefreshTriggerStartup); err != nil {
			a.logger.Error("initial aggregation failed", "error", err)
		}
	} else {
		a.logger.Info("scheduled refresh disabled, skipping startup aggregation")
	}

	ticker := time.NewTicker(a.cfg.HealthCheck.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// 每个 tick 重新读取开关，运行时切换下一个 tick 即生效
			if !a.autoRefreshEnabled() {
				a.logger.Debug("scheduled refresh disabled, skipping tick")
				continue
			}
			if err := a.RunOnce(ctx, store.RefreshTriggerScheduled); err != nil {
				if errors.Is(err, ErrRefreshInProgress) {
					a.logger.Info("scheduled aggregation skipped: refresh in progress")
				} else {
					a.logger.Error("aggregation failed", "error", err)
				}
			}
		}
	}
}

// autoRefreshEnabled 读取“定时刷新”开关（默认开）。只有显式设为 "false" 才关闭；
// 读取设置失败时按开处理（fail-open：宁可多刷一轮，也不因设置读不出而让刷新永久停摆）。
func (a *Aggregator) autoRefreshEnabled() bool {
	settings, err := a.st.GetSystemSettings()
	if err != nil {
		a.logger.Warn("get system settings failed, assuming scheduled refresh enabled", "error", err)
		return true
	}
	return settings["scheduled_refresh_enabled"] != "false"
}

// RunOnce 同步执行一轮完整的聚合流水线，并写入刷新记录
func (a *Aggregator) RunOnce(ctx context.Context, trigger string) error {
	if !a.refreshing.CompareAndSwap(false, true) {
		return ErrRefreshInProgress
	}
	defer a.refreshing.Store(false)

	rl, err := a.newRunLog(trigger)
	if err != nil {
		// 刷新记录写不进去不阻断聚合，仅丢失本次日志
		a.logger.Warn("create refresh run failed, continuing without refresh log", "error", err)
	}
	a.execute(ctx, rl)
	return nil
}

// TriggerRefresh 异步启动一轮刷新，立即返回刷新记录 ID
func (a *Aggregator) TriggerRefresh(ctx context.Context, trigger string) (int64, error) {
	if !a.refreshing.CompareAndSwap(false, true) {
		return 0, ErrRefreshInProgress
	}

	// 手动刷新必须拿到记录 ID 供前端轮询，创建失败即整体失败
	rl, err := a.newRunLog(trigger)
	if err != nil {
		a.refreshing.Store(false)
		return 0, fmt.Errorf("create refresh run: %w", err)
	}

	go func() {
		defer a.refreshing.Store(false)
		// 请求上下文会随响应结束被取消，异步刷新使用独立上下文
		a.execute(context.Background(), rl)
	}()

	return rl.runID, nil
}

// runLog 一次刷新的结构化事件记录器；runID 为 0 时所有写入降级为 no-op
type runLog struct {
	st     *store.Store
	logger *slog.Logger
	runID  int64
}

// newRunLog 创建刷新记录；失败时返回可安全使用的 no-op 记录器和错误，由调用方决定是否降级
func (a *Aggregator) newRunLog(trigger string) (*runLog, error) {
	rl := &runLog{st: a.st, logger: a.logger}
	run, err := a.st.CreateRefreshRun(trigger)
	if err != nil {
		return rl, err
	}
	rl.runID = run.ID
	return rl, nil
}

// event 写入一条事件；data 序列化为 JSON 附加信息
func (r *runLog) event(level, stage, message string, data map[string]any) {
	if r.runID == 0 {
		return
	}
	encoded := ""
	if len(data) > 0 {
		b, err := json.Marshal(data)
		if err != nil {
			r.logger.Warn("marshal refresh event data failed", "error", err, "stage", stage)
		} else {
			encoded = string(b)
		}
	}
	if err := r.st.AppendRefreshEvent(r.runID, level, stage, message, encoded); err != nil {
		r.logger.Warn("append refresh event failed", "error", err)
	}
}

// finish 写入刷新结果汇总
func (r *runLog) finish(status string, total, available, final int, errMsg string) {
	if r.runID == 0 {
		return
	}
	if err := r.st.FinishRefreshRun(r.runID, status, total, available, final, errMsg); err != nil {
		r.logger.Warn("finish refresh run failed", "error", err)
	}
}

// fetchResult 拉取阶段产出
type fetchResult struct {
	airportNodes map[string][]*subscription.Node // 按机场分组（拉取失败的为 nil），供告警判断
	allNodes     []*subscription.Node
	enabled      int // 启用的机场数
	failed       int // 拉取失败的机场数
}

// execute 聚合流水线：拉取 → 健康检查 → 过滤 → 注入自建节点 → 更新节点池 → 告警
func (a *Aggregator) execute(ctx context.Context, rl *runLog) {
	a.logger.Info("aggregation started")

	fetched, err := a.fetchAirports(rl)
	if err != nil {
		a.logger.Error("list airports failed", "error", err)
		rl.event(levelError, stageFetch, "读取机场列表失败："+err.Error(), nil)
		rl.finish(store.RefreshStatusFailed, 0, 0, 0, "读取机场列表失败")
		return
	}

	// 全量拉取失败 = 本轮没有任何数据,而非"节点都挂了"。此时保留现有节点池,
	// 避免一次网络抖动 / 机场临时不可达就把所有节点清空(见用户反馈:刷新失败清空节点)。
	if fetched.enabled > 0 && fetched.failed == fetched.enabled {
		a.mu.RLock()
		retained := len(a.nodes)
		a.mu.RUnlock()

		errMsg := fmt.Sprintf("%d/%d 机场拉取失败", fetched.failed, fetched.enabled)
		a.logger.Error("aggregation aborted: all airports failed, retaining existing pool",
			"failed", fetched.failed, "enabled", fetched.enabled, "retained", retained)
		rl.event(levelError, stageFetch,
			fmt.Sprintf("全部机场拉取失败，保留现有 %d 个节点，本轮不更新", retained),
			map[string]any{"retained": retained})
		rl.finish(store.RefreshStatusFailed, 0, 0, retained, errMsg)
		a.checkAlerts(fetched.airportNodes, 0)
		return
	}

	// 地区识别：从节点名提取地区代码，填充 Region 字段
	a.recognizeRegions(rl, fetched.allNodes)

	// 注入自建节点(常驻安全网)。放在健康检查之前，使其与机场节点一起被检测——
	// 自建节点的延迟/可用性才能反映真实状态，而非恒为「可用、延迟 0」。
	// 放在 recognizeRegions 之后，保留其 Region "SELF" 标记不被地区识别覆盖。
	allNodes := a.appendSelfHosted(rl, fetched.allNodes)

	// 健康检查：记录每个节点的可用性与延迟，但**不过滤**。
	// 节点池保留全量数据(包括不可用/慢/重复的)，过滤延迟到订阅生成时。
	available := a.checkHealth(ctx, rl, allNodes)

	// MergePool carry-forward：用旧池的检测状态（DetectionLastCheck/Available/Latency/带宽）
	// 覆盖新池（修复刷新抹掉真实检测结果的 bug），并标记消失节点为 stale。
	a.mu.RLock()
	oldPool := a.nodes
	a.mu.RUnlock()
	mergedPool := subscription.MergePool(oldPool, allNodes)

	// 应用覆盖层（机场节点的 display_name/region 编辑）
	if overrides, err := a.st.ListNodeOverrides(); err == nil {
		for _, n := range mergedPool {
			if n.Source == subscription.SourceSelfHosted {
				continue // 自建节点不走覆盖层
			}
			if override, exists := overrides[n.NodeKey()]; exists {
				if override.DisplayName != "" {
					n.DisplayName = override.DisplayName
				}
				if override.Region != "" {
					n.Region = override.Region
				}
			}
		}
	} else {
		a.logger.Warn("load node overrides failed", "error", err)
	}

	a.mu.Lock()
	a.nodes = mergedPool
	a.lastUpdate = time.Now()
	a.mu.Unlock()

	// 持久化节点池快照，供进程重启后回填内存池（见 ADR 0008）。
	// 写库失败只记日志、不阻断本轮聚合——内存池已更新，订阅照常可用。
	if err := a.st.SaveNodePool(mergedPool); err != nil {
		a.logger.Warn("persist node pool failed", "error", err)
	}

	a.logger.Info("aggregation finished",
		"total", len(fetched.allNodes), "available", len(available), "final", len(mergedPool))

	status := store.RefreshStatusSuccess
	errMsg := ""
	if fetched.failed > 0 {
		errMsg = fmt.Sprintf("%d/%d 机场拉取失败", fetched.failed, fetched.enabled)
		if fetched.failed == fetched.enabled {
			status = store.RefreshStatusFailed
		} else {
			status = store.RefreshStatusPartial
		}
	}
	rl.event(levelInfo, stageDone, fmt.Sprintf("刷新完成：最终节点池 %d 个(含不可用/慢速节点)", len(mergedPool)),
		map[string]any{"total": len(mergedPool), "available": len(available)})
	rl.finish(status, len(fetched.allNodes), len(available), len(mergedPool), errMsg)

	a.checkAlerts(fetched.airportNodes, len(available))
}

// fetchAirports 拉取全部启用机场的订阅；单个机场失败不阻断，仅计入失败数。
// 拉取按 fetchConcurrency() 的度并行(semaphore),结果按机场列表顺序归并,
// 不随完成序漂移(事件按完成序写,带时间戳,顺序乱属正常)。
func (a *Aggregator) fetchAirports(rl *runLog) (*fetchResult, error) {
	airports, err := a.st.ListAirports()
	if err != nil {
		return nil, err
	}

	var enabled []*store.Airport
	for _, airport := range airports {
		if airport.Enabled {
			enabled = append(enabled, airport)
		}
	}

	if len(enabled) == 0 {
		rl.event(levelWarn, stageFetch, "没有启用的机场，跳过拉取", nil)
	} else {
		rl.event(levelInfo, stageFetch, fmt.Sprintf("开始拉取 %d 个机场", len(enabled)), nil)
	}

	concurrency := a.fetchConcurrency()
	type outcome struct {
		nodes []*subscription.Node
		err   error
	}
	outcomes := make([]outcome, len(enabled))
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for i, airport := range enabled {
		wg.Add(1)
		go func(i int, airport *store.Airport) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			rl.event(levelInfo, stageFetch, fmt.Sprintf("拉取「%s」…", airport.Name), nil)
			sub, err := a.fetcher.Fetch(airport.Name, airport.URL)
			if err != nil {
				a.logger.Warn("fetch airport failed", "airport", airport.Name, "error", err)
				rl.event(levelWarn, stageFetch, fmt.Sprintf("「%s」拉取失败：%s", airport.Name, err.Error()),
					map[string]any{"airport": airport.Name})
				outcomes[i] = outcome{err: err}
				return
			}
			a.logger.Info("fetched airport", "airport", airport.Name, "nodes", len(sub.Nodes))
			rl.event(levelInfo, stageFetch, fmt.Sprintf("「%s」拉取成功，%d 个节点", airport.Name, len(sub.Nodes)),
				map[string]any{"airport": airport.Name, "nodes": len(sub.Nodes)})
			outcomes[i] = outcome{nodes: sub.Nodes}
		}(i, airport)
	}
	wg.Wait()

	result := &fetchResult{
		airportNodes: make(map[string][]*subscription.Node),
		enabled:      len(enabled),
	}
	for i, airport := range enabled {
		o := outcomes[i]
		if o.err != nil {
			result.airportNodes[airport.Name] = nil
			result.failed++
			continue
		}
		result.airportNodes[airport.Name] = o.nodes
		result.allNodes = append(result.allNodes, o.nodes...)
	}
	return result, nil
}

// 机场拉取并行度的取值边界(系统设置 fetch_concurrency,见 ticket 02)。
const (
	defaultFetchConcurrency = 4
	minFetchConcurrency     = 1
	maxFetchConcurrency     = 10
)

// fetchConcurrency 读取系统设置里的机场拉取并行度。
// 缺失/非法值回退默认 4;越界 clamp 到 [1,10](1 = 退化为串行,与旧版行为一致)。
func (a *Aggregator) fetchConcurrency() int {
	settings, err := a.st.GetSystemSettings()
	if err != nil {
		a.logger.Warn("get system settings failed, using default fetch concurrency", "error", err)
		return defaultFetchConcurrency
	}
	n, err := strconv.Atoi(settings["fetch_concurrency"])
	if err != nil {
		return defaultFetchConcurrency
	}
	if n < minFetchConcurrency {
		return minFetchConcurrency
	}
	if n > maxFetchConcurrency {
		return maxFetchConcurrency
	}
	return n
}

// checkHealth 健康检查所有节点,记录延迟到每个节点对象及 DB,返回可用节点列表(仅供告警统计)。
// **不过滤节点** — 节点池保留全量数据,过滤延迟到订阅生成时。
// **仅更新 Latency,不改 Available**(Available 由真实检测控制;从未检测的节点用 TCP 连通性降级判定)。
func (a *Aggregator) checkHealth(ctx context.Context, rl *runLog, allNodes []*subscription.Node) (available []*subscription.Node) {
	rl.event(levelInfo, stageCheck, fmt.Sprintf("开始健康检查，共 %d 个节点", len(allNodes)), nil)
	checkStart := time.Now()
	results := a.checker.CheckAll(ctx, allNodes)
	healthRecords := make([]store.HealthRecord, 0, len(results))
	for _, r := range results {
		// 只更新 Latency 和 LastCheck,不动 Available
		r.Node.Latency = r.Latency
		r.Node.LastCheck = time.Now()

		// 降级逻辑:如果节点从未做过真实检测(DetectionLastCheck 零值),用 TCP 连通性给个初始 Available
		if r.Node.DetectionLastCheck.IsZero() {
			r.Node.Available = r.Available
		}

		healthRecords = append(healthRecords, store.HealthRecord{
			NodeKey:   r.Node.NodeKey(),
			Name:      r.Node.Name,
			Source:    r.Node.Source,
			Available: r.Available, // 记录 TCP 连通性(历史查询用),但不影响节点 Available 字段
			LatencyMS: r.Latency,
		})
		// available 统计:优先用真实检测结果(DetectionLastCheck 非零),降级到 TCP
		if r.Node.DetectionLastCheck.IsZero() {
			if r.Available {
				available = append(available, r.Node)
			}
		} else {
			if r.Node.Available {
				available = append(available, r.Node)
			}
		}
	}
	if err := a.st.RecordHealth(healthRecords); err != nil {
		a.logger.Warn("record health failed", "error", err)
	}

	rl.event(levelInfo, stageCheck, fmt.Sprintf("健康检查完成：可用 %d/%d，耗时 %.1fs",
		len(available), len(allNodes), time.Since(checkStart).Seconds()),
		map[string]any{"available": len(available), "total": len(allNodes)})
	return available
}

// appendSelfHosted 注入自建节点（FailBack，常驻）。返回新切片，不修改入参。
// 节点由 store.SelfHostedNode.ToNode() 统一构造（与 serve-time 合并共用）；
// Available 交给随后的健康检查覆盖，反映真实状态。
func (a *Aggregator) appendSelfHosted(rl *runLog, nodes []*subscription.Node) []*subscription.Node {
	selfHostedNodes, err := a.st.ListSelfHostedNodes()
	if err != nil {
		a.logger.Warn("list self hosted nodes failed", "error", err)
		return nodes
	}
	if len(selfHostedNodes) == 0 {
		return nodes
	}
	result := make([]*subscription.Node, 0, len(nodes)+len(selfHostedNodes))
	result = append(result, nodes...)
	for _, shn := range selfHostedNodes {
		result = append(result, shn.ToNode())
	}
	rl.event(levelInfo, stageFilter, fmt.Sprintf("注入自建节点 %d 个", len(selfHostedNodes)), nil)
	return result
}

// checkAlerts 检查告警条件（带冷却：同一问题只告警一次，恢复后重置）
func (a *Aggregator) checkAlerts(airportNodes map[string][]*subscription.Node, availableCount int) {
	// 从数据库读取告警配置
	settings, err := a.st.GetSystemSettings()
	if err != nil {
		a.logger.Warn("get system settings failed", "error", err)
		return
	}

	feishuWebhook := settings["feishu_webhook"]
	minAvailableNodes := 10 // 默认值
	if val := settings["min_available_nodes"]; val != "" {
		if n, err := fmt.Sscanf(val, "%d", &minAvailableNodes); err == nil && n == 1 {
			// 解析成功
		}
	}

	// 跳过告警如果未配置 Webhook
	if a.alerter == nil || feishuWebhook == "" {
		return
	}

	// 机场级：某机场全部节点不可用（或拉取失败）
	for name, nodes := range airportNodes {
		key := "airport_down:" + name
		down := true
		for _, n := range nodes {
			if n.Available {
				down = false
				break
			}
		}

		if down {
			if !a.alerted[key] {
				if err := a.alerter.AlertAirportDown(name, len(nodes)); err != nil {
					a.logger.Warn("send alert failed", "error", err)
				} else {
					a.alerted[key] = true
				}
			}
		} else {
			delete(a.alerted, key)
		}
	}

	// 阈值级：可用节点总数不足
	key := "low_availability"
	if availableCount < minAvailableNodes {
		if !a.alerted[key] {
			if err := a.alerter.AlertLowAvailability(availableCount, minAvailableNodes); err != nil {
				a.logger.Warn("send alert failed", "error", err)
			} else {
				a.alerted[key] = true
			}
		}
	} else {
		delete(a.alerted, key)
	}
}

// recognizeRegions 识别全部节点的地区代码，填充 Region 字段
func (a *Aggregator) recognizeRegions(rl *runLog, nodes []*subscription.Node) {
	if a.recognizer == nil {
		return // 识别器加载失败，跳过
	}

	regionStats := make(map[string]int)
	for _, node := range nodes {
		node.Region = a.recognizer.Recognize(node.Name)
		regionStats[node.Region]++
	}

	// 记录识别统计
	rl.event(levelInfo, stageFetch, fmt.Sprintf("地区识别完成：%d 个节点分布于 %d 个地区",
		len(nodes), len(regionStats)), map[string]any{"regions": regionStats})
	a.logger.Info("region recognition completed", "nodes", len(nodes), "regions", len(regionStats), "stats", regionStats)
}
