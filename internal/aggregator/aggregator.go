package aggregator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/taliove/proxyhub/internal/config"
	"github.com/taliove/proxyhub/internal/detection"
	"github.com/taliove/proxyhub/internal/filter"
	"github.com/taliove/proxyhub/internal/healthcheck"
	"github.com/taliove/proxyhub/internal/jobs"
	"github.com/taliove/proxyhub/internal/poolops"
	"github.com/taliove/proxyhub/internal/region"
	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

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
	// Alert 通用告警(名称槽位空槽/恢复等,issue #100)
	Alert(title, content string) error
}

// Aggregator 订阅聚合调度器：拉取 → 检查 → 过滤 → 更新节点池 → 告警
type Aggregator struct {
	cfg       *config.Config
	fetcher   *subscription.Fetcher
	checker   *healthcheck.Checker
	filt      *filter.Filter
	alerter   Notifier
	st        *store.Store
	logger    *slog.Logger
	regionRec *region.Recognizer // 统一三层地区识别器(issue #37)

	mu sync.RWMutex
	// pools 按属主用户分片的内存节点池(ticket 07):map[userID] -> 该用户的节点。
	// Invariant B(单管理员):未归属节点(user_id=0)在回填/合并时归一到超管分片,
	// 节点池按用户隔离,互不串台。分片 key 永远 >0(有超管时);无超管(初始化前)退化为 0 桶。
	pools map[int64][]*subscription.Node
	// lastUpdate 最近一次成功聚合时刻(进程级;分片化后仍按同一时刻记录,够用)。
	lastUpdate time.Time

	// refreshJobs 刷新任务运行时(jobs kind=refresh;单实例/取消/中断标记)。
	// 取代旧的 refreshing atomic 锁,互斥细化到机场级(见 refresh_job.go)。
	refreshJobs *jobs.Manager
	// poolOps 单机场 upsert 口径(单机场刷新复用,见 ticket 01/04)
	poolOps *poolops.StoreAdapter

	// refreshStartMu 串行化"冲突检查+发起任务"临界区(机场级互斥的 TOCTOU 防护;
	// 跨 kind 互斥同用:机场测试发起走 StartAirportTestExclusive,见 refresh_job.go)
	refreshStartMu sync.Mutex

	// airportTestConflict 跨 kind 互斥的测试侧查询回调(server 装配期注入,issue 0025);
	// nil 表示无测试运行时,冲突恒无。
	airportTestConflict func(airportID int64) (string, bool)

	// 告警冷却：同一问题只告警一次，恢复后清除
	alerted map[string]bool
}

// ownerUserID 机场/自建节点属主解析:行已带 user_id(>=1)时直接用;
// 未归属(0)时回退为首个 super_admin(Invariant B:单管理员部署里一切归超管)。
// 无用户(初始化前)回退 0(未归属桶)。
func (a *Aggregator) ownerUserID(rowUserID int64) int64 {
	if rowUserID > 0 {
		return rowUserID
	}
	if a == nil || a.st == nil {
		return 0
	}
	users, err := a.st.ListUsers()
	if err != nil {
		a.logger.Warn("list users for owner resolution failed, falling back to unowned shard", "error", err)
		return 0
	}
	for _, u := range users {
		if u.Role == store.RoleSuperAdmin {
			return u.ID
		}
	}
	return 0
}

// New 创建聚合器。会从库里回填上一次成功聚合的节点池快照，
// 使进程重启后立即有节点可用，无需等待一轮刷新（见 ADR 0008）。
func New(cfg *config.Config, alerter Notifier, st *store.Store, logger *slog.Logger) *Aggregator {
	// 统一三层地区识别器(issue #37):名称规则 -> 国旗 emoji 反解 -> GeoIP。
	// 各层构造失败只降级不报错;全量刷新与单机场 upsert 共用同一实例同一口径。
	regionRec := region.NewFromStore(st, logger)

	// 健康检查器:直连出口配置热读(settings 改后下一轮检查即生效,与检测主链路同一开关)。
	checker := healthcheck.NewChecker(
		cfg.HealthCheck.Timeout.Latency,
		cfg.HealthCheck.Timeout.Request,
		cfg.HealthCheck.TestURL,
		cfg.HealthCheck.Concurrent,
	)
	checker.SetDirectEgressConfigProvider(st.GetDirectEgressConfig)

	a := &Aggregator{
		cfg:       cfg,
		fetcher:   subscription.NewFetcher(30 * time.Second),
		checker:   checker,
		filt:      filter.NewFilter(cfg.Filter.NodesPerRegion, cfg.Filter.Deduplicate),
		alerter:   alerter,
		st:        st,
		logger:    logger,
		regionRec: regionRec,
		pools:     make(map[int64][]*subscription.Node),
		alerted:   make(map[string]bool),
	}
	// 刷新任务运行时:注册 refresh kind,恢复遗留 running 为 interrupted。
	// 用 RecoverOwn 而非 Recover:多 Manager 共存,不误标其他运行时续跑的任务。
	a.poolOps = poolops.NewStoreAdapter(st, regionRec)
	a.refreshJobs = jobs.NewManager(
		st.Jobs(),
		jobs.WithErrorHandler(func(err error) {
			logger.Warn("refresh job runtime error", "error", err)
		}),
	)
	a.refreshJobs.Register(&refreshKind{agg: a})
	if err := a.refreshJobs.RecoverOwn(); err != nil {
		logger.Error("refresh jobs recover failed", "error", err)
	}
	// 启动即收口上进程残留的 running 刷新记录(本进程还没开始跑,running 必是死进程残留)
	if err := st.FailRunningRefreshRuns("process restarted"); err != nil {
		logger.Warn("fail stale running refresh runs failed", "error", err)
	}
	a.restoreNodePool()
	return a
}

