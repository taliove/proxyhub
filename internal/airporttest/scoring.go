package airporttest

import (
	"math"
	"sort"

	"github.com/taliove/proxyhub/internal/subscription"
)

// SampledNodeResult 抽样节点的单次检活明细,随 dimensions_json 持久化,
// 供详情抽屉「最近测试」报告段展示每节点可用性/延迟(ticket 0037)。
// 旧 run 的 dimensions_json 无此字段,前端降级为只显示汇总。
type SampledNodeResult struct {
	Name      string `json:"name"`            // 展示名(DisplayName 优先,回退机场原名)
	Region    string `json:"region"`          // 地区代码(HK/SG/US 等,可为空)
	Available bool   `json:"available"`       // 本次检活是否可用
	LatencyMs int    `json:"latency_ms"`      // 本次检活延迟(ms),失败为 0
	Error     string `json:"error,omitempty"` // 失败原因(成功时省略)
}

// ScoreDimensions 评分维度明细
type ScoreDimensions struct {
	AvailabilityScore  float64        `json:"availability_score"`  // 可用率得分(0-50)
	LatencyScore       float64        `json:"latency_score"`       // 延迟得分(0-30)
	FetchHealthScore   float64        `json:"fetch_health_score"`  // 拉取健康得分(0-10)
	RegionScore        float64        `json:"region_score"`        // 地区覆盖得分(0-10)
	AvailableNodes     int            `json:"available_nodes"`     // 可用节点数
	TotalNodes         int            `json:"total_nodes"`         // 总节点数
	MeanLatency        float64        `json:"mean_latency_ms"`     // 平均延迟(ms)
	P95Latency         float64        `json:"p95_latency_ms"`      // P95延迟(ms)
	RegionCount        int            `json:"region_count"`        // 覆盖地区数
	RegionDistribution map[string]int `json:"region_distribution"` // 地区分布
	HTTPStatus         int            `json:"http_status"`         // 拉取HTTP状态
	ParseSuccessRate   float64        `json:"parse_success_rate"`  // 解析成功率
	URLReachable       bool           `json:"url_reachable"`       // 订阅URL是否可达(HTTP 2xx)
	// 各维度权重(%,与得分同源输出):URL 可达 50/30/10/10;
	// 不可达按 5:3:1 重归一,拉取健康为 null(N/A)。
	// 旧 run 无此组字段,前端回退硬编码权重(见 useAirportTest.dimensionWeightsOf)。
	AvailabilityWeight float64             `json:"availability_weight"`
	LatencyWeight      float64             `json:"latency_weight"`
	FetchHealthWeight  *float64            `json:"fetch_health_weight"`
	RegionWeight       float64             `json:"region_weight"`
	SampledNodes       []SampledNodeResult `json:"sampled_nodes,omitempty"` // 抽样节点检活明细(仅 completed run)
}

