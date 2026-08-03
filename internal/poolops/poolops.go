// Package poolops 提供节点池的单机场维度操作:按来源加载、单机场 upsert 合并。
//
// 从 airporttest 上移(原 PoolOperations/StorePoolAdapter,ADR 0025),成为聚合层
// 共用能力:机场测试的池空补救与单机场刷新(ticket 04)复用同一口径——
// 解析→地区识别→MergePool carry-forward→SaveNodePool。
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
// "读全池-改本机场-写全池",并行写会 lost update(后写覆盖先写)。
// 包级而非实例级:aggregator(单机场刷新)与 server(机场测试池空补救)
// 各自 new 适配器,而节点池同一进程只有一份,写必须全局串行。
var upsertMu sync.Mutex

// Operations 抽象节点池的按源加载与单机场 upsert。
type Operations interface {
	// LoadPoolBySource 返回池中指定来源(机场名)的在架节点(不含 stale)。
	LoadPoolBySource(source string) ([]*subscription.Node, error)
	// UpsertAirportNodes 把新拉取的节点合并进池(单机场范围):
	// 该机场旧节点走 MergePool carry-forward,其他机场节点不动。
	// ctx 用于地区识别 L3 的 DNS(取消即中断,识别 best-effort 不阻断入池)。
	UpsertAirportNodes(ctx context.Context, airportName string, fetchedNodes []*subscription.Node) error
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

// UpsertAirportNodes 单机场 upsert:复用全局刷新口径(地区识别 + MergePool + SaveNodePool)。
// 池的读-改-写段由包级 upsertMu 串行,调用方无需自带锁。
func (a *StoreAdapter) UpsertAirportNodes(ctx context.Context, airportName string, fetchedNodes []*subscription.Node) error {
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

	// 第二步:加载当前池
	oldPool, err := a.store.LoadNodePool()
	if err != nil {
		return fmt.Errorf("load old pool: %w", err)
	}

	// 第三步:只对该机场的旧节点做 MergePool(carry-forward 检测状态),其他机场不动
	var airportOldPool []*subscription.Node
	var otherNodes []*subscription.Node
	for _, n := range oldPool {
		if n.Source == airportName {
			airportOldPool = append(airportOldPool, n)
		} else {
			otherNodes = append(otherNodes, n)
		}
	}

	mergedAirport := subscription.MergePool(airportOldPool, fetchedNodes)

	newPool := append(mergedAirport, otherNodes...)

	// 第四步:写回
	if err := a.store.SaveNodePool(newPool); err != nil {
		return fmt.Errorf("save merged pool: %w", err)
	}

	return nil
}