// restoreNodePool 用库里持久化的快照回填内存节点池。
// 读取失败或快照为空都不算错误——大不了等一次刷新，与旧行为一致。
// 回填时把未归属节点(user_id=0)归一到超管分片(Invariant B),与其他路径同一归一规则。
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
	for _, n := range nodes {
		shard := a.ownerUserID(n.UserID)
		n.UserID = shard
		a.pools[shard] = append(a.pools[shard], n)
	}
	a.mu.Unlock()
	a.logger.Info("node pool restored from snapshot", "count", len(nodes), "shards", len(a.pools))
}

// Nodes 返回合并池(所有分片,跨用户,旧语义)。仅供订阅生成/内部聚合用;
// 管理面 handler 必须改用 NodesForUser(ticket 07)。
func (a *Aggregator) Nodes() []*subscription.Node {
	return a.NodesForUser(0)
}

// NodesForUser 返回指定用户分片的节点池(ticket 07)。
// userID=0 返回合并池(与 Nodes 等价,跨用户旧语义)。
func (a *Aggregator) NodesForUser(userID int64) []*subscription.Node {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if userID > 0 {
		return a.pools[userID]
	}
	total := 0
	for _, p := range a.pools {
		total += len(p)
	}
	out := make([]*subscription.Node, 0, total)
	for _, p := range a.pools {
		out = append(out, p...)
	}
	return out
}

// SetNodesForUser 覆盖指定用户分片的节点池(ticket 07 测试/恢复用)。
// 传入 nil 或空切片表示清空该分片。不修改入参底层数组(复制持有)。
func (a *Aggregator) SetNodesForUser(userID int64, nodes []*subscription.Node) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(nodes) == 0 {
		delete(a.pools, userID)
		return
	}
	cp := make([]*subscription.Node, len(nodes))
	copy(cp, nodes)
	a.pools[userID] = cp
	a.lastUpdate = time.Now()
}

// LastUpdate 返回最近一次池写入时间(含成功刷新/全挂健康续命/取消部分合并;
// 拉取长期失败时请查刷新记录状态,此时间戳不区分写入来源)
func (a *Aggregator) LastUpdate() time.Time {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.lastUpdate
}

// LastUpdateForUser 返回指定用户分片最近刷新时刻(ticket 07)。
// 刷新时间戳按进程级记录(每次刷新更新同一时刻),userID 参数对齐接口,行为同 LastUpdate。
func (a *Aggregator) LastUpdateForUser(userID int64) time.Time {
	return a.LastUpdate()
}

// UpdateNodeTestResult 将单节点即时测试结果写回内存池（按 NodeKey 匹配）。
// quick/real 更新 Available/Latency/DetectionLastCheck 与失败原因(failReason/failDetail,
// 见 ticket 0017:失败填分类与截断详情,成功清空);bandwidth 更新带宽字段,不动失败原因。
// 找到并更新返回 true；池中无此节点（如自建节点未入池）返回 false。
func (a *Aggregator) UpdateNodeTestResult(nodeKey, mode string, available bool, latency int, downMbps, upMbps float64, failReason, failDetail string) bool {
	return a.UpdateNodeTestResultForUser(0, nodeKey, mode, available, latency, downMbps, upMbps, failReason, failDetail)
}

// UpdateNodeTestResultForUser 写回指定用户分片的节点(ticket 07);
// userID=0 回退为跨分片查找(单管理员/内部路径等价旧行为)。
//
// 命中即落库(issue #33):检测状态的持久化不再绑死在「全量刷新成功」上——
// 机场 URL 拉不通时,手动检测/批量检测/订阅实测写回的可用性不再随重启蒸发。
// 落库在锁外单行 UPDATE,失败只告警(内存池仍是事实源,下轮刷新兜底落库)。
func (a *Aggregator) UpdateNodeTestResultForUser(userID int64, nodeKey, mode string, available bool, latency int, downMbps, upMbps float64, failReason, failDetail string) bool {
	updated := a.updateNodeTestResultInPool(userID, nodeKey, mode, available, latency, downMbps, upMbps, failReason, failDetail)
	if updated == nil {
		return false
	}
	if err := a.st.UpdateNodeDetectionResult(updated, mode); err != nil {
		a.logger.Warn("persist node test result failed, memory pool updated only",
			"node", nodeKey, "mode", mode, "error", err)
	}
	return true
}

