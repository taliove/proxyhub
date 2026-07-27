package aggregator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/taliove/proxyhub/internal/store"
)

// 刷新任务的 kind 名与 key 编码(ADR 0019 收口:刷新迁入 jobs 运行时)。
const (
	refreshJobKindName = "refresh"
	// refreshKeyAll 全量刷新的任务 key;单机场为 "airport-<id>"(ticket 04)。
	refreshKeyAll = "all"
)

// ErrRefreshConflict 发起的刷新与进行中的刷新在机场级互斥上冲突
// (全量 vs 任何单机场互斥;同 key 不冲突,按 kind+key 单实例附加)。
// 与进行中机场测试(kind=airport_test)的同机场冲突也归并到这个哨兵下(409 语义一致)。
var ErrRefreshConflict = errors.New("refresh conflicts with a running refresh job")

// ErrAirportTestConflict 发起的机场测试与进行中的刷新在机场级互斥上冲突
// (同机场单机场刷新或全量刷新在跑;不同机场不互斥)。
var ErrAirportTestConflict = errors.New("airport test conflicts with a running refresh job")

// RefreshJobParams 刷新任务启动参数(params_json)。
type RefreshJobParams struct {
	Trigger string `json:"trigger"` // manual / scheduled / startup
	// AirportID 单机场刷新的机场 id;0 = 全量。
	AirportID int64 `json:"airport_id,omitempty"`
	// AirportName 单机场刷新的机场名(展示用,发起时尽力填充;空不影响执行)
	AirportName string `json:"airport_name,omitempty"`
	// UserID 发起者属主(ticket 07):任务按用户分片,刷新只聚合该用户名下机场;
	// 0 = 全局全量(定时/启动刷新,聚合全部机场,结果按各机场属主自动分片)。
	UserID int64 `json:"user_id,omitempty"`
}

// refreshJobKey 任务 key:全量 "all",单机场 "airport-<id>"。
func refreshJobKey(airportID int64) string {
	if airportID <= 0 {
		return refreshKeyAll
	}
	return fmt.Sprintf("airport-%d", airportID)
}

// refreshKind 聚合刷新作为 jobs 运行时的 kind。不可续跑(刷新无游标,
// 重启即 interrupted;定时/手动自会再触发)。
type refreshKind struct {
	agg *Aggregator
}

func (k *refreshKind) Name() string    { return refreshJobKindName }
func (k *refreshKind) Resumable() bool { return false }

func (k *refreshKind) Run(ctx context.Context, params json.RawMessage, _ string, _ func(json.RawMessage), progress func(string)) error {
	var p RefreshJobParams
	if err := json.Unmarshal(params, &p); err != nil {
		return fmt.Errorf("parse refresh params: %w", err)
	}
	if p.Trigger == "" {
		p.Trigger = store.RefreshTriggerManual
	}
	if p.AirportID > 0 {
		return k.runSingle(ctx, &p)
	}
	return k.runFull(ctx, &p, progress)
}

// runFull 全量刷新:完整聚合流水线(拉取→地区识别→健康检查→合并入池)。
// 取消时 execute 内部中断于当前阶段,已拉取部分照常入池,refresh_runs 记 cancelled。
func (k *refreshKind) runFull(ctx context.Context, p *RefreshJobParams, progress func(string)) error {
	rl, err := k.agg.newRunLog(p.Trigger, k.agg.findRunningJobID(refreshKeyAll))
	if err != nil {
		// 刷新记录写不进去不阻断聚合,仅丢失本次日志
		k.agg.logger.Warn("create refresh run failed, continuing without refresh log", "error", err)
	}
	k.agg.executeForUser(ctx, rl, progress, p.UserID)
	return ctx.Err()
}

