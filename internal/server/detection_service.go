package server

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/taliove/proxyhub/internal/detection"
	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

// DetectionService 检测任务服务(单飞 + 可取消)
type DetectionService struct {
	detector   *detection.Detector
	store      *store.Store
	running    atomic.Bool
	cancel     context.CancelFunc
	getNodes   func() []*subscription.Node        // 获取内存节点池
	getTargets func() ([]detection.Target, error) // 获取检测目标配置
}

// NewDetectionService 创建检测服务
func NewDetectionService(
	detector *detection.Detector,
	st *store.Store,
	getNodes func() []*subscription.Node,
	getTargets func() ([]detection.Target, error),
) *DetectionService {
	return &DetectionService{
		detector:   detector,
		store:      st,
		getNodes:   getNodes,
		getTargets: getTargets,
	}
}

// DetectionScope 检测范围
type DetectionScope struct {
	Type     string           `json:"type"`                // "all" / "query" / "selected"
	Query    *DetectionFilter `json:"query,omitempty"`     // type=query 时的筛选条件
	NodeKeys []string         `json:"node_keys,omitempty"` // type=selected 时的节点列表
}

// DetectionFilter 前端传来的筛选条件(带 JSON tag,available/blocked 为字符串 "true"/"false")
type DetectionFilter struct {
	Region    string `json:"region"`
	Type      string `json:"type"`
	Available string `json:"available"` // ""/"true"/"false"
	Blocked   string `json:"blocked"`
	Stale     string `json:"stale"` // ""/"true"/"false"
	Source    string `json:"source"`
}

// parseBoolFilter 把 ""/"true"/"false" 转成 *bool（""→nil 表示不筛选）
func parseBoolFilter(s string) *bool {
	switch s {
	case "true":
		v := true
		return &v
	case "false":
		v := false
		return &v
	default:
		return nil
	}
}

// toNodeQuery 把前端筛选条件转成 NodeQuery(处理字符串→*bool)
func (f *DetectionFilter) toNodeQuery() NodeQuery {
	return NodeQuery{
		Region:    f.Region,
		Type:      f.Type,
		Source:    f.Source,
		Available: parseBoolFilter(f.Available),
		Blocked:   parseBoolFilter(f.Blocked),
		Stale:     parseBoolFilter(f.Stale),
	}
}

// DetectionStatus 检测进度状态
type DetectionStatus struct {
	Running        bool      `json:"running"`
	TotalNodes     int       `json:"total_nodes"`
	CompletedNodes int       `json:"completed_nodes"`
	CurrentNode    string    `json:"current_node,omitempty"`
	StartedAt      time.Time `json:"started_at,omitempty"`
}

// TriggerDetection 启动检测任务(单飞:同时只能有一个在跑)
func (ds *DetectionService) TriggerDetection(_ context.Context, scope DetectionScope) error {
	if !ds.running.CompareAndSwap(false, true) {
		return fmt.Errorf("detection already running")
	}

	// 用 context.Background() 而非请求 context:检测是异步后台任务,
	// HTTP handler 返回后请求 context 会被取消,若基于它会导致所有网络操作立即失败。
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	ds.cancel = cancel

	go func() {
		defer ds.running.Store(false)
		defer cancel()
		ds.runDetection(ctx, scope)
	}()

	return nil
}

// CancelDetection 取消当前检测任务
func (ds *DetectionService) CancelDetection() error {
	if !ds.running.Load() {
		return fmt.Errorf("no detection running")
	}
	if ds.cancel != nil {
		ds.cancel()
	}
	return nil
}

// GetStatus 查询当前检测状态
func (ds *DetectionService) GetStatus() DetectionStatus {
	// TODO: 实现更细粒度的进度追踪
	return DetectionStatus{
		Running: ds.running.Load(),
	}
}

// runDetection 执行检测任务(内部实现)
func (ds *DetectionService) runDetection(ctx context.Context, scope DetectionScope) {
	// 1. 圈定检测范围
	allNodes := ds.getNodes()
	nodesToDetect := ds.selectNodes(allNodes, scope)
	if len(nodesToDetect) == 0 {
		return
	}

	// 2. 获取检测目标
	targets, err := ds.getTargets()
	if err != nil || len(targets) == 0 {
		return
	}

	// 3. 执行检测
	resultsMap := ds.detector.DetectAll(ctx, nodesToDetect, targets)

	// 4. 写回结果到节点对象 + node_health
	nodeMap := make(map[string]*subscription.Node)
	for _, n := range nodesToDetect {
		nodeMap[n.NodeKey()] = n
	}

	for nodeKey, results := range resultsMap {
		node := nodeMap[nodeKey]
		if node == nil {
			continue
		}

		// 更新节点的 Available/Latency/DetectionLastCheck。
		// 节点可用性只取通用(连通性)目标的结果;专用解锁目标(如 netflix,当前为骨架/未实现)
		// 不得决定节点是否可用,否则播种的解锁目标会把所有节点误判为不可用。
		if len(results) > 0 {
			if avail, ok := nodeAvailabilityResult(targets, results); ok {
				node.Available = avail.Available
				node.Latency = avail.Latency
			}
			node.DetectionLastCheck = time.Now()
		}

		// 写 node_health(多维结果)
		if err := ds.store.SaveDetectionResults(results, node.Name, node.Source); err != nil {
			// 记录错误但继续(不因单个节点失败而中断)
			continue
		}
	}
}

// nodeAvailabilityResult 从检测结果中选出决定节点可用性的那一条:对应第一个通用(generic)目标。
// results 与 targets 按下标一一对应(detectNode 按 targets 顺序构造)。
// 没有通用目标时返回 ok=false,调用方据此保留既有可用性,不让专用解锁目标篡改节点可用性。
func nodeAvailabilityResult(targets []detection.Target, results []detection.Result) (detection.Result, bool) {
	for i, t := range targets {
		if !t.IsGeneric() {
			continue
		}
		if i < len(results) {
			return results[i], true
		}
		return detection.Result{}, false
	}
	return detection.Result{}, false
}

// selectNodes 根据 scope 圈定待检测节点
func (ds *DetectionService) selectNodes(allNodes []*subscription.Node, scope DetectionScope) []*subscription.Node {
	switch scope.Type {
	case "all":
		return allNodes

	case "query":
		if scope.Query == nil {
			return allNodes
		}
		// 复用 NodeQuery 筛选逻辑(去掉分页,取全部匹配)
		query := scope.Query.toNodeQuery()
		query.Page = 1
		query.PageSize = len(allNodes) + 1         // 一页装下全部,等效不分页
		result := QueryNodes(allNodes, nil, query) // blocked map 传 nil(检测不关心屏蔽状态)
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

// TestNode 单节点即时测试透传(不受单飞限制,独立于批量检测任务)。
func (ds *DetectionService) TestNode(ctx context.Context, node *subscription.Node, mode string) detection.TestResult {
	return ds.detector.TestNode(ctx, node, mode)
}

// TestBandwidthStream 单节点带宽测试流式版本:实时采样并回调。
func (ds *DetectionService) TestBandwidthStream(ctx context.Context, node *subscription.Node, onSample func(detection.Sample)) detection.TestResult {
	return ds.detector.TestBandwidthStream(ctx, node, onSample)
}
