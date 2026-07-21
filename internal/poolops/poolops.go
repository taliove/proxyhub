// Package poolops 提供节点池的单机场维度操作:按来源加载、单机场 upsert 合并。
//
// 从 airporttest 上移(原 PoolOperations/StorePoolAdapter,ADR 0025),成为聚合层
// 共用能力:机场测试的池空补救与单机场刷新(ticket 04)复用同一口径——
// 解析→地区识别→MergePool carry-forward→SaveNodePool。
package poolops

import (
	"fmt"

	"github.com/taliove/proxyhub/internal/geoip"
	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

// Operations 抽象节点池的按源加载与单机场 upsert。
type Operations interface {
	// LoadPoolBySource 返回池中指定来源(机场名)的在架节点(不含 stale)。
	LoadPoolBySource(source string) ([]*subscription.Node, error)
	// UpsertAirportNodes 把新拉取的节点合并进池(单机场范围):
	// 该机场旧节点走 MergePool carry-forward,其他机场节点不动。
	UpsertAirportNodes(airportName string, fetchedNodes []*subscription.Node) error
}

// StoreAdapter 以 store.Store 为底的 Operations 实现。
type StoreAdapter struct {
	store *store.Store
}

// NewStoreAdapter 创建基于 store 的池操作适配器。
func NewStoreAdapter(st *store.Store) *StoreAdapter {
	return &StoreAdapter{store: st}
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
func (a *StoreAdapter) UpsertAirportNodes(airportName string, fetchedNodes []*subscription.Node) error {
	// 第一步:地区识别(有离线 GeoIP 则兜底,不阻断)
	for _, node := range fetchedNodes {
		if node.Region == "" {
			if country, err := geoip.LookupCountry(node.Server); err == nil {
				node.Region = country
			}
		}
	}

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