// runSingle 单机场刷新:只拉取→解析→地区识别→MergePool upsert 入池,不跑健康检查。
// 用于刚加机场/换订阅 token 后快速可见;可用性交给后续检测或全量刷新。
func (k *refreshKind) runSingle(ctx context.Context, p *RefreshJobParams) error {
	airport, err := k.agg.st.GetAirportByID(p.AirportID)
	if err != nil {
		return fmt.Errorf("get airport %d: %w", p.AirportID, err)
	}

	rl, err := k.agg.newRunLog(p.Trigger, k.agg.findRunningJobID(refreshJobKey(p.AirportID)))
	if err != nil {
		k.agg.logger.Warn("create refresh run failed, continuing without refresh log", "error", err)
	}
	rl.event(levelInfo, stageFetch,
		fmt.Sprintf("单机场刷新「%s」(仅拉取入池,不含健康检查)", airport.Name),
		map[string]any{"airport": airport.Name, "airport_id": airport.ID})

	sub, diag, err := k.agg.fetcher.FetchWithDiagnostics(airport.Name, airport.URL)
	if err != nil {
		rl.fetchDiag(airport, diag, err.Error())
		rl.event(levelError, stageFetch, fmt.Sprintf("「%s」拉取失败:%s", airport.Name, err.Error()),
			map[string]any{"airport": airport.Name, "http_status": diag.HTTPStatus, "duration_ms": diag.DurationMs})
		rl.finish(store.RefreshStatusFailed, 0, 0, 0, err.Error())
		return fmt.Errorf("fetch airport %s: %w", airport.Name, err)
	}
	rl.fetchDiag(airport, diag, "")
	rl.event(levelInfo, stageFetch, fmt.Sprintf("「%s」拉取成功,%d 个节点", airport.Name, len(sub.Nodes)),
		map[string]any{
			"airport": airport.Name, "nodes": len(sub.Nodes),
			"http_status": diag.HTTPStatus, "duration_ms": diag.DurationMs,
			"parse_failures": diag.ParseFailures,
		})

	// 取消检查:拉取完成到入池之间被取消,不入池、状态记 cancelled,
	// 与 jobs 行的 cancelled 终态保持口径一致
	if err := ctx.Err(); err != nil {
		rl.finish(store.RefreshStatusCancelled, len(sub.Nodes), 0, 0, "cancelled")
		return err
	}

	// 池写串行化已由 poolops 包内 upsertMu 保证(UpsertAirportNodes 是
	// "读全池-改本机场-写全池",串行代价低);不同机场的单机场刷新拉取仍并行。
	upsertErr := func() error {
		if err := k.agg.poolOps.UpsertAirportNodes(airport.Name, sub.Nodes); err != nil {
			return err
		}
		// 内存池回填(DB 已是新状态;读失败不阻断,下轮全量刷新自愈)
		k.agg.restoreNodePool()
		return nil
	}()
	if upsertErr != nil {
		rl.finish(store.RefreshStatusFailed, len(sub.Nodes), 0, 0, upsertErr.Error())
		return fmt.Errorf("upsert airport nodes: %w", upsertErr)
	}

	k.agg.mu.RLock()
	poolSize := len(k.agg.pools[k.agg.ownerUserID(airport.UserID)])
	k.agg.mu.RUnlock()
	rl.event(levelInfo, stageDone, fmt.Sprintf("单机场刷新完成:「%s」%d 个节点入池", airport.Name, len(sub.Nodes)),
		map[string]any{"airport": airport.Name, "nodes": len(sub.Nodes)})
	rl.finish(store.RefreshStatusSuccess, len(sub.Nodes), 0, poolSize, "")
	return ctx.Err()
}

// findRunningJobID 查本任务的 jobs 行 id(用于 refresh_runs.job_id 关联)。
// Insert 先于 Run(startOrAttach 顺序保证),同 key 单实例保证唯一;查不到退化 0。
func (a *Aggregator) findRunningJobID(key string) int64 {
	recs, err := a.st.Jobs().LoadRunning()
	if err != nil {
		a.logger.Warn("load running jobs failed, refresh run will not link job", "error", err)
		return 0
	}
	for _, r := range recs {
		if r.Kind == refreshJobKindName && r.Key == key {
			return r.ID
		}
	}
	return 0
}

// refreshConflict 机场级互斥检查:全量与任何进行中刷新互斥;单机场只与全量互斥。
// 同 key 不算冲突(由 kind+key 单实例附加语义处理)。
// 以 Manager 内存态(RunningKeys)为准:持久化失败退化的纯内存任务对 DB 查询是隐身的。
func (a *Aggregator) refreshConflict(userID int64, key string) (string, bool) {
	for _, running := range a.refreshJobs.RunningKeysFor(userID, refreshJobKindName) {
		if running == key {
			continue
		}
		if key == refreshKeyAll || running == refreshKeyAll {
			return running, true
		}
	}
	return "", false
}

// StartRefreshJob 经 jobs 运行时发起全量刷新(实现 server.NodeSource)。
// 返回 jobs 行 id、任务 key、是否本次新启动(同 key 重复触发附加到进行中任务)。
// 与进行中的单机场刷新冲突时返回 ErrRefreshConflict。
func (a *Aggregator) StartRefreshJob(trigger string) (int64, string, bool, error) {
	return a.startRefresh(0, trigger, 0)
}

// StartRefreshJobForUser 以指定属主发起全量刷新(ticket 07):
// 任务按 userID 分片,刷新只聚合该用户名下机场;userID=0 与 StartRefreshJob 等价。
func (a *Aggregator) StartRefreshJobForUser(userID int64, trigger string) (int64, string, bool, error) {
	return a.startRefresh(userID, trigger, 0)
}