// updateNodeTestResultInPool 内存池写回(持锁):命中后浅拷贝替换并返回新对象
// (供锁外落库),未命中返回 nil。
func (a *Aggregator) updateNodeTestResultInPool(userID int64, nodeKey, mode string, available bool, latency int, downMbps, upMbps float64, failReason, failDetail string) *subscription.Node {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	for _, pool := range a.poolsFor(userID) {
		for i, n := range pool {
			if n.NodeKey() != nodeKey {
				continue
			}
			// 不可变语义:浅拷贝再改,与 UpdateNodeIdentity 一致,和 Nodes() 返回引用的并发读者隔离。
			updated := *n
			if mode == "bandwidth" {
				updated.BandwidthDownMbps = downMbps
				updated.BandwidthUpMbps = upMbps
				updated.BandwidthCheck = now
			} else {
				updated.Available = available
				updated.Latency = latency
				updated.DetectionLastCheck = now
				// 判定来源如实跟随本次测试类型:quick=TCP 快检,real=真实代理检测(ticket 0016)。
				// 不做单调升级——real 之后再 quick,Available 已被快检覆盖,来源须回落为 health。
				if mode == "real" {
					updated.DetectionKind = subscription.DetectionKindReal
				} else {
					updated.DetectionKind = subscription.DetectionKindHealth
				}
				// 失败原因:成功清空(不残留旧失败误导排障),失败记录分类+截断详情(ticket 0017)
				if available {
					updated.DetectionFailReason = ""
					updated.DetectionFailDetail = ""
				} else {
					updated.DetectionFailReason = failReason
					updated.DetectionFailDetail = detection.TruncateFailDetail(failDetail)
				}
			}
			pool[i] = &updated
			return &updated
		}
	}
	return nil
}

// poolsFor 选取要遍历的分片集合:userID>0 只取该分片;=0 取全部分片(调用方须持锁)。
func (a *Aggregator) poolsFor(userID int64) [][]*subscription.Node {
	if userID > 0 {
		if p, ok := a.pools[userID]; ok {
			return [][]*subscription.Node{p}
		}
		return nil
	}
	out := make([][]*subscription.Node, 0, len(a.pools))
	for _, p := range a.pools {
		out = append(out, p)
	}
	return out
}

// UpdateNodeIdentity 按 NodeKey 更新内存池中节点的身份字段(名称/地区),
// 使重命名与地区回写立即反映在 /nodes 列表,不必等下一轮聚合刷新。
// name/region 为空表示"本次不改该字段"(region 回写只带 region,rename 只带 name)。
// 不可变语义:命中后替换切片中的节点对象(浅拷贝再改),不原地改写旧指针,
// 与 Nodes() 返回引用的并发读者隔离。找到并更新返回 true;池中无此节点返回 false。
func (a *Aggregator) UpdateNodeIdentity(nodeKey, name, region string) bool {
	return a.UpdateNodeIdentityForUser(0, nodeKey, name, region)
}

// UpdateNodeIdentityForUser 更新指定用户分片的节点身份(ticket 07);
// userID=0 回退为跨分片查找。
func (a *Aggregator) UpdateNodeIdentityForUser(userID int64, nodeKey, name, region string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, pool := range a.poolsFor(userID) {
		for i, n := range pool {
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
			pool[i] = &updated
			return true
		}
	}
	return false
}

// Run 定时执行聚合流水线(经 jobs 运行时发起刷新任务)。
// 按用户分片(多租户):每个 tick 遍历全部启用用户,定时刷新开关(租户级设置,
// 回退全局默认)生效者各自发起一轮 StartRefreshJobForUser;开关每 tick 重读,
// 运行时切换下一个 tick 即生效。
func (a *Aggregator) Run(ctx context.Context) {
	// 启动时立即跑一轮（仅限“定时刷新”开启者;默认关,见 ADR 0004/0042）
	a.startScheduledRefreshes(store.RefreshTriggerStartup)

	ticker := time.NewTicker(a.cfg.HealthCheck.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.startScheduledRefreshes(store.RefreshTriggerScheduled)
		}
	}
}

// startScheduledRefreshes 遍历全部启用用户,为开关生效者发起一轮定时/启动刷新。
// 用户列表读取失败时回退为旧全局全量刷新(fail-open:不让刷新永久停摆)。
func (a *Aggregator) startScheduledRefreshes(trigger string) {
	users, err := a.st.ListUsers()
	if err != nil {
		a.logger.Warn("list users failed, falling back to global refresh", "error", err)
		if _, _, _, err := a.StartRefreshJob(trigger); err != nil {
			a.logger.Error("aggregation failed", "error", err)
		}
		return
	}
	for _, u := range users {
		if u.Disabled() {
			continue
		}
		if !a.autoRefreshEnabledForUser(u.ID) {
			continue
		}
		// 撞车跳过:该用户已有全量进行中(附加)或与单机场刷新冲突,本轮不再发起
		_, _, started, err := a.StartRefreshJobForUser(u.ID, trigger)
		switch {
		case errors.Is(err, ErrRefreshConflict):
			a.logger.Info("scheduled aggregation skipped: conflicting refresh in progress", "user_id", u.ID)
		case err != nil:
			a.logger.Error("aggregation failed", "user_id", u.ID, "error", err)
		case !started:
			a.logger.Info("scheduled aggregation skipped: refresh already running", "user_id", u.ID)
		}
	}
}

// autoRefreshEnabled 读取“定时刷新”全局开关（默认关,ADR 0042)。只有显式设为 "true" 才开启；
// 读取设置失败时按关处理（fail-closed:机场订阅被服务器出口封锁(403)是常态,
// 宁可不刷,也不默认定时外打机场 URL;节点更新由手动刷新/粘贴导入驱动）。
func (a *Aggregator) autoRefreshEnabled() bool {
	settings, err := a.st.GetSystemSettings()
	if err != nil {
		a.logger.Warn("get system settings failed, assuming scheduled refresh disabled", "error", err)
		return false
	}
	return settings["scheduled_refresh_enabled"] == "true"
}

