package detection

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"

	"github.com/taliove/proxyhub/internal/jobs"
	"github.com/taliove/proxyhub/internal/subscription"
)

// batchStabilitySingletonKey 批量"出网+稳定性"任务的全局单例 key(同一时刻只跑一个)。
const batchStabilitySingletonKey = "batch_stability"

// batchStabilityParams 批量"出网+稳定性"参数:节点 key 列表。凭证不入库,活节点经内存旁路传递。
type batchStabilityParams struct {
	NodeKeys []string `json:"node_keys"`
	// Scope 触发范围标记("all"/"selected"),仅用于任务中心展示,不影响执行语义
	Scope string `json:"scope,omitempty"`
}

// StabilityCheckRunner 运行单节点"出网+稳定性"检查:出网画像 + 稳定性评分,不含解锁/测速。
// 与 ExamRunner/SimplifiedExamRunner 签名一致,可直接注入 *Detector.ExamStreamEgressStability。
type StabilityCheckRunner func(ctx context.Context, node *subscription.Node, emit func(ExamEvent)) ExamReport

// batchStabilityKind 批量"出网+稳定性"kind:逐节点跑检查,串行,游标续跑,每节点落历史
// (带 source=stability_check 来源标记)。契约与 batchExamKind 一致:游标 = 已完成节点数计数、
// CancelEvent 补发 cancelled 终止事件、Resumable = true、全局单例 key。
// 事件流复用 BatchExamEvent 结构(wire 格式相同:node_start/node_done/node_error/done/cancelled)。
type batchStabilityKind struct {
	runCheck   StabilityCheckRunner
	onComplete func(nodeKey string, report ExamReport) // 落历史 + 触发标签重算
	onErr      func(error)
	nodes      sync.Map // nodeKey -> *subscription.Node(活节点,仅内存)
}

func (k *batchStabilityKind) Name() string    { return batchStabilitySingletonKey }
func (k *batchStabilityKind) Resumable() bool { return true }

// CancelEvent 取消时补发的终止事件。
func (k *batchStabilityKind) CancelEvent() (json.RawMessage, bool) {
	b, err := json.Marshal(BatchExamEvent{Phase: "cancelled"})
	if err != nil {
		if k.onErr != nil {
			k.onErr(fmt.Errorf("batch_stability: marshal cancel event: %w", err))
		}
		return nil, false
	}
	return b, true
}

// Run 批量检查主循环:解析参数 -> 从游标续跑 -> 逐节点串行检查 -> 每节点落历史 + 推进度。
func (k *batchStabilityKind) Run(ctx context.Context, params json.RawMessage, cursor string, emit func(json.RawMessage), progress func(string)) error {
	var p batchStabilityParams
	if err := json.Unmarshal(params, &p); err != nil {
		return fmt.Errorf("batch_stability: unmarshal params: %w", err)
	}

	total := len(p.NodeKeys)
	startIdx := 0

	// 游标续跑:cursor 是已完成节点数的字符串表示
	if cursor != "" {
		completed, err := strconv.Atoi(cursor)
		if err == nil && completed >= 0 && completed < total {
			startIdx = completed
		}
	}

	// 逐节点串行检查(与批量体检同:避免总量大时墙钟失控)
	for i := startIdx; i < total; i++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		nodeKey := p.NodeKeys[i]
		current := i + 1

		k.emitEvent(emit, BatchExamEvent{
			Phase:   "node_start",
			NodeKey: nodeKey,
			Current: current,
			Total:   total,
		})

		// 从内存旁路取活节点
		v, ok := k.nodes.Load(nodeKey)
		if !ok {
			k.emitEvent(emit, BatchExamEvent{
				Phase:   "node_error",
				NodeKey: nodeKey,
				Current: current,
				Total:   total,
				Error:   "node not found in memory pool",
			})
			progress(strconv.Itoa(current))
			continue
		}

		node := v.(*subscription.Node)

		// 运行出网+稳定性检查(检查内部事件不转发,批量任务只推进度)
		report := k.runCheck(ctx, node, func(ExamEvent) {})

		// 有稳定性段才落历史(与单节点体检/批量体检语义一致)
		if report.Stability != nil && k.onComplete != nil {
			k.onComplete(nodeKey, report)
		}

		k.emitEvent(emit, BatchExamEvent{
			Phase:    "node_done",
			NodeKey:  nodeKey,
			NodeName: node.Name,
			Current:  current,
			Total:    total,
			Report:   &report,
		})

		// 记录进度游标
		progress(strconv.Itoa(current))
	}

	k.emitEvent(emit, BatchExamEvent{Phase: "done", Current: total, Total: total})
	return nil
}

