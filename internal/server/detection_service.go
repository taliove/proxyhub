package server

import (
	"context"
	"log/slog"
	"time"

	"github.com/taliove/proxyhub/internal/detection"
	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

// DetectionService 检测服务(单节点测试/体检透传)。
// 批量检测已任务化:见 DetectionServiceJobs(jobs 运行时),本结构不再承载批量触发/状态。
type DetectionService struct {
	detector   *detection.Detector
	store      *store.Store
	logger     *slog.Logger
	getNodes   func() []*subscription.Node        // 获取内存节点池
	getTargets func() ([]detection.Target, error) // 获取检测目标配置
}

// NewDetectionService 创建检测服务
func NewDetectionService(
	detector *detection.Detector,
	st *store.Store,
	logger *slog.Logger,
	getNodes func() []*subscription.Node,
	getTargets func() ([]detection.Target, error),
) *DetectionService {
	if logger == nil {
		logger = slog.Default()
	}
	return &DetectionService{
		detector:   detector,
		store:      st,
		logger:     logger,
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

// SaveAndRetag 落库单节点检测结果并重算其自动标签。
// 落库失败则跳过(不中断整体检测,下一轮重试);重算为 best-effort(标签腐化可接受,
// 见票据 21:晚间定时重算兜底)。批量检测 job(batch_detection kind)经此函数落库。
func SaveAndRetag(st *store.Store, logger *slog.Logger, node *subscription.Node, results []detection.Result) {
	if err := st.SaveDetectionResults(results, node.Name, node.Source); err != nil {
		logger.Warn("save detection results failed", "node_key", node.NodeKey(), "error", err)
		return
	}
	if err := st.RecomputeNodeTags(node.NodeKey()); err != nil {
		logger.Warn("recompute node tags after detection failed", "node_key", node.NodeKey(), "error", err)
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

// TestBaselineDown 批量快速测速档透传:仅基准下行(与体检基准行同口径)。
func (ds *DetectionService) TestBaselineDown(ctx context.Context, node *subscription.Node) detection.TestResult {
	return ds.detector.TestBaselineDown(ctx, node)
}

// TestSpeedtestStream 单节点快速测速流式档透传:基准端点 + 既有采样流。
func (ds *DetectionService) TestSpeedtestStream(ctx context.Context, node *subscription.Node, onSample func(detection.Sample)) detection.TestResult {
	return ds.detector.TestSpeedtestStream(ctx, node, onSample)
}

// ExamStream 单节点深度体检流式版本:各段串行执行,实时推送事件。
func (ds *DetectionService) ExamStream(ctx context.Context, node *subscription.Node, emit func(detection.ExamEvent)) detection.ExamReport {
	return ds.detector.ExamStream(ctx, node, emit)
}

// ExamStreamSimplified 单节点精简体检流式版本(批量体检专用):出网 + 稳定性 + 基准下行。
func (ds *DetectionService) ExamStreamSimplified(ctx context.Context, node *subscription.Node, emit func(detection.ExamEvent)) detection.ExamReport {
	return ds.detector.ExamStreamSimplified(ctx, node, emit)
}

// ExamStreamEgressStability 单节点"出网+稳定性"检查流式版本:出网画像 + 稳定性评分,
// 不含解锁目标、不测速;报告带 source=stability_check 来源标记。
func (ds *DetectionService) ExamStreamEgressStability(ctx context.Context, node *subscription.Node, emit func(detection.ExamEvent)) detection.ExamReport {
	return ds.detector.ExamStreamEgressStability(ctx, node, emit)
}
