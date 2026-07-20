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

// batchExamConcurrency 批量体检并发度(串行或低并发):避免总量大时墙钟失控。
// 设为 2 供未来调优(现串行实现,带注释常量)。
const batchExamConcurrency = 2

// BatchExamEvent 批量体检事件:进度更新(每节点一行)。
type BatchExamEvent struct {
	Phase    string      `json:"phase"` // "node_start" | "node_done" | "node_error" | "done" | "cancelled"
	NodeKey  string      `json:"node_key,omitempty"`
	NodeName string      `json:"node_name,omitempty"`
	Current  int         `json:"current,omitempty"` // 已完成节点数
	Total    int         `json:"total,omitempty"`   // 总节点数
	Error    string      `json:"error,omitempty"`   // node_error 时的错误信息
	Report   *ExamReport `json:"report,omitempty"`  // node_done 时的简化报告
}

// batchExamParams 批量体检参数:节点 key 列表。凭证不入库,活节点经内存旁路传递。
type batchExamParams struct {
	NodeKeys []string `json:"node_keys"`
}

// SimplifiedExamRunner 运行精简体检:出网 + 稳定性 + 基准下行,跳过多地域 8 区与解锁。
// 与 ExamRunner 签名一致,可直接注入。
type SimplifiedExamRunner func(ctx context.Context, node *subscription.Node, emit func(ExamEvent)) ExamReport

// batchExamKind 批量体检 kind:逐节点跑精简体检,串行或低并发,游标续跑,每节点落历史。
type batchExamKind struct {
	runSimplified SimplifiedExamRunner
	onComplete    func(nodeKey string, report ExamReport) // 落历史 + 触发标签重算
	onErr         func(error)
	nodes         sync.Map // nodeKey -> *subscription.Node(活节点,仅内存)
}

func (k *batchExamKind) Name() string    { return "batch_exam" }
func (k *batchExamKind) Resumable() bool { return true }

// CancelEvent 取消时补发的终止事件。
func (k *batchExamKind) CancelEvent() (json.RawMessage, bool) {
	b, err := json.Marshal(BatchExamEvent{Phase: "cancelled"})
	if err != nil {
		if k.onErr != nil {
			k.onErr(fmt.Errorf("batch_exam: marshal cancel event: %w", err))
		}
		return nil, false
	}
	return b, true
}

