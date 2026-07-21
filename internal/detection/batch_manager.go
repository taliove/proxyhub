package detection

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/taliove/proxyhub/internal/jobs"
	"github.com/taliove/proxyhub/internal/subscription"
)

// BatchDetectionManager 批量检测任务管理器:封装 jobs.Manager + batch_detection kind。
type BatchDetectionManager struct {
	mgr  *jobs.Manager
	kind *batchDetectionKind
}

// NewBatchDetectionManager 构造批量检测任务管理器。
func NewBatchDetectionManager(
	store *jobs.Store,
	getNodes func() []*subscription.Node,
	getTargets func() ([]Target, error),
	detectNode func(context.Context, *subscription.Node, []Target) []Result,
	saveRetag func(*subscription.Node, []Result),
	opts ...jobs.Option,
) *BatchDetectionManager {
	kind := &batchDetectionKind{
		getNodes:   getNodes,
		getTargets: getTargets,
		detectNode: detectNode,
		saveRetag:  saveRetag,
	}

	mgr := jobs.NewManager(store, opts...)
	mgr.Register(kind)

	return &BatchDetectionManager{
		mgr:  mgr,
		kind: kind,
	}
}

// Trigger 启动批量检测任务:nodeKeys 指定待检测节点(空表示全量检测)。
// scope 为触发范围标记("all"/"query"/"selected"),仅记录进 params 供任务中心展示。
// 已有任务在运行时返回错误;附加订阅用 Subscribe。
func (m *BatchDetectionManager) Trigger(nodeKeys []string, scope string) error {
	params := batchDetectionParams{NodeKeys: nodeKeys, Scope: scope}
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("marshal params: %w", err)
	}

	_, err = m.mgr.Open(m.kind.Name(), "all", paramsJSON)
	if err != nil {
		return err
	}
	return nil
}

// Cancel 取消当前批量检测任务:无任务或已完成返回 false。
func (m *BatchDetectionManager) Cancel() bool {
	return m.mgr.Cancel(m.kind.Name(), "all")
}

// Subscribe 订阅批量检测任务事件:回放 + 直播。无任务时返回空订阅(已关闭通道)。
func (m *BatchDetectionManager) Subscribe() *jobs.Subscription {
	sub, err := m.mgr.Open(m.kind.Name(), "all", nil)
	if err != nil {
		// 无任务或 kind 未注册(后者是编程错误,已在 Register 时 panic 拦截)
		return &jobs.Subscription{Live: closedChan()}
	}
	return sub
}

// Recover 重启恢复:加载 running 任务并从游标续跑。
func (m *BatchDetectionManager) Recover() error {
	return m.mgr.RecoverOwn()
}

// SweepExpired 清理超过 TTL 的已完成任务。
func (m *BatchDetectionManager) SweepExpired() {
	m.mgr.SweepExpired()
}

func closedChan() <-chan jobs.Event {
	ch := make(chan jobs.Event)
	close(ch)
	return ch
}
