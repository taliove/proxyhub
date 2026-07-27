package aggregator

import (
	"errors"
	"fmt"
	"time"

	"github.com/taliove/proxyhub/internal/subscription"
)

// ErrPurgeConflict 清空机场节点与进行中的刷新任务冲突(拒绝而非等待)。
var ErrPurgeConflict = errors.New("purge conflicts with a running refresh job")

// PurgeAirportNodes 一键清空机场节点(CONTEXT.md「机场节点清空」):
// 内存池剔除全部 Source != self-hosted 条目 + DB 快照同步删除,双清缺一不可——
// 只清 DB 订阅仍从内存池下发旧节点(现有 handleDeleteSelfNode 的已知瑕疵,此处不得复制)。
// 自建节点豁免;屏蔽名单/名称覆盖保留(关联表语义见 store.DeleteAirportNodes)。
//
// 并发语义(spec 遗留待决 4 定夺:拒绝而非等待):
// 任何刷新任务(全量/单机场)进行中时拒绝清空,返回 ErrPurgeConflict——
// 否则进行中的刷新可能在清空后把旧节点写回池,用户看到"刚清空又冒回来"的混乱结果。
// refreshStartMu 把"冲突检查 + 双清"包成临界区,消除 TOCTOU:
// 清空完成前新刷新无法发起(发起方走 startRefresh 的同一互斥)。
func (a *Aggregator) PurgeAirportNodes() (int, error) {
	return a.PurgeAirportNodesForUser(0)
}

// PurgeAirportNodesForUser 清空指定用户分片的机场节点(ticket 07):
// 只清该用户池与 DB 中该 user_id 的机场节点,其他用户分片不受影响。
// userID=0 为旧行为(跨用户全清,兼容既有调用与测试)。
func (a *Aggregator) PurgeAirportNodesForUser(userID int64) (int, error) {
	a.refreshStartMu.Lock()
	defer a.refreshStartMu.Unlock()

	if keys := a.refreshJobs.RunningKeys(refreshJobKindName); len(keys) > 0 {
		return 0, fmt.Errorf("%w: running keys %v", ErrPurgeConflict, keys)
	}

	// 先清 DB:失败则内存池原样不动,两侧保持一致
	// (反过来内存先清、DB 再失败会留下"内存空、DB 有、重启复活"的分裂状态)。
	removed, err := a.st.DeleteAirportNodesForUser(userID)
	if err != nil {
		return 0, fmt.Errorf("delete airport nodes: %w", err)
	}

	a.mu.Lock()
	keptTotal := 0
	for uid, pool := range a.pools {
		if userID > 0 && uid != userID {
			continue
		}
		kept := make([]*subscription.Node, 0, len(pool))
		for _, n := range pool {
			if n.Source == subscription.SourceSelfHosted {
				kept = append(kept, n)
			}
		}
		a.pools[uid] = kept
		keptTotal += len(kept)
	}
	a.lastUpdate = time.Now()
	a.mu.Unlock()

	a.logger.Info("airport nodes purged", "removed", removed, "kept_self_hosted", keptTotal, "user_id", userID)
	return int(removed), nil
}
