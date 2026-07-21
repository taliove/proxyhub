package aggregator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/taliove/proxyhub/internal/jobs"
	"github.com/taliove/proxyhub/internal/poolops"
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
var ErrRefreshConflict = errors.New("refresh conflicts with a running refresh job")

// RefreshJobParams 刷新任务启动参数(params_json)。
type RefreshJobParams struct {
	Trigger string `json:"trigger"` // manual / scheduled / startup
	// AirportID 单机场刷新的机场 id;0 = 全量。
	AirportID int64 `json:"airport_id,omitempty"`
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

func (k *refreshKind) Run(ctx context.Context, params json.RawMessage, _ string, _ func(json.RawMessage), _ func(string)) error {
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
	return k.runFull(ctx, &p)
}

// runFull 全量刷新:完整聚合流水线(拉取→地区识别→健康检查→合并入池)。
// 取消时 execute 内部中断于当前阶段,已拉取部分照常入池,refresh_runs 记 cancelled。
func (k *refreshKind) runFull(ctx context.Context, p *RefreshJobParams) error {
	rl, err := k.agg.newRunLog(p.Trigger, k.agg.findRunningJobID(refreshKeyAll))
	if err != nil {
		// 刷新记录写不进去不阻断聚合,仅丢失本次日志
		k.agg.logger.Warn("create refresh run failed, continuing without refresh log", "error", err)
	}
	k.agg.execute(ctx, rl)
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

	sub, err := k.agg.fetcher.Fetch(airport.Name, airport.URL)
	if err != nil {
		rl.event(levelError, stageFetch, fmt.Sprintf("「%s」拉取失败:%s", airport.Name, err.Error()),
			map[string]any{"airport": airport.Name})
		rl.finish(store.RefreshStatusFailed, 0, 0, 0, err.Error())
		return fmt.Errorf("fetch airport %s: %w", airport.Name, err)
	}
	rl.event(levelInfo, stageFetch, fmt.Sprintf("「%s」拉取成功,%d 个节点", airport.Name, len(sub.Nodes)),
		map[string]any{"airport": airport.Name, "nodes": len(sub.Nodes)})

	// 取消检查:拉取完成到入池之间被取消,不入池、状态记 cancelled,
	// 与 jobs 行的 cancelled 终态保持口径一致
	if err := ctx.Err(); err != nil {
		rl.finish(store.RefreshStatusCancelled, len(sub.Nodes), 0, 0, "cancelled")
		return err
	}

	// 池写串行化:不同机场的单机场刷新允许并行拉取,但 UpsertAirportNodes
	// 是"读全池-改本机场-写全池",并行写会丢更新(lost update);串行代价低(纯 DB 操作)
	k.agg.singleUpsertMu.Lock()
	upsertErr := k.agg.poolOps.UpsertAirportNodes(airport.Name, sub.Nodes)
	if upsertErr == nil {
		// 内存池回填(DB 已是新状态;读失败不阻断,下轮全量刷新自愈)
		k.agg.restoreNodePool()
	}
	k.agg.singleUpsertMu.Unlock()
	if upsertErr != nil {
		rl.finish(store.RefreshStatusFailed, len(sub.Nodes), 0, 0, upsertErr.Error())
		return fmt.Errorf("upsert airport nodes: %w", upsertErr)
	}

	k.agg.mu.RLock()
	poolSize := len(k.agg.nodes)
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
func (a *Aggregator) refreshConflict(key string) (string, bool) {
	for _, running := range a.refreshJobs.RunningKeys(refreshJobKindName) {
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
	return a.startRefresh(trigger, 0)
}

// startRefresh 发起刷新任务(全量 airportID=0 / 单机场)。
// refreshStartMu 把冲突检查与 OpenIDForce 包成临界区,消除 TOCTOU:
// 否则两个并发触发(全量 + 单机场)可同时通过检查,破坏机场级互斥。
func (a *Aggregator) startRefresh(trigger string, airportID int64) (int64, string, bool, error) {
	key := refreshJobKey(airportID)
	a.refreshStartMu.Lock()
	defer a.refreshStartMu.Unlock()
	if conflictKey, ok := a.refreshConflict(key); ok {
		return 0, key, false, fmt.Errorf("%w: running key %s", ErrRefreshConflict, conflictKey)
	}
	params, err := json.Marshal(RefreshJobParams{Trigger: trigger, AirportID: airportID})
	if err != nil {
		return 0, key, false, fmt.Errorf("marshal refresh params: %w", err)
	}
	// OpenIDForce:进行中附加(单实例),已收口则重开新一轮(再点一次就再跑一轮)
	rowID, started, err := a.refreshJobs.OpenIDForce(refreshJobKindName, key, params)
	if err != nil {
		return 0, key, false, err
	}
	return rowID, key, started, nil
}

// CancelRefresh 取消指定 key 的刷新任务;无进行中任务返回 false。
func (a *Aggregator) CancelRefresh(key string) bool {
	return a.refreshJobs.Cancel(refreshJobKindName, key)
}

// RefreshJobs 暴露刷新任务管理器(供 server 取消分发等)。
func (a *Aggregator) RefreshJobs() *jobs.Manager {
	return a.refreshJobs
}

// PoolOps 暴露单机场池操作(供 airporttest 等复用同一口径)。
func (a *Aggregator) PoolOps() poolops.Operations {
	return a.poolOps
}
