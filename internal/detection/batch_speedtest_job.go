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

// BatchSpeedtestEvent 批量快速测速事件:进度更新(每节点一行)。
type BatchSpeedtestEvent struct {
	Phase    string      `json:"phase"` // "node_start" | "node_done" | "node_error" | "done" | "cancelled"
	NodeKey  string      `json:"node_key,omitempty"`
	NodeName string      `json:"node_name,omitempty"`
	Current  int         `json:"current,omitempty"` // 已完成节点数
	Total    int         `json:"total,omitempty"`   // 总节点数
	Error    string      `json:"error,omitempty"`   // node_error 时的错误信息
	Result   *TestResult `json:"result,omitempty"`  // node_done 时的测速结果(基准下行)
}

// batchSpeedtestParams 批量快速测速参数:节点 key 列表。凭证不入库,活节点经内存旁路传递。
type batchSpeedtestParams struct {
	NodeKeys []string `json:"node_keys"`
	// Scope 触发范围标记("all"/"selected"),仅用于任务中心展示,不影响执行语义
	Scope string `json:"scope,omitempty"`
}

// SpeedtestRunner 基准下行测量器(批量档:仅下行,控成本;签名与 Detector.TestBaselineDown 一致)。
type SpeedtestRunner func(ctx context.Context, node *subscription.Node) TestResult

// batchSpeedtestKind 批量快速测速 kind:逐节点测基准下行,串行,游标续跑,每节点写回。
// 契约照 batchExamKind 抄:游标 = 已完成节点数计数、CancelEvent 补终止帧、Resumable、
// 全局单例 key(见 BatchSpeedtestJobManager.Start)。
type batchSpeedtestKind struct {
	run        SpeedtestRunner
	onComplete func(node *subscription.Node, result TestResult) // 写回 node_health + 内存池
	onErr      func(error)
	nodes      sync.Map // nodeKey -> *subscription.Node(活节点,仅内存)
}

func (k *batchSpeedtestKind) Name() string    { return "batch_speedtest" }
func (k *batchSpeedtestKind) Resumable() bool { return true }

// CancelEvent 取消时补发的终止事件。
func (k *batchSpeedtestKind) CancelEvent() (json.RawMessage, bool) {
	b, err := json.Marshal(BatchSpeedtestEvent{Phase: "cancelled"})
	if err != nil {
		if k.onErr != nil {
			k.onErr(fmt.Errorf("batch_speedtest: marshal cancel event: %w", err))
		}
		return nil, false
	}
	return b, true
}