// StartAirportRefreshJob 经 jobs 运行时发起单机场刷新(只拉取入池,不含健康检查)。
// 与进行中的全量刷新或同机场刷新冲突时返回 ErrRefreshConflict;
// 不同机场的单机场刷新可并行(拉取并行,池写串行)。
func (a *Aggregator) StartAirportRefreshJob(trigger string, airportID int64) (int64, string, bool, error) {
	return a.StartAirportRefreshJobForUser(0, trigger, airportID)
}

// StartAirportRefreshJobForUser 以指定属主发起单机场刷新(ticket 07)。
func (a *Aggregator) StartAirportRefreshJobForUser(userID int64, trigger string, airportID int64) (int64, string, bool, error) {
	if airportID <= 0 {
		return 0, "", false, fmt.Errorf("invalid airport id %d", airportID)
	}
	return a.startRefresh(userID, trigger, airportID)
}

// startRefresh 发起刷新任务(全量 airportID=0 / 单机场)。
// refreshStartMu 把冲突检查与 OpenIDForce 包成临界区,消除 TOCTOU:
// 否则两个并发触发(全量 + 单机场)可同时通过检查,破坏机场级互斥。
// 跨 kind 互斥(issue 0025):同机场机场测试在跑同样 409——与
// StartAirportTestExclusive 共用同一把 refreshStartMu,双向互斥无竞态窗口。
func (a *Aggregator) startRefresh(userID int64, trigger string, airportID int64) (int64, string, bool, error) {
	key := refreshJobKey(airportID)
	a.refreshStartMu.Lock()
	defer a.refreshStartMu.Unlock()
	if conflictKey, ok := a.refreshConflict(userID, key); ok {
		return 0, key, false, fmt.Errorf("%w: running key %s", ErrRefreshConflict, conflictKey)
	}
	if a.airportTestConflict != nil {
		if testKey, ok := a.airportTestConflict(airportID); ok {
			return 0, key, false, fmt.Errorf("%w: airport test %s running", ErrRefreshConflict, testKey)
		}
	}
	// 单机场任务带上机场名供任务中心展示(尽力而为,查不到不影响发起)
	var airportName string
	if airportID > 0 {
		if ap, err := a.st.GetAirportByID(airportID); err == nil {
			airportName = ap.Name
		}
	}
	params, err := json.Marshal(RefreshJobParams{Trigger: trigger, AirportID: airportID, AirportName: airportName, UserID: userID})
	if err != nil {
		return 0, key, false, fmt.Errorf("marshal refresh params: %w", err)
	}
	// OpenIDForceFor:按属主分片(同 key 不同用户互不冲突);进行中附加,已收口重开
	rowID, started, err := a.refreshJobs.OpenIDForceFor(userID, refreshJobKindName, key, params)
	if err != nil {
		return 0, key, false, err
	}
	return rowID, key, started, nil
}

// CancelRefresh 取消指定 key 的刷新任务;无进行中任务返回 false。
func (a *Aggregator) CancelRefresh(key string) bool {
	return a.refreshJobs.Cancel(refreshJobKindName, key)
}

// CancelRefreshForUser 取消指定用户进行中刷新的任务 key(ticket 07)。
func (a *Aggregator) CancelRefreshForUser(userID int64, key string) bool {
	return a.refreshJobs.CancelFor(userID, refreshJobKindName, key)
}

// SetAirportTestConflictChecker 注入跨 kind 互斥的测试侧查询回调(issue 0025)。
// 由机场测试任务运行时的持有方(server)在装配期、对外服务前调用一次;
// airportID=0 表示全量刷新视角(任何进行中的机场测试都算冲突),
// 否则只查同机场 key。nil(未注入)表示无测试运行时,冲突恒无。
func (a *Aggregator) SetAirportTestConflictChecker(fn func(airportID int64) (string, bool)) {
	a.refreshStartMu.Lock()
	defer a.refreshStartMu.Unlock()
	a.airportTestConflict = fn
}

// StartAirportTestExclusive 在 refreshStartMu 临界区内发起机场测试(issue 0025 跨 kind 互斥):
// 同机场单机场刷新或全量刷新在跑 → ErrAirportTestConflict(调用方映射 409);
// 否则在锁内回调 start 完成 kind+key 单实例发起。与 startRefresh 共用同一把锁,
// 两个方向的"检查+发起"互为临界区,无 TOCTOU 窗口;不同机场不互斥。
func (a *Aggregator) StartAirportTestExclusive(airportID int64, start func() (int64, string, bool, error)) (int64, string, bool, error) {
	key := refreshJobKey(airportID) // 与单机场刷新同编码("airport-<id>")
	a.refreshStartMu.Lock()
	defer a.refreshStartMu.Unlock()
	for _, running := range a.refreshJobs.RunningKeys(refreshJobKindName) {
		if running == refreshKeyAll || running == key {
			return 0, key, false, fmt.Errorf("%w: running refresh %s", ErrAirportTestConflict, running)
		}
	}
	return start()
}