// Run 批量体检主循环:解析参数 -> 从游标续跑 -> 逐节点串行体检 -> 每节点落历史 + 推进度。
func (k *batchExamKind) Run(ctx context.Context, params json.RawMessage, cursor string, emit func(json.RawMessage), progress func(string)) error {
	var p batchExamParams
	if err := json.Unmarshal(params, &p); err != nil {
		return fmt.Errorf("batch_exam: unmarshal params: %w", err)
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

	// 逐节点串行体检(未来可改为 semaphore 控制的低并发)
	for i := startIdx; i < total; i++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		nodeKey := p.NodeKeys[i]
		current := i + 1

		// 发射 node_start 事件
		k.emitEvent(emit, BatchExamEvent{
			Phase:   "node_start",
			NodeKey: nodeKey,
			Current: current,
			Total:   total,
		})

		// 从内存旁路取活节点
		v, ok := k.nodes.Load(nodeKey)
		if !ok {
			// 节点不在内存池:发射 node_error 事件,继续下一个
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

		// 运行精简体检(出网 + 稳定性 + 基准下行)
		report := k.runSimplified(ctx, node, func(e ExamEvent) {
			// 体检内部事件不转发(批量体检只推进度,不推详细采样)
		})

		// 有稳定性段才落历史(与单节点体检语义一致)
		if report.Stability != nil && k.onComplete != nil {
			k.onComplete(nodeKey, report)
		}

		// 发射 node_done 事件
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

	// 发射 done 事件
	k.emitEvent(emit, BatchExamEvent{Phase: "done", Current: total, Total: total})
	return nil
}

// OnComplete jobs 运行时自然完成 hook:批量体检自己管理每节点落历史,此处无需额外动作。
func (k *batchExamKind) OnComplete(key string, runErr error) {
	// 批量任务已在 Run 中逐节点调用 k.onComplete,此处空实现
}

// emitEvent 辅助函数:marshal + emit,失败时记日志。
func (k *batchExamKind) emitEvent(emit func(json.RawMessage), ev BatchExamEvent) {
	b, err := json.Marshal(ev)
	if err != nil {
		if k.onErr != nil {
			k.onErr(fmt.Errorf("batch_exam: marshal event: %w", err))
		}
		return
	}
	emit(b)
}

// BatchExamJobManager 批量体检任务管理器:封装 jobs.Manager,提供启动/取消/订阅接口。
type BatchExamJobManager struct {
	mgr  *jobs.Manager
	kind *batchExamKind
}

// BatchExamJobOption 配置批量体检任务管理器。
type BatchExamJobOption func(*batchExamJobConfig)

type batchExamJobConfig struct {
	store *jobs.Store
	onErr func(error)
}

// WithBatchExamJobStore 注入 jobs 表存储:任务生命周期持久化。
func WithBatchExamJobStore(st *jobs.Store) BatchExamJobOption {
	return func(c *batchExamJobConfig) { c.store = st }
}

// WithBatchExamErrorHandler 注入错误回调。
func WithBatchExamErrorHandler(h func(error)) BatchExamJobOption {
	return func(c *batchExamJobConfig) { c.onErr = h }
}

// NewBatchExamJobManager 构造批量体检任务管理器。
// runSimplified: 精简体检运行器(出网 + 稳定性 + 基准下行)。
// onComplete: 每节点完成回调(落历史 + 触发标签重算)。
func NewBatchExamJobManager(runSimplified SimplifiedExamRunner, onComplete func(nodeKey string, report ExamReport), opts ...BatchExamJobOption) *BatchExamJobManager {
	cfg := batchExamJobConfig{}
	for _, o := range opts {
		o(&cfg)
	}

	k := &batchExamKind{
		runSimplified: runSimplified,
		onComplete:    onComplete,
		onErr:         cfg.onErr,
	}

	mopts := []jobs.Option{jobs.WithBufferCap(512)}
	if cfg.onErr != nil {
		mopts = append(mopts, jobs.WithErrorHandler(cfg.onErr))
	}
	mgr := jobs.NewManager(cfg.store, mopts...)
	mgr.Register(k)

	return &BatchExamJobManager{mgr: mgr, kind: k}
}

// Start 启动批量体检任务:nodeKeys 为空则对全部节点体检。返回任务 key(供订阅/取消)。
// nodes 是活节点列表(含凭证),存入内存旁路。
func (m *BatchExamJobManager) Start(nodeKeys []string, nodes []*subscription.Node) (string, error) {
	// 活节点存内存旁路
	for _, n := range nodes {
		m.kind.nodes.Store(n.NodeKey(), n)
	}

	params, err := json.Marshal(batchExamParams{NodeKeys: nodeKeys})
	if err != nil {
		return "", fmt.Errorf("batch_exam: marshal params: %w", err)
	}

	// 任务 key 固定为 "batch_exam"(全局单例,同一时刻只能跑一个批量体检)
	key := "batch_exam"
	sub, err := m.mgr.Open(m.kind.Name(), key, params)
	if err != nil {
		return "", err
	}
	sub.Close() // 立即关闭订阅(任务已启动,此处不消费事件)

	return key, nil
}

// Subscribe 订阅批量体检任务事件流:回放 + 直播。任务不存在时返回错误。
// 调用方需在订阅结束时 Close。
func (m *BatchExamJobManager) Subscribe(key string) (*jobs.Subscription, error) {
	// 使用空 params 附加到现有任务(params 只在启动时用,附加时不读取)
	return m.mgr.Open(m.kind.Name(), key, nil)
}

// Cancel 取消批量体检任务。
func (m *BatchExamJobManager) Cancel(key string) bool {
	return m.mgr.Cancel(m.kind.Name(), key)
}

// RecoverInterrupted 重启恢复:批量体检可续跑,从游标续跑。
func (m *BatchExamJobManager) RecoverInterrupted() error {
	return m.mgr.Recover()
}
