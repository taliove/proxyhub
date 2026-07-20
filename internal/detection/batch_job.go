package detection

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/taliove/proxyhub/internal/subscription"
)

// batchDetectionParams 批量检测任务的持久化参数。
type batchDetectionParams struct {
	// NodeKeys 指定节点列表(空表示全量检测)
	NodeKeys []string `json:"node_keys,omitempty"`
}

// batchDetectionEvent 批量检测事件(SSE 推送格式)。
type batchDetectionEvent struct {
	Phase     string `json:"phase"`     // start/node_done/done/cancelled
	Total     int    `json:"total"`     // 总节点数(phase=start时有效)
	Completed int    `json:"completed"` // 已完成节点数(phase=node_done时递增)
	NodeKey   string `json:"node_key"`  // 当前节点key(phase=node_done时有效)
	NodeName  string `json:"node_name"` // 当前节点名称(phase=node_done时有效)
	Available bool   `json:"available"` // 节点是否可用(phase=node_done时有效)
	Error     string `json:"error"`     // 错误信息(phase=node_done失败时有效)
}

// batchDetectionKind 批量检测任务 kind 实现:按游标逐节点检测,
// 每节点结果即时落 node_health 并重算标签,支持重启续跑。
type batchDetectionKind struct {
	getNodes   func() []*subscription.Node                                  // 获取内存节点池
	getTargets func() ([]Target, error)                                     // 获取检测目标配置
	detectNode func(context.Context, *subscription.Node, []Target) []Result // 单节点检测实现
	saveRetag  func(*subscription.Node, []Result)                           // 保存结果并重算标签
}

func (k *batchDetectionKind) Name() string {
	return "batch_detection"
}

func (k *batchDetectionKind) Resumable() bool {
	return true
}

// CancelEvent 取消时补发的终止事件。
func (k *batchDetectionKind) CancelEvent() (json.RawMessage, bool) {
	data, _ := json.Marshal(batchDetectionEvent{Phase: "cancelled"})
	return data, true
}

// Run 执行批量检测:逐节点检测并即时落库,游标记录已完成节点数。
// cursor 为已完成节点数的字符串形式("0"/"1"/"2"...),空串表示全新启动。
func (k *batchDetectionKind) Run(
	ctx context.Context,
	params json.RawMessage,
	cursor string,
	emit func(json.RawMessage),
	progress func(string),
) error {
	// 解析参数
	var p batchDetectionParams
	if len(params) > 0 && string(params) != "null" {
		if err := json.Unmarshal(params, &p); err != nil {
			return fmt.Errorf("batch_detection: bad params: %w", err)
		}
	}

	// 获取节点与目标
	allNodes := k.getNodes()
	targets, err := k.getTargets()
	if err != nil {
		return fmt.Errorf("batch_detection: get targets: %w", err)
	}
	if len(targets) == 0 {
		return fmt.Errorf("batch_detection: no targets configured")
	}

	// 根据参数筛选节点
	nodes := allNodes
	if len(p.NodeKeys) > 0 {
		keySet := make(map[string]bool)
		for _, k := range p.NodeKeys {
			keySet[k] = true
		}
		filtered := make([]*subscription.Node, 0, len(p.NodeKeys))
		for _, n := range allNodes {
			if keySet[n.NodeKey()] {
				filtered = append(filtered, n)
			}
		}
		nodes = filtered
	}

	// 解析游标(已完成节点数)
	startIdx := 0
	if cursor != "" {
		n, err := strconv.Atoi(cursor)
		if err != nil {
			return fmt.Errorf("batch_detection: bad cursor %q: %w", cursor, err)
		}
		startIdx = n
	}

	// 推送 start 事件
	k.emitEvent(emit, batchDetectionEvent{
		Phase: "start",
		Total: len(nodes),
	})

	// 逐节点检测(从游标恢复)
	for i := startIdx; i < len(nodes); i++ {
		// 检查取消
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		node := nodes[i]

		// 执行检测
		results := k.detectNode(ctx, node, targets)

		// 立即保存结果并重算标签(复用既有钩子)
		k.saveRetag(node, results)

		// 推送 node_done 事件
		available := false
		errMsg := ""
		if len(results) > 0 {
			available = results[0].Available
			errMsg = results[0].Error
		}
		k.emitEvent(emit, batchDetectionEvent{
			Phase:     "node_done",
			Completed: i + 1,
			NodeKey:   node.NodeKey(),
			NodeName:  node.Name,
			Available: available,
			Error:     errMsg,
		})

		// 记录游标(已完成节点数)
		progress(strconv.Itoa(i + 1))
	}

	// 推送 done 事件
	k.emitEvent(emit, batchDetectionEvent{
		Phase: "done",
	})

	return nil
}

// emitEvent 辅助函数:marshal 事件并 emit,marshal 失败静默忽略(已在缓冲区中,无法补救)。
func (k *batchDetectionKind) emitEvent(emit func(json.RawMessage), ev batchDetectionEvent) {
	if emit == nil {
		return
	}
	data, err := json.Marshal(ev)
	if err != nil {
		return
	}
	emit(data)
}
