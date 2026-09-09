// Package poolops 提供节点池的单机场维度操作:按来源加载、单机场 upsert 合并。
//
// 从 airporttest 上移(原 PoolOperations/StorePoolAdapter,ADR 0025),成为聚合层
// 共用能力:机场测试的池空补救与单机场刷新(ticket 04)复用同一口径——
// 解析→地区识别→MergePool carry-forward→分片局部 upsert(issue #143)。
package poolops

import (
	"context"
	"fmt"
	"sync"

	"github.com/taliove/proxyhub/internal/region"
	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

// upsertMu 串行化全部 StoreAdapter 实例的池写。UpsertAirportNodes 是
// "读全池-改本机场-写本机场分片":读-改-写跨两条 DB 语句,并行写同机场会
// lost update(后写覆盖先写)。包级而非实例级:aggregator(单机场刷新)与
// server(机场测试池空补救)各自 new 适配器,而节点池同一进程只有一份,
// 写必须全局串行。
var upsertMu sync.Mutex

// Operations 抽象节点池的按源加载与单机场 upsert。
type Operations interface {
	// LoadPoolBySource 返回池中指定来源(机场名)的在架节点(不含 stale)。
	LoadPoolBySource(source string) ([]*subscription.Node, error)
	// UpsertAirportNodes 把新拉取的节点合并进池(单机场范围):
	// 该机场旧节点走 MergePool carry-forward,其他机场节点不动。
	// userID 为机场属主(issue #143 跨用户隔离):机场名是用户自选字符串,
	// 两用户可有同名机场,分片读写都按 (airportName, userID) 双重限定。
	// ctx 用于地区识别 L3 的 DNS(取消即中断,识别 best-effort 不阻断入池)。
	UpsertAirportNodes(ctx context.Context, airportName string, userID int64, fetchedNodes []*subscription.Node) error
}

// StoreAdapter 以 store.Store 为底的 Operations 实现。
type StoreAdapter struct {
	store *store.Store
	rec   *region.Recognizer // 统一三层地区识别器(issue #37);nil = 跳过识别
}

// NewStoreAdapter 创建基于 store 的池操作适配器。
// rec 为统一地区识别器,与 aggregator 全量刷新共用同一实例同一口径;
// 传 nil 跳过地区识别(仅测试)。
func NewStoreAdapter(st *store.Store, rec *region.Recognizer) *StoreAdapter {
	return &StoreAdapter{store: st, rec: rec}
}

// LoadPoolBySource 返回池中匹配来源(机场名)的节点,排除 stale。
func (a *StoreAdapter) LoadPoolBySource(source string) ([]*subscription.Node, error) {
	allNodes, err := a.store.LoadNodePool()
	if err != nil {
		return nil, fmt.Errorf("load node pool: %w", err)
	}

	var filtered []*subscription.Node
	for _, n := range allNodes {
		if n.Source == source && !n.Stale {
			filtered = append(filtered, n)
		}
	}
	return filtered, nil
}

// UpsertAirportNodes 单机场 upsert:复用全局刷新口径(地区识别 + MergePool),
// 但只重写本机场本属主的分片(issue #143 Bug 3 + 跨用户隔离):其他机场、
// 其他用户同名机场的节点全程不动,任一行写失败只回滚本分片,不再连坐全池。
// 池的读-改-写段由包级 upsertMu 串行,调用方无需自带锁。
func (a *StoreAdapter) UpsertAirportNodes(ctx context.Context, airportName string, userID int64, fetchedNodes []*subscription.Node) error {
	// 第一步:统一三层地区识别(issue #37,与全量刷新同一识别器同一口径:
	// 名称规则 -> 国旗 emoji 反解 -> GeoIP 兜底,best-effort 失败降级 Unknown)。
	// 只改调用方自己的 fetchedNodes,不触碰共享池,留在临界区外缩短持锁时间。
	if a.rec != nil {
		reqs := make([]region.Request, len(fetchedNodes))
		for i, node := range fetchedNodes {
			reqs[i] = region.Request{Name: node.Name, Server: node.Server}
		}
		codes := a.rec.RecognizeBatch(ctx, reqs)
		for i, node := range fetchedNodes {
			node.Region = codes[i]
		}
	}

	upsertMu.Lock()
	defer upsertMu.Unlock()

	// 第二步:加载当前池,取本机场本属主的旧节点(含 stale:MergePool 靠它做
	// carry-forward 与消失标记)。属主谓词(issue #143):两用户可有同名机场,
	// 只按 source 过滤会把对方用户节点并入 MergePool——消失被误判 stale、
	// position 被本轮重写。其他机场/其他用户的节点不参与本次读写。
	oldPool, err := a.store.LoadNodePool()
	if err != nil {
		return fmt.Errorf("load old pool: %w", err)
	}

	var airportOldPool []*subscription.Node
	for _, n := range oldPool {
		if n.Source == airportName && n.UserID == userID {
			airportOldPool = append(airportOldPool, n)
		}
	}

	// 第三步:只对该机场的旧节点做 MergePool(carry-forward 检测状态),
	// 产出本机场分片的完整新状态(在架 + 消失标 stale)
	mergedAirport := subscription.MergePool(airportOldPool, fetchedNodes)

	// 属主打标:分片内未归属行(user_id=0,含新拉取节点)归到本次属主,
	// 与聚合器 ownerUserID 归一规则(Invariant B)一致;upsert 落库的 user_id
	// 与分片谓词同源,不会出现"写进 A 分片却标着 0"的自相矛盾。
	for _, n := range mergedAirport {
		if n.UserID == 0 {
			n.UserID = userID
		}
	}

	// 第四步:分片局部 upsert,只重写本机场本属主
	if err := a.store.UpsertNodePoolShard(userID, airportName, mergedAirport); err != nil {
		return fmt.Errorf("upsert airport shard: %w", err)
	}

	return nil
}