// Run 批量快速测速主循环:解析参数 -> 从游标续跑 -> 逐节点串行测基准下行 -> 每节点写回 + 推进度。
func (k *batchSpeedtestKind) Run(ctx context.Context, params json.RawMessage, cursor string, emit func(json.RawMessage), progress func(string)) error {
	var p batchSpeedtestParams
	if err := json.Unmarshal(params, &p); err != nil {
		return fmt.Errorf("batch_speedtest: unmarshal params: %w", err)
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

	for i := startIdx; i < total; i++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		nodeKey := p.NodeKeys[i]
		current := i + 1

		k.emitEvent(emit, BatchSpeedtestEvent{
			Phase:   "node_start",
			NodeKey: nodeKey,
			Current: current,
			Total:   total,
		})

		// 从内存旁路取活节点
		v, ok := k.nodes.Load(nodeKey)
		if !ok {
			k.emitEvent(emit, BatchSpeedtestEvent{
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

		// 测基准下行(成功与失败结果都写回:失败让节点视图看到"测过但失败"而非停留旧值)
		result := k.run(ctx, node)
		rc := result
		if k.onComplete != nil {
			k.onComplete(node, result)
		}

		k.emitEvent(emit, BatchSpeedtestEvent{
			Phase:    "node_done",
			NodeKey:  nodeKey,
			NodeName: node.Name,
			Current:  current,
			Total:    total,
			Result:   &rc,
		})

		progress(strconv.Itoa(current))
	}

	k.emitEvent(emit, BatchSpeedtestEvent{Phase: "done", Current: total, Total: total})
	return nil
}

// OnComplete jobs 运行时自然完成 hook:批量任务已在 Run 中逐节点写回,此处无需额外动作。
func (k *batchSpeedtestKind) OnComplete(key string, runErr error) {}

// emitEvent 辅助函数:marshal + emit,失败时记日志。
func (k *batchSpeedtestKind) emitEvent(emit func(json.RawMessage), ev BatchSpeedtestEvent) {
	b, err := json.Marshal(ev)
	if err != nil {
		if k.onErr != nil {
			k.onErr(fmt.Errorf("batch_speedtest: marshal event: %w", err))
		}
		return
	}
	emit(b)
}

// BatchSpeedtestJobManager 批量快速测速任务管理器:封装 jobs.Manager,提供启动/取消/订阅接口。
type BatchSpeedtestJobManager struct {
	mgr  *jobs.Manager
	kind *batchSpeedtestKind
}

// BatchSpeedtestJobOption 配置批量快速测速任务管理器。
type BatchSpeedtestJobOption func(*batchSpeedtestJobConfig)

type batchSpeedtestJobConfig struct {
	store *jobs.Store
	onErr func(error)
}

// WithBatchSpeedtestJobStore 注入 jobs 表存储:任务生命周期持久化。
func WithBatchSpeedtestJobStore(st *jobs.Store) BatchSpeedtestJobOption {
	return func(c *batchSpeedtestJobConfig) { c.store = st }
}

// WithBatchSpeedtestErrorHandler 注入错误回调。
func WithBatchSpeedtestErrorHandler(h func(error)) BatchSpeedtestJobOption {
	return func(c *batchSpeedtestJobConfig) { c.onErr = h }
}

// NewBatchSpeedtestJobManager 构造批量快速测速任务管理器。
// run: 基准下行测量器(仅下行);onComplete: 每节点完成回调(写回 node_health + 内存池)。
func NewBatchSpeedtestJobManager(run SpeedtestRunner, onComplete func(node *subscription.Node, result TestResult), opts ...BatchSpeedtestJobOption) *BatchSpeedtestJobManager {
	cfg := batchSpeedtestJobConfig{}
	for _, o := range opts {
		o(&cfg)
	}

	k := &batchSpeedtestKind{
		run:        run,
		onComplete: onComplete,
		onErr:      cfg.onErr,
	}

	mopts := []jobs.Option{jobs.WithBufferCap(512)}
	if cfg.onErr != nil {
		mopts = append(mopts, jobs.WithErrorHandler(cfg.onErr))
	}
	mgr := jobs.NewManager(cfg.store, mopts...)
	mgr.Register(k)

	return &BatchSpeedtestJobManager{mgr: mgr, kind: k}
}

// Start 启动批量快速测速任务:nodeKeys 为空则对全部节点测速。返回任务 key(供订阅/取消)。
// nodes 是活节点列表(含凭证),存入内存旁路。scope 为触发范围标记("all"/"selected"),
// 仅记录进 params 供任务中心展示。
func (m *BatchSpeedtestJobManager) Start(nodeKeys []string, nodes []*subscription.Node, scope string) (string, error) {
	for _, n := range nodes {
		m.kind.nodes.Store(n.NodeKey(), n)
	}

	params, err := json.Marshal(batchSpeedtestParams{NodeKeys: nodeKeys, Scope: scope})
	if err != nil {
		return "", fmt.Errorf("batch_speedtest: marshal params: %w", err)
	}

	// 任务 key 固定为 "batch_speedtest"(全局单例,同一时刻只能跑一个批量快速测速)
	key := "batch_speedtest"
	sub, err := m.mgr.Open(m.kind.Name(), key, params)
	if err != nil {
		return "", err
	}
	sub.Close() // 立即关闭订阅(任务已启动,此处不消费事件)

	return key, nil
}

// Subscribe 订阅批量快速测速任务事件流:回放 + 直播。任务不存在时返回错误。
// 用 Attach 而非 Open:订阅不得幻影启动新任务(Open 语义是"无则启动")。
// 调用方需在订阅结束时 Close。
func (m *BatchSpeedtestJobManager) Subscribe(key string) (*jobs.Subscription, error) {
	return m.mgr.Attach(m.kind.Name(), key)
}

// Cancel 取消批量快速测速任务。
func (m *BatchSpeedtestJobManager) Cancel(key string) bool {
	return m.mgr.Cancel(m.kind.Name(), key)
}

// RecoverInterrupted 重启恢复:批量快速测速可续跑,从游标续跑。
func (m *BatchSpeedtestJobManager) RecoverInterrupted() error {
	return m.mgr.RecoverOwn()
}
