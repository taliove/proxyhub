package aggregator

import (
	"context"
	"fmt"

	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

// ImportManualAirportNodes 手动机场粘贴导入的入池入口(同步 HTTP,非任务)。
//
// 凭证红线:粘贴内容含节点凭证,做成 job kind 会把内容落进 params_json
// (任务中心 API 原样回显,等于凭证持久化);故走同步调用,内容不落库/日志。
//
// 互斥语义与单机场刷新一致:refreshStartMu 临界区内做冲突检查 + upsert,
// 全量刷新、同机场单机场刷新、同机场机场测试任一在跑即 ErrRefreshConflict(409)。
// upsert 语义同单机场刷新:该机场旧节点 carry-forward,其他机场不动,不跑健康检查。
// ctx 透传给地区识别 L3 的 DNS(请求取消即中断,识别 best-effort 不阻断导入)。
func (a *Aggregator) ImportManualAirportNodes(ctx context.Context, airport *store.Airport, nodes []*subscription.Node) error {
	a.refreshStartMu.Lock()
	defer a.refreshStartMu.Unlock()

	key := refreshJobKey(airport.ID)
	for _, running := range a.refreshJobs.RunningKeys(refreshJobKindName) {
		if running == refreshKeyAll || running == key {
			return fmt.Errorf("%w: running refresh %s", ErrRefreshConflict, running)
		}
	}
	if a.airportTestConflict != nil {
		if testKey, ok := a.airportTestConflict(airport.ID); ok {
			return fmt.Errorf("%w: airport test %s running", ErrRefreshConflict, testKey)
		}
	}

	// 属主打标:导入节点归属机场属主分片(未归属行归一到超管,同拉取路径)。
	owner := a.ownerUserID(airport.UserID)
	for _, n := range nodes {
		if n.UserID == 0 {
			n.UserID = owner
		}
	}

	if err := a.poolOps.UpsertAirportNodes(ctx, airport.Name, nodes); err != nil {
		return fmt.Errorf("upsert airport nodes: %w", err)
	}
	// 内存池回填(DB 已是新状态;读失败不阻断,下轮全量刷新自愈)
	a.restoreNodePool()
	return nil
}