// autoRefreshEnabledForUser 读取指定用户的定时刷新开关(租户级设置,回退全局默认):
// 用户未覆盖时跟随全局 autoRefreshEnabled;同样 fail-closed(ADR 0042)。
func (a *Aggregator) autoRefreshEnabledForUser(userID int64) bool {
	val, err := a.st.GetSettingForUser(userID, "scheduled_refresh_enabled")
	if err != nil {
		return false
	}
	return val == "true"
}

// RunOnce 同步执行一轮完整的聚合流水线，并写入刷新记录。
// 内部路径(测试/直接调用):自身无任何互斥,调用方自负并发安全
// (生产路径一律走 jobs 运行时的 kind+key 单实例,见 refresh_job.go)。
func (a *Aggregator) RunOnce(ctx context.Context, trigger string) error {
	rl, err := a.newRunLog(0, trigger, 0)
	if err != nil {
		// 刷新记录写不进去不阻断聚合，仅丢失本次日志
		a.logger.Warn("create refresh run failed, continuing without refresh log", "error", err)
	}
	a.execute(ctx, rl, nil)
	return nil
}

// runLog 一次刷新的结构化事件记录器；runID 为 0 时所有写入降级为 no-op
type runLog struct {
	st     *store.Store
	logger *slog.Logger
	runID  int64
}

