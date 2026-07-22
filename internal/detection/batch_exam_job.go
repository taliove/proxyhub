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

// 批量体检模式:simplified(默认,出网+稳定性+基准下行)| full(完整四段,与单节点深度体检同口径)。
const (
	BatchExamModeSimplified = "simplified"
	BatchExamModeFull       = "full"
)

// normalizeBatchExamMode 归一化批量体检模式:空(老任务 params 无此字段,或调用方未指定)
// 一律落 simplified,保证升级后重启游标续跑口径不漂移;未知值报错而非静默降级。
func normalizeBatchExamMode(mode string) (string, error) {
	switch mode {
	case "", BatchExamModeSimplified:
		return BatchExamModeSimplified, nil
	case BatchExamModeFull:
		return BatchExamModeFull, nil
	default:
		return "", fmt.Errorf("batch_exam: unknown mode %q", mode)
	}
}

// newBatchExamParams 构造批量体检参数:mode 归一化后显式落 params,任务中心与续跑口径自描述。
func newBatchExamParams(nodeKeys []string, scope, mode string) (batchExamParams, error) {
	normalized, err := normalizeBatchExamMode(mode)
	if err != nil {
		return batchExamParams{}, err
	}
	return batchExamParams{NodeKeys: nodeKeys, Scope: scope, Mode: normalized}, nil
}

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
	// Scope 触发范围标记("all"/"selected"),仅用于任务中心展示,不影响执行语义
	Scope string `json:"scope,omitempty"`
	// Mode 体检模式:simplified(默认)| full(完整四段)。老任务 params 无此字段,
	// 归一化时按 simplified 处理(升级后重启游标续跑口径不漂移)。
	Mode string `json:"mode,omitempty"`
}

// SimplifiedExamRunner 运行精简体检:出网 + 稳定性 + 基准下行,跳过多地域 8 区与解锁。
// 与 ExamRunner 签名一致,可直接注入。
type SimplifiedExamRunner func(ctx context.Context, node *subscription.Node, emit func(ExamEvent)) ExamReport

// batchExamKind 批量体检 kind:逐节点体检(模式由 params 选 runner),串行或低并发,游标续跑,每节点落历史。
type batchExamKind struct {
	runSimplified SimplifiedExamRunner
	runFull       ExamRunner                              // full 模式:完整四段,与单节点深度体检同口径
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

// resolveRunner 按 params mode 选 runner:空/simplified 用精简 runner,full 用完整 runner。
// SimplifiedExamRunner 与 ExamRunner 签名一致,选中后统一为同一调用形态。
func (k *batchExamKind) resolveRunner(mode string) (SimplifiedExamRunner, error) {
	normalized, err := normalizeBatchExamMode(mode)
	if err != nil {
		return nil, err
	}
	if normalized == BatchExamModeFull {
		if k.runFull == nil {
			return nil, fmt.Errorf("batch_exam: full mode requested but full runner not configured")
		}
		return SimplifiedExamRunner(k.runFull), nil
	}
	if k.runSimplified == nil {
		return nil, fmt.Errorf("batch_exam: simplified runner not configured")
	}
	return k.runSimplified, nil
}

// Run 批量体检主循环:解析参数 -> 按 mode 选 runner -> 从游标续跑 -> 逐节点串行体检 -> 每节点落历史 + 推进度。
func (k *batchExamKind) Run(ctx context.Context, params json.RawMessage, cursor string, emit func(json.RawMessage), progress func(string)) error {
	var p batchExamParams
	if err := json.Unmarshal(params, &p); err != nil {
		return fmt.Errorf("batch_exam: unmarshal params: %w", err)
	}

	// mode 决定每节点跑精简还是完整四段;老任务 params 无 mode 字段,归一化为 simplified。
	runExam, err := k.resolveRunner(p.Mode)
	if err != nil {
		return err
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

		// 运行体检(精简或完整四段,由 params mode 决定)
		report := runExam(ctx, node, func(e ExamEvent) {
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
// runSimplified: 精简体检运行器(出网 + 稳定性 + 基准下行),mode=simplified 或未指定时使用。
// runFull: 完整体检运行器(完整四段,与单节点深度体检同口径),mode=full 时使用。
// onComplete: 每节点完成回调(落历史 + 触发标签重算)。
func NewBatchExamJobManager(runSimplified SimplifiedExamRunner, runFull ExamRunner, onComplete func(nodeKey string, report ExamReport), opts ...BatchExamJobOption) *BatchExamJobManager {
	cfg := batchExamJobConfig{}
	for _, o := range opts {
		o(&cfg)
	}

	k := &batchExamKind{
		runSimplified: runSimplified,
		runFull:       runFull,
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
// nodes 是活节点列表(含凭证),存入内存旁路。scope 为触发范围标记("all"/"selected"),
// 仅记录进 params 供任务中心展示。mode 为体检模式(simplified/full),空按 simplified。
func (m *BatchExamJobManager) Start(nodeKeys []string, nodes []*subscription.Node, scope string, mode string) (string, error) {
	// 活节点存内存旁路
	for _, n := range nodes {
		m.kind.nodes.Store(n.NodeKey(), n)
	}

	p, err := newBatchExamParams(nodeKeys, scope, mode)
	if err != nil {
		return "", err
	}
	params, err := json.Marshal(p)
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
// 用 Attach 而非 Open:订阅不得幻影启动一个 params 为空的假任务。
// 调用方需在订阅结束时 Close。
func (m *BatchExamJobManager) Subscribe(key string) (*jobs.Subscription, error) {
	return m.mgr.Attach(m.kind.Name(), key)
}

// Cancel 取消批量体检任务。
func (m *BatchExamJobManager) Cancel(key string) bool {
	return m.mgr.Cancel(m.kind.Name(), key)
}

// RecoverInterrupted 重启恢复:批量体检可续跑,从游标续跑。
func (m *BatchExamJobManager) RecoverInterrupted() error {
	return m.mgr.RecoverOwn()
}