// CalculateScore 计算综合评分(0-100)及维度明细。
//
// 正常权重(URL可达,HTTP 2xx):可用率50% + 延迟30% + 拉取健康10% + 地区覆盖10%。
// 重归一权重(URL不可达,HTTP非2xx):可用率5/9(55.56%) + 延迟3/9(33.33%) + 地区覆盖1/9(11.11%)。
// 拉取健康维度标记N/A,其权重按比例重分配到其余三维度,保证总分仍为0-100。
//
// nodes为空时返回零分与空维度(无错误,符合"机场无节点"的既定行为)。
func CalculateScore(nodes []*subscription.Node, httpStatus, parseFailures, totalLines int) (float64, *ScoreDimensions) {
	dims := &ScoreDimensions{
		TotalNodes:         len(nodes),
		HTTPStatus:         httpStatus,
		RegionDistribution: make(map[string]int),
	}

	// 判断是否需要重归一(URL不可达)
	fetchHealthAvailable := httpStatus >= 200 && httpStatus < 300
	dims.URLReachable = fetchHealthAvailable

	// 原始权重
	availabilityWeight := 50.0
	latencyWeight := 30.0
	fetchHealthWeight := 10.0
	regionWeight := 10.0

	// 重归一:拉取健康N/A时,将其10%按原比例分配到其余三维度
	// 原比例 availability:latency:region = 50:30:10 = 5:3:1
	// 重分配后: 5/9*100 : 3/9*100 : 1/9*100 = 55.56 : 33.33 : 11.11
	if !fetchHealthAvailable {
		availabilityWeight = 5.0 / 9.0 * 100
		latencyWeight = 3.0 / 9.0 * 100
		regionWeight = 1.0 / 9.0 * 100
		fetchHealthWeight = 0
	}

	// 权重与得分同源落库(前端报告直接读;旧 run 无此字段,前端回退硬编码)
	dims.AvailabilityWeight = availabilityWeight
	dims.LatencyWeight = latencyWeight
	dims.RegionWeight = regionWeight
	if fetchHealthAvailable {
		dims.FetchHealthWeight = &fetchHealthWeight
	}

	// 节点为空:各维度零分(权重字段照常落库)
	if len(nodes) == 0 {
		return 0, dims
	}

	// 可用率维度:可用节点数/总节点数 * weight
	availableCount := 0
	var latencies []int
	regionSet := make(map[string]bool)
	for _, n := range nodes {
		if n.Available {
			availableCount++
			latencies = append(latencies, n.Latency)
		}
		if n.Region != "" {
			regionSet[n.Region] = true
			dims.RegionDistribution[n.Region]++
		}
	}
	dims.AvailableNodes = availableCount
	availabilityRate := float64(availableCount) / float64(len(nodes))
	dims.AvailabilityScore = availabilityRate * availabilityWeight

	// 延迟维度:平均延迟与P95延迟各占延迟权重的一半
	// 映射函数(线性):mean≤100ms满分,≥1000ms零分;P95同理
	// 公式:score = max(0, (latencyWeight/2) * (1000 - latency) / 900)
	if len(latencies) > 0 {
		halfLatencyWeight := latencyWeight / 2

		// 计算平均延迟
		sum := 0
		for _, l := range latencies {
			sum += l
		}
		dims.MeanLatency = float64(sum) / float64(len(latencies))
		meanScore := math.Max(0, halfLatencyWeight*(1000-dims.MeanLatency)/900)
		if meanScore > halfLatencyWeight {
			meanScore = halfLatencyWeight
		}

		// 计算P95延迟(排序后取95分位)
		sortedLatencies := make([]int, len(latencies))
		copy(sortedLatencies, latencies)
		sort.Ints(sortedLatencies)
		p95Index := int(float64(len(sortedLatencies)) * 0.95)
		if p95Index >= len(sortedLatencies) {
			p95Index = len(sortedLatencies) - 1
		}
		dims.P95Latency = float64(sortedLatencies[p95Index])
		p95Score := math.Max(0, halfLatencyWeight*(1000-dims.P95Latency)/900)
		if p95Score > halfLatencyWeight {
			p95Score = halfLatencyWeight
		}
		dims.LatencyScore = meanScore + p95Score
	}

	// 拉取健康维度:HTTP 2xx且解析成功率
	// HTTP非2xx时此维度N/A(权重已重归一到其他维度)
	if fetchHealthAvailable {
		if totalLines > 0 {
			parseSuccessCount := totalLines - parseFailures
			dims.ParseSuccessRate = float64(parseSuccessCount) / float64(totalLines)
			dims.FetchHealthScore = dims.ParseSuccessRate * fetchHealthWeight
		} else {
			dims.FetchHealthScore = fetchHealthWeight // 无行数信息默认满分
		}
	}

	// 地区覆盖维度:覆盖地区数
	// 优先区(HK/SG/US)各2分,其他区各1分,上限10分(原始分)
	// 实际得分按重归一后的权重缩放
	dims.RegionCount = len(regionSet)
	rawRegionScore := 0.0
	for region := range regionSet {
		if _, isPriority := PriorityRegions[region]; isPriority {
			rawRegionScore += 2
		} else {
			rawRegionScore += 1
		}
	}
	if rawRegionScore > 10 {
		rawRegionScore = 10
	}
	// 按权重缩放:原始10分对应regionWeight分
	dims.RegionScore = rawRegionScore * (regionWeight / 10.0)

	overall := dims.AvailabilityScore + dims.LatencyScore + dims.FetchHealthScore + dims.RegionScore
	return overall, dims
}