// newRunLog 创建刷新记录；失败时返回可安全使用的 no-op 记录器和错误，由调用方决定是否降级。
// jobID 为关联的 jobs 任务 id(任务化刷新回填;直接 RunOnce 传 0)。
// userID 为属主(多租户):刷新历史按用户隔离;0 = 全局(定时/启动刷新)。
func (a *Aggregator) newRunLog(userID int64, trigger string, jobID int64) (*runLog, error) {
	rl := &runLog{st: a.st, logger: a.logger}
	run, err := a.st.CreateRefreshRunForUser(userID, trigger, jobID)
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

// fetchDiag 落一条机场拉取诊断(ticket 0018);失败不阻断,仅丢本条诊断。
// errMsg 为空表示拉取成功。
func (r *runLog) fetchDiag(airport *store.Airport, diag *subscription.FetchDiagnostics, errMsg string) {
	if r.runID == 0 || diag == nil {
		return
	}
	d := &store.RefreshFetchDiag{
		RunID:         r.runID,
		Airport:       airport.Name,
		AirportID:     airport.ID,
		HTTPStatus:    diag.HTTPStatus,
		DurationMs:    diag.DurationMs,
		NodeCount:     diag.NodeCount,
		ParseFailures: diag.ParseFailures,
		Error:         errMsg,
	}
	if err := r.st.InsertRefreshFetchDiag(d); err != nil {
		r.logger.Warn("insert refresh fetch diag failed", "error", err, "airport", airport.Name)
	}
}

// persistAirportUsage 拉取成功后落库用量信息(仅当响应头存在;
// 落库失败不阻断拉取主路径,只丢本次捕获)。全量与单机场刷新共用。
func (a *Aggregator) persistAirportUsage(airport *store.Airport, diag *subscription.FetchDiagnostics) {
	if diag == nil || diag.Usage == nil {
		return
	}
	if err := a.st.UpdateAirportUsage(airport.ID, diag.Usage); err != nil {
		a.logger.Warn("update airport usage failed", "airport", airport.Name, "error", err)
	}
}

// persistAirportHosts 拉取成功后覆盖落库上游 hosts 映射(issue #116;含空映射
// 清空:上游不再声明 hosts 时不残留旧映射)。落库失败不阻断拉取主路径。
// 全量与单机场刷新共用;拉取失败/取消的旧映射原样保留(与 per-source 合并语义一致)。
func (a *Aggregator) persistAirportHosts(airport *store.Airport, sub *subscription.Subscription) {
	if sub == nil {
		return
	}
	if err := a.st.UpdateAirportHosts(airport.ID, sub.Hosts); err != nil {
		a.logger.Warn("update airport hosts failed", "airport", airport.Name, "error", err)
	}
}

// fetchResult 拉取阶段产出
type fetchResult struct {
	airportNodes map[string][]*subscription.Node // 按机场分组（拉取失败的为 nil），供告警判断
	allNodes     []*subscription.Node
	enabled      int   // 启用的机场数
	failed       int   // 拉取失败的机场数
	owner        int64 // 本轮刷新的目标分片(ticket 07):拉取机场属主(未归属已归一到超管)
	// preserve 本轮未成功拉取、但仍现存且启用的机场名集合(手动跳过/拉取失败/
	// 取消未启动):per-source 合并时这些来源的旧节点原样保留而非标 stale。
	// 不在成功集也不在 preserve 的旧池来源(机场已删除/被禁用/已改名的旧名)
	// 属合法消失,照旧进 MergePool stale 扫描(见 ADR 0034 与 Check H1)。
	preserve map[string]bool
}

// execute 聚合流水线(旧入口,兼容既有调用):全表扫机场与自建节点,内部按分片归属
// 为节点打上 UserID 后合入对应分片池(ticket 07 Invariant B:单管理员一切归超管)。
// progress 为任务游标回调(已完成机场数;RunOnce 直接调用传 nil)。
func (a *Aggregator) execute(ctx context.Context, rl *runLog, progress func(string)) {
	a.executeForUser(ctx, rl, progress, 0)
}

// executeForUser 聚合流水线:拉取(airportOwnerID>0 限定该用户机场,=0 全量)
// → 地区识别 → 健康检查 → 按分片 MergePool 合并入池 → 持久化 → 告警。
// 刷新任务统一以机场属主为 owner 调用(手动/定时全量走超管分片)。
func (a *Aggregator) executeForUser(ctx context.Context, rl *runLog, progress func(string), airportOwnerID int64) {
	a.logger.Info("aggregation started", "airport_owner", airportOwnerID)

	fetched, err := a.fetchAirports(ctx, rl, progress, airportOwnerID)
	if err != nil {
		a.logger.Error("list airports failed", "error", err)
		rl.event(levelError, stageFetch, "读取机场列表失败："+err.Error(), nil)
		rl.finish(store.RefreshStatusFailed, 0, 0, 0, "读取机场列表失败")
		return
	}

	// 全量拉取失败 = 本轮没有任何数据,而非"节点都挂了"。此时保留现有节点池,
	// 避免一次网络抖动 / 机场临时不可达就把所有节点清空(见用户反馈:刷新失败清空节点)。
	if fetched.enabled > 0 && fetched.failed == fetched.enabled {
		owner := fetched.owner
		a.mu.RLock()
		pool := a.pools[owner]
		a.mu.RUnlock()

		errMsg := fmt.Sprintf("%d/%d 机场拉取失败", fetched.failed, fetched.enabled)
		// WARN 而非 ERROR:机场订阅封锁服务器出口(403)是常态拒绝,不是系统故障(ADR 0042 上下文)
		a.logger.Warn("all airports failed, retaining pool and refreshing health only",
			"failed", fetched.failed, "enabled", fetched.enabled, "retained", len(pool))

		// 健康检查续命(ADR 0042):订阅地址被封 ≠ 节点死了——403 封的是拉取通道,
		// 不是节点本身。对保留池跑健康检查,可用性/延迟/健康历史保持新鲜,
		// 定时拉取关闭(默认)后健康数据不至于永久冻结。
		// 克隆后检查再整体换分片:checkHealth 原地写字段,直接动在线池会与订阅读取竞态。
		// 已知窗口:健康检查期间同分片的 UI 写回(单节点检测/重命名)可能被换片覆盖——
		// 一次性丢失、下轮检测自愈,与成功路径的读-改-换同构(Check MEDIUM-1 记录在案)。
		available := 0
		if len(pool) > 0 {
			cloned := make([]*subscription.Node, len(pool))
			for i, n := range pool {
				cp := *n
				cloned[i] = &cp
			}
			available = len(a.checkHealth(ctx, rl, cloned))

			a.mu.Lock()
			a.pools[owner] = cloned
			a.lastUpdate = time.Now()
			a.mu.Unlock()

			// 持久化刷新后的健康快照(写库失败仅告警,内存池已更新)
			if err := a.st.SaveNodePoolForUser(owner, cloned); err != nil {
				a.logger.Warn("persist node pool failed", "error", err)
			}
		}

		rl.event(levelWarn, stageFetch,
			fmt.Sprintf("全部机场拉取失败，保留现有 %d 个节点，仅更新健康状态", len(pool)),
			map[string]any{"retained": len(pool), "available": available})
		rl.finish(store.RefreshStatusFailed, 0, available, len(pool), errMsg)
		a.checkAlerts(fetched.airportNodes, available)
		return
	}

	// 取消:只对成功拉取的机场做 MergePool 入池,未拉取的机场节点原样保留。
	// 直接走全量 MergePool 会把未拉取机场的节点全部标 stale(见 code-review 发现)。
	if ctx.Err() != nil {
		a.mergePartialOnCancel(ctx, rl, fetched)
		return
	}

	// 地区识别：从节点名提取地区代码，填充 Region 字段
	a.recognizeRegions(ctx, rl, fetched.allNodes)

	// 注入自建节点(常驻安全网)。放在健康检查之前，使其与机场节点一起被检测——
	// 自建节点的延迟/可用性才能反映真实状态，而非恒为「可用、延迟 0」。
	// 放在 recognizeRegions 之后，保留其 Region "SELF" 标记不被地区识别覆盖。
	allNodes := a.appendSelfHosted(rl, fetched.allNodes, airportOwnerID)

	// 健康检查：记录每个节点的可用性与延迟，但**不过滤**。
	// 节点池保留全量数据(包括不可用/慢/重复的)，过滤延迟到订阅生成时。
	available := a.checkHealth(ctx, rl, allNodes)

	// MergePool carry-forward：用旧池的检测状态（DetectionLastCheck/Available/Latency/带宽）
	// 覆盖新池（修复刷新抹掉真实检测结果的 bug），并标记消失节点为 stale。
	// per-source 合并(spec-manual-airport-import):stale 扫描集 = 成功拉取来源 ∪
	// 旧池中"不再现存启用"的来源(删除/禁用/旧名);手动机场(永不拉取)与拉取
	// 失败机场(本轮无数据)在 preserve 集,节点原样保留(MergePool stale 陷阱)。
	owner := fetched.owner
	a.mu.RLock()
	oldPool := a.pools[owner]
	a.mu.RUnlock()
	mergedPool := mergePerSource(oldPool, allNodes, fetched, true)

	// 应用覆盖层（机场节点的 region 编辑;display_name 自 ADR 0047 起走名称槽位,不在此）
	a.applyOverrides(owner, mergedPool)
	// 刷新完成后自动重算名称(issue #51):按属主生效设置,开启时重算 DisplayName。
	// 槽位节点在 standardizePoolNames 内部跳过(ADR 0047),用户接管的名字不被抹掉。
	mergedPool = a.standardizePoolNames(owner, mergedPool)
	// 属主回填:本轮节点全部归属刷新 owner 分片(Invariant B)
	for _, n := range mergedPool {
		if n.UserID == 0 {
			n.UserID = owner
		}
	}

	a.mu.Lock()
	a.pools[owner] = mergedPool
	a.lastUpdate = time.Now()
	a.mu.Unlock()

	// 持久化节点池快照，供进程重启后回填内存池（见 ADR 0008）。
	// 写库失败只记日志、不阻断本轮聚合——内存池已更新，订阅照常可用。
	if err := a.st.SaveNodePoolForUser(owner, mergedPool); err != nil {
		a.logger.Warn("persist node pool failed", "error", err)
	}

	// 空槽告警(issue #100):槽位节点消失/stale → 告警一次;回归自动恢复
	a.alertEmptySlots(owner, mergedPool)

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
func (a *Aggregator) fetchAirports(ctx context.Context, rl *runLog, progress func(string), airportOwnerID int64) (*fetchResult, error) {
	var airports []*store.Airport
	var err error
	if airportOwnerID > 0 {
		airports, err = a.st.ListAirportsByUser(airportOwnerID)
	} else {
		airports, err = a.st.ListAirports()
	}
	if err != nil {
		return nil, err
	}

	var enabled []*store.Airport
	manualSkipped := 0
	preserve := make(map[string]bool)
	for _, airport := range airports {
		if !airport.Enabled {
			continue
		}
		// 手动机场无订阅 URL 可拉,定时/全量刷新跳过(CONTEXT.md「手动机场」);
		// 其节点进 preserve 集,per-source 合并原样保留而不标 stale。
		if airport.SourceType == store.AirportSourceManual {
			manualSkipped++
			preserve[airport.Name] = true
			continue
		}
		enabled = append(enabled, airport)
	}

	// 本轮分片属主:单用户刷新直接用请求属主;全量刷新归一到首个超管(Invariant B)。
	owner := airportOwnerID
	if owner <= 0 {
		owner = a.ownerUserID(0)
	}

	if manualSkipped > 0 {
		rl.event(levelInfo, stageFetch, fmt.Sprintf("跳过 %d 个手动机场(无订阅 URL,不参与拉取)", manualSkipped), nil)
	}
	if len(enabled) == 0 {
		rl.event(levelWarn, stageFetch, "没有启用的机场，跳过拉取", nil)
	} else {
		rl.event(levelInfo, stageFetch, fmt.Sprintf("开始拉取 %d 个机场", len(enabled)), nil)
	}

	concurrency := a.fetchConcurrency()
	type outcome struct {
		nodes   []*subscription.Node
		err     error
		skipped bool // 取消导致未启动(不是机场故障,不计入失败)
	}
	outcomes := make([]outcome, len(enabled))
	sem := make(chan struct{}, concurrency)
	var completed atomic.Int64
	var wg sync.WaitGroup
	reportProgress := func() {
		if progress != nil {
			progress(strconv.FormatInt(completed.Add(1), 10))
		}
	}
	for i, airport := range enabled {
		wg.Add(1)
		go func(i int, airport *store.Airport) {
			defer wg.Done()
			defer reportProgress()
			// 取消=中断当前拉取:不再启动新拉取(进行中的由 fetcher 超时兜底跑完)
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				outcomes[i] = outcome{skipped: true}
				return
			}

			rl.event(levelInfo, stageFetch, fmt.Sprintf("拉取「%s」…", airport.Name), nil)
			sub, diag, err := a.fetcher.FetchWithDiagnostics(airport.Name, airport.URL)
			if err != nil {
				a.logger.Warn("fetch airport failed", "airport", airport.Name, "error", err)
				rl.fetchDiag(airport, diag, err.Error())
				rl.event(levelWarn, stageFetch, fmt.Sprintf("「%s」拉取失败：%s", airport.Name, err.Error()),
					map[string]any{"airport": airport.Name, "http_status": diag.HTTPStatus, "duration_ms": diag.DurationMs})
				outcomes[i] = outcome{err: err}
				return
			}
			a.logger.Info("fetched airport", "airport", airport.Name, "nodes", len(sub.Nodes))
			// 属主打标(ticket 07):机场节点归属机场属主(未归属行归一到超管分片)。
			nodeOwner := a.ownerUserID(airport.UserID)
			for _, n := range sub.Nodes {
				n.UserID = nodeOwner
			}
			rl.fetchDiag(airport, diag, "")
			a.persistAirportUsage(airport, diag)
			a.persistAirportHosts(airport, sub)
			rl.event(levelInfo, stageFetch, fmt.Sprintf("「%s」拉取成功，%d 个节点", airport.Name, len(sub.Nodes)),
				map[string]any{
					"airport": airport.Name, "nodes": len(sub.Nodes),
					"http_status": diag.HTTPStatus, "duration_ms": diag.DurationMs,
					"parse_failures": diag.ParseFailures,
				})
			outcomes[i] = outcome{nodes: sub.Nodes}
		}(i, airport)
	}
	wg.Wait()

	result := &fetchResult{
		airportNodes: make(map[string][]*subscription.Node),
		enabled:      len(enabled),
		owner:        owner,
		preserve:     preserve,
	}
	for i, airport := range enabled {
		o := outcomes[i]
		if o.skipped {
			result.airportNodes[airport.Name] = nil
			preserve[airport.Name] = true // 取消未启动:现存启用机场,节点保留
			continue
		}
		if o.err != nil {
			result.airportNodes[airport.Name] = nil
			result.failed++
			preserve[airport.Name] = true // 拉取失败:本轮无数据 ≠ 节点消失
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
			// 来源标记:本次 Available 由 TCP 快检写下(幂等;真实检测节点进不了此分支,不会被降级)
			r.Node.DetectionKind = subscription.DetectionKindHealth
			// 失败原因:TCP 不通时分类记录,连通时清空(ticket 0017)
			if r.Available {
				r.Node.DetectionFailReason = ""
				r.Node.DetectionFailDetail = ""
			} else if r.Error != nil {
				r.Node.DetectionFailReason = detection.ClassifyFailure(r.Error)
				r.Node.DetectionFailDetail = detection.TruncateFailDetail(r.Error.Error())
			}
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
func (a *Aggregator) appendSelfHosted(rl *runLog, nodes []*subscription.Node, airportOwnerID int64) []*subscription.Node {
	var selfHostedNodes []*store.SelfHostedNode
	var err error
	if airportOwnerID > 0 {
		selfHostedNodes, err = a.st.ListSelfHostedNodesByUser(airportOwnerID)
	} else {
		selfHostedNodes, err = a.st.ListSelfHostedNodes()
	}
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
		node := shn.ToNode()
		// 属主打标(ticket 07):自建节点归属其属主(未归属行归一到超管分片)。
		node.UserID = a.ownerUserID(shn.UserID)
		result = append(result, node)
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

// alertEmptySlots 空槽告警(ADR 0047 / issue #100):刷新后槽位指向的节点
// 从池中消失或被标 stale → 飞书告警一次(冷却:同槽位恢复前不重复);
// 节点以同 node_key 回归 → 自动恢复通知。预建空槽(node_key 空)不告警——
// 那是用户故意的"先起名后挑节点"。绝不自动改绑,补位是用户的显式动作。
func (a *Aggregator) alertEmptySlots(userID int64, pool []*subscription.Node) {
	slots, err := a.st.ListNameSlotsForUser(userID)
	if err != nil {
		a.logger.Warn("list name slots for empty-slot alert failed", "error", err)
		return
	}
	staleByKey := make(map[string]bool, len(pool))
	for _, n := range pool {
		staleByKey[n.NodeKey()] = n.Stale
	}
	prefix := fmt.Sprintf("slot_empty:%d:", userID)
	seen := make(map[string]bool, len(slots))
	for _, sl := range slots {
		if sl.NodeKey == "" {
			continue
		}
		stale, exists := staleByKey[sl.NodeKey]
		gone := !exists || stale
		key := prefix + sl.Name
		seen[key] = true
		if gone {
			if !a.alerted[key] {
				if a.alerter != nil {
					if err := a.alerter.Alert("名称槽位待指派",
						fmt.Sprintf("名称「%s」挂载的节点已从机场消失或下架，订阅已停发该名称。\n请把名称指派给新节点（节点管理 → 名称槽位）。\nnode_key: %s",
							sl.Name, sl.NodeKey)); err != nil {
						a.logger.Warn("send empty-slot alert failed", "error", err)
					} else {
						a.alerted[key] = true
					}
				}
			}
		} else {
			if a.alerted[key] && a.alerter != nil {
				if err := a.alerter.Alert("名称槽位已恢复",
					fmt.Sprintf("名称「%s」挂载的节点已回归，订阅自动恢复下发。", sl.Name)); err != nil {
					a.logger.Warn("send slot recovery alert failed", "error", err)
				}
			}
			delete(a.alerted, key)
		}
	}
	// 清理已删除槽位的冷却残留(alerted 只增不减会泄漏;改名等同删旧建新)
	for k := range a.alerted {
		if strings.HasPrefix(k, prefix) && !seen[k] {
			delete(a.alerted, k)
		}
	}
}

// recognizeRegions 识别全部节点的地区代码，填充 Region 字段。
// 统一三层识别(issue #37):名称规则 -> 国旗 emoji 反解 -> GeoIP 兜底,
// 与 poolops 单机场 upsert 同一识别器同一口径;ctx 从刷新任务透传,
// L3 内部有界并发 + 短超时 + 持久缓存,失败静默降级 Unknown。
func (a *Aggregator) recognizeRegions(ctx context.Context, rl *runLog, nodes []*subscription.Node) {
	if a.regionRec == nil {
		return // 识别器缺失(不应发生),跳过
	}

	reqs := make([]region.Request, len(nodes))
	for i, node := range nodes {
		reqs[i] = region.Request{Name: node.Name, Server: node.Server}
	}
	codes := a.regionRec.RecognizeBatch(ctx, reqs)

	regionStats := make(map[string]int)
	for i, node := range nodes {
		node.Region = codes[i]
		regionStats[node.Region]++
	}

	// 记录识别统计
	rl.event(levelInfo, stageFetch, fmt.Sprintf("地区识别完成：%d 个节点分布于 %d 个地区",
		len(nodes), len(regionStats)), map[string]any{"regions": regionStats})
	a.logger.Info("region recognition completed", "nodes", len(nodes), "regions", len(regionStats), "stats", regionStats)
}

// applyOverrides 应用机场节点覆盖层(region 编辑);自建节点豁免。
// 按池属主读覆盖层(多租户):同一节点可被不同用户独立覆盖。
// display_name 自 ADR 0047(issue #96)起不再由此应用:命名统一走名称槽位
// (订阅生成时实时叠加,立即生效),覆盖层只保留 region 维度。
func (a *Aggregator) applyOverrides(userID int64, pool []*subscription.Node) {
	overrides, err := a.st.ListNodeOverridesForUser(userID)
	if err != nil {
		a.logger.Warn("load node overrides failed", "error", err)
		return
	}
	for _, n := range pool {
		if n.Source == subscription.SourceSelfHosted {
			continue // 自建节点不走覆盖层
		}
		if override, exists := overrides[n.NodeKey()]; exists {
			if override.Region != "" {
				n.Region = override.Region
			}
		}
	}
}

// fetchedSources 返回本轮成功拉取到节点的来源集合(失败/跳过/手动机场不在内)。
func fetchedSources(fetched *fetchResult) map[string]bool {
	sources := make(map[string]bool, len(fetched.airportNodes))
	for name, nodes := range fetched.airportNodes {
		if nodes != nil {
			sources[name] = true
		}
	}
	return sources
}

// mergePerSource 按来源合并节点池(成功路径与取消路径共用的 per-source 手法):
//
//	stale 扫描集 = 成功拉取来源 ∪ 旧池中"不再现存启用"的来源(机场已删除/被禁用/
//	已改名的旧名——属合法消失,照旧标 stale,否则会永久 active 持续下发订阅);
//	保留集 = fetched.preserve(手动跳过/拉取失败/取消未启动的现存启用机场)——
//	本轮无数据 ≠ 节点消失,旧节点原样保留。
//
// includeSelfHosted 区分两条路径:成功路径自建节点随 allNodes 重新注入,
// 旧自建必须进 MergePool(carry-forward 检测状态,未被再注入的旧自建由
// MergePool 的自建豁免丢弃);取消路径无注入,自建走保留侧原样留下。
//
// 尾部按 NodeKey 去重:rest 侧与新拉取集同 key 的旧节点丢弃(新拉取集优先),
// 防跨来源同 server:port 双份。
func mergePerSource(oldPool, newNodes []*subscription.Node, fetched *fetchResult, includeSelfHosted bool) []*subscription.Node {
	fetchedSources := fetchedSources(fetched)
	var oldOfFetched, rest []*subscription.Node
	for _, n := range oldPool {
		// 自建节点路由只看路径,不经 preserve 判定(H1R):成功路径进 MergePool
		// (随注入 carry-forward,未再注入的旧自建由其自建豁免丢弃);取消路径
		// 无注入,必须走保留侧——进 MergePool 会被其豁免分支直接丢弃,
		// 自建 FailBack 节点将整批从池与 DB 快照消失。
		if n.Source == subscription.SourceSelfHosted {
			if includeSelfHosted {
				oldOfFetched = append(oldOfFetched, n)
			} else {
				rest = append(rest, n)
			}
			continue
		}
		inFetchSet := fetchedSources[n.Source]
		// 保留侧仅限"明确保留"的来源;既未成功拉取又不保留的来源(删除/禁用/旧名)
		// 并入 stale 扫描集——合法消失必须下架。
		if inFetchSet || !fetched.preserve[n.Source] {
			oldOfFetched = append(oldOfFetched, n)
		} else {
			rest = append(rest, n)
		}
	}
	merged := subscription.MergePool(oldOfFetched, newNodes)
	seen := make(map[string]bool, len(merged)+len(rest))
	for _, n := range merged {
		seen[n.NodeKey()] = true
	}
	for _, n := range rest {
		if seen[n.NodeKey()] {
			continue // 新拉取集优先:rest 同 key 旧节点丢弃
		}
		merged = append(merged, n)
	}
	return merged
}

// mergePartialOnCancel 取消时的部分入池:只对成功拉取的机场做 MergePool
// (carry-forward + stale 标记只作用于这些机场),未拉取的机场节点原样保留——
// 取消不等于"那些机场消失了",标 stale 会让整个池在订阅里消失。
func (a *Aggregator) mergePartialOnCancel(ctx context.Context, rl *runLog, fetched *fetchResult) {
	a.recognizeRegions(ctx, rl, fetched.allNodes)

	owner := fetched.owner
	a.mu.RLock()
	oldPool := a.pools[owner]
	a.mu.RUnlock()
	newPool := mergePerSource(oldPool, fetched.allNodes, fetched, false)
	a.applyOverrides(owner, newPool)
	for _, n := range newPool {
		if n.UserID == 0 {
			n.UserID = owner
		}
	}

	a.mu.Lock()
	a.pools[owner] = newPool
	a.lastUpdate = time.Now()
	a.mu.Unlock()

	if err := a.st.SaveNodePoolForUser(owner, newPool); err != nil {
		a.logger.Warn("persist node pool failed", "error", err)
	}
	a.alertEmptySlots(owner, newPool)

	rl.event(levelWarn, stageDone,
		fmt.Sprintf("刷新已取消：%d 个机场已拉取部分照常入池，未拉取机场保持原状", len(fetchedSources(fetched))),
		map[string]any{"fetched_airports": len(fetchedSources(fetched))})
	rl.finish(store.RefreshStatusCancelled, len(fetched.allNodes), 0, len(newPool), "cancelled")
	a.checkAlerts(fetched.airportNodes, 0)
}