// OnComplete jobs 运行时自然完成 hook:批量任务已在 Run 中逐节点调用 k.onComplete,此处空实现。
func (k *batchStabilityKind) OnComplete(key string, runErr error) {}

// emitEvent 辅助函数:marshal + emit,失败时记日志。
func (k *batchStabilityKind) emitEvent(emit func(json.RawMessage), ev BatchExamEvent) {
	b, err := json.Marshal(ev)
	if err != nil {
		if k.onErr != nil {
			k.onErr(fmt.Errorf("batch_stability: marshal event: %w", err))
		}
		return
	}
	emit(b)
}

// BatchStabilityJobManager 批量"出网+稳定性"任务管理器:封装 jobs.Manager,提供启动/取消/订阅接口。
type BatchStabilityJobManager struct {
	mgr  *jobs.Manager
	kind *batchStabilityKind
}

// BatchStabilityJobOption 配置批量"出网+稳定性"任务管理器。
type BatchStabilityJobOption func(*batchStabilityJobConfig)

type batchStabilityJobConfig struct {
	store *jobs.Store
	onErr func(error)
}

// WithBatchStabilityJobStore 注入 jobs 表存储:任务生命周期持久化。
func WithBatchStabilityJobStore(st *jobs.Store) BatchStabilityJobOption {
	return func(c *batchStabilityJobConfig) { c.store = st }
}

// WithBatchStabilityErrorHandler 注入错误回调。
func WithBatchStabilityErrorHandler(h func(error)) BatchStabilityJobOption {
	return func(c *batchStabilityJobConfig) { c.onErr = h }
}

// NewBatchStabilityJobManager 构造批量"出网+稳定性"任务管理器。
// runCheck: 单节点检查运行器(出网画像 + 稳定性评分)。
// onComplete: 每节点完成回调(落历史带来源标记 + 触发标签重算)。
func NewBatchStabilityJobManager(runCheck StabilityCheckRunner, onComplete func(nodeKey string, report ExamReport), opts ...BatchStabilityJobOption) *BatchStabilityJobManager {
	cfg := batchStabilityJobConfig{}
	for _, o := range opts {
		o(&cfg)
	}

	k := &batchStabilityKind{
		runCheck:   runCheck,
		onComplete: onComplete,
		onErr:      cfg.onErr,
	}

	mopts := []jobs.Option{jobs.WithBufferCap(512)}
	if cfg.onErr != nil {
		mopts = append(mopts, jobs.WithErrorHandler(cfg.onErr))
	}
	mgr := jobs.NewManager(cfg.store, mopts...)
	mgr.Register(k)

	return &BatchStabilityJobManager{mgr: mgr, kind: k}
}

// Start 启动批量检查任务。返回任务 key(固定 batchStabilitySingletonKey,全局单例)。
// nodes 是活节点列表(含凭证),存入内存旁路。scope 为触发范围标记,仅记录进 params 供任务中心展示。
func (m *BatchStabilityJobManager) Start(nodeKeys []string, nodes []*subscription.Node, scope string) (string, error) {
	for _, n := range nodes {
		m.kind.nodes.Store(n.NodeKey(), n)
	}

	params, err := json.Marshal(batchStabilityParams{NodeKeys: nodeKeys, Scope: scope})
	if err != nil {
		return "", fmt.Errorf("batch_stability: marshal params: %w", err)
	}

	key := batchStabilitySingletonKey
	sub, err := m.mgr.Open(m.kind.Name(), key, params)
	if err != nil {
		return "", err
	}
	sub.Close() // 立即关闭订阅(任务已启动,此处不消费事件)

	return key, nil
}

// Subscribe 订阅批量检查任务事件流:回放 + 直播。任务不存在时返回错误。
// 调用方需在订阅结束时 Close。
func (m *BatchStabilityJobManager) Subscribe(key string) (*jobs.Subscription, error) {
	return m.mgr.Open(m.kind.Name(), key, nil)
}

// Cancel 取消批量检查任务。
func (m *BatchStabilityJobManager) Cancel(key string) bool {
	return m.mgr.Cancel(m.kind.Name(), key)
}

// RecoverInterrupted 重启恢复:批量检查可续跑,从游标续跑。
func (m *BatchStabilityJobManager) RecoverInterrupted() error {
	return m.mgr.RecoverOwn()
}
