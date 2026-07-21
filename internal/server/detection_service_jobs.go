package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/taliove/proxyhub/internal/detection"
	"github.com/taliove/proxyhub/internal/jobs"
	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

// DetectionServiceJobs 基于 jobs 运行时的检测任务服务:批量检测已任务化,
// 单节点测试/体检仍复用既有 DetectionService 的透传方法。
type DetectionServiceJobs struct {
	detector   *detection.Detector
	batchMgr   *detection.BatchDetectionManager
	store      *store.Store
	logger     *slog.Logger
	getNodes   func() []*subscription.Node
	getTargets func() ([]detection.Target, error)

	// 当前任务状态(内存缓存,从订阅事件聚合)
	mu             sync.Mutex
	currentStatus  DetectionStatus
	latestEvents   []jobs.Event
	activeSub      *jobs.Subscription
	stopAggregator func()
}

// NewDetectionServiceJobs 构造基于 jobs 的检测服务。
func NewDetectionServiceJobs(
	batchMgr *detection.BatchDetectionManager,
	detector *detection.Detector,
	st *store.Store,
	logger *slog.Logger,
	getNodes func() []*subscription.Node,
	getTargets func() ([]detection.Target, error),
) *DetectionServiceJobs {
	if logger == nil {
		logger = slog.Default()
	}
	return &DetectionServiceJobs{
		detector:   detector,
		batchMgr:   batchMgr,
		store:      st,
		logger:     logger,
		getNodes:   getNodes,
		getTargets: getTargets,
	}
}

// TriggerDetection 启动批量检测任务:圈定范围 -> 启动 job -> 后台聚合事件更新状态。
func (ds *DetectionServiceJobs) TriggerDetection(_ context.Context, scope DetectionScope) error {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	// 检查是否已有任务在运行
	if ds.currentStatus.Running {
		return fmt.Errorf("detection already running")
	}

	// 圈定节点范围(复用既有 selectNodes 逻辑)
	allNodes := ds.getNodes()
	nodesToDetect := selectNodes(allNodes, scope)
	if len(nodesToDetect) == 0 {
		return fmt.Errorf("no nodes to detect")
	}

	// 提取节点 keys
	nodeKeys := make([]string, len(nodesToDetect))
	for i, n := range nodesToDetect {
		nodeKeys[i] = n.NodeKey()
	}

	// 启动任务(通过 BatchDetectionManager);scope.Type 记入 params 供任务中心展示范围
	if err := ds.batchMgr.Trigger(nodeKeys, scope.Type); err != nil {
		return fmt.Errorf("start batch detection: %w", err)
	}

	// 订阅任务事件并启动后台聚合器
	sub := ds.batchMgr.Subscribe()
	ctx, cancel := context.WithCancel(context.Background())
	ds.activeSub = sub
	ds.stopAggregator = cancel
	ds.currentStatus = DetectionStatus{
		Running:    true,
		TotalNodes: len(nodesToDetect),
		StartedAt:  time.Now(),
	}

	go ds.aggregateEvents(ctx, sub)

	return nil
}

// aggregateEvents 后台聚合任务事件,更新内存状态(供 GetStatus 轮询)。
func (ds *DetectionServiceJobs) aggregateEvents(ctx context.Context, sub *jobs.Subscription) {
	defer sub.Close()

	// 回放已有事件
	for _, ev := range sub.Replay {
		ds.processEvent(ev)
	}

	// 转直播
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-sub.Live:
			if !ok {
				// 任务收口
				ds.mu.Lock()
				ds.currentStatus.Running = false
				ds.activeSub = nil
				ds.stopAggregator = nil
				ds.mu.Unlock()
				return
			}
			ds.processEvent(ev)
		}
	}
}

// processEvent 处理单个事件并更新状态。
func (ds *DetectionServiceJobs) processEvent(ev jobs.Event) {
	var detEv batchDetectionEvent
	if err := json.Unmarshal(ev.Data, &detEv); err != nil {
		return
	}

	ds.mu.Lock()
	defer ds.mu.Unlock()

	// 缓存最近事件(供状态查询)
	ds.latestEvents = append(ds.latestEvents, ev)
	if len(ds.latestEvents) > 100 {
		ds.latestEvents = ds.latestEvents[len(ds.latestEvents)-100:]
	}

	// 更新聚合状态
	switch detEv.Phase {
	case "start":
		ds.currentStatus.TotalNodes = detEv.Total
	case "node_done":
		ds.currentStatus.CompletedNodes = detEv.Completed
		ds.currentStatus.CurrentNode = detEv.NodeName
	case "done", "cancelled":
		ds.currentStatus.Running = false
	}
}

// batchDetectionEvent 批量检测事件(从 detection 包复制,避免循环依赖)。
type batchDetectionEvent struct {
	Phase     string `json:"phase"`
	Total     int    `json:"total"`
	Completed int    `json:"completed"`
	NodeKey   string `json:"node_key"`
	NodeName  string `json:"node_name"`
	Available bool   `json:"available"`
	Error     string `json:"error"`
}

// Recover 恢复重启前未完成的批量检测任务(游标续跑),供 Server.RecoverJobs 调用。
func (ds *DetectionServiceJobs) Recover() error {
	return ds.batchMgr.Recover()
}

// CancelDetection 取消当前批量检测任务。
func (ds *DetectionServiceJobs) CancelDetection() error {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	if !ds.currentStatus.Running {
		return fmt.Errorf("no detection running")
	}

	if !ds.batchMgr.Cancel() {
		return fmt.Errorf("cancel failed")
	}

	return nil
}

// GetStatus 查询当前检测状态(从内存聚合状态返回,兼容既有轮询 API)。
func (ds *DetectionServiceJobs) GetStatus() DetectionStatus {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	return ds.currentStatus
}

// selectNodes 圈定待检测节点(复用既有逻辑,提升为包级函数)。
func selectNodes(allNodes []*subscription.Node, scope DetectionScope) []*subscription.Node {
	switch scope.Type {
	case "all":
		return allNodes

	case "query":
		if scope.Query == nil {
			return allNodes
		}
		query := scope.Query.toNodeQuery()
		query.Page = 1
		query.PageSize = len(allNodes) + 1
		result := QueryNodes(allNodes, nil, query)
		return result.Nodes

	case "selected":
		if len(scope.NodeKeys) == 0 {
			return nil
		}
		keySet := make(map[string]bool)
		for _, k := range scope.NodeKeys {
			keySet[k] = true
		}
		selected := make([]*subscription.Node, 0, len(scope.NodeKeys))
		for _, n := range allNodes {
			if keySet[n.NodeKey()] {
				selected = append(selected, n)
			}
		}
		return selected

	default:
		return nil
	}
}

// TestNode 单节点即时测试透传。
func (ds *DetectionServiceJobs) TestNode(ctx context.Context, node *subscription.Node, mode string) detection.TestResult {
	return ds.detector.TestNode(ctx, node, mode)
}

// TestBandwidthStream 单节点带宽测试流式版本。
func (ds *DetectionServiceJobs) TestBandwidthStream(ctx context.Context, node *subscription.Node, onSample func(detection.Sample)) detection.TestResult {
	return ds.detector.TestBandwidthStream(ctx, node, onSample)
}

// ExamStream 单节点深度体检流式版本。
func (ds *DetectionServiceJobs) ExamStream(ctx context.Context, node *subscription.Node, emit func(detection.ExamEvent)) detection.ExamReport {
	return ds.detector.ExamStream(ctx, node, emit)
}
