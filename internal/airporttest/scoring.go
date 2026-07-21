package airporttest

import (
	"math"

	"github.com/taliove/proxyhub/internal/subscription"
)

// ScoreDimensions 评分维度明细
type ScoreDimensions struct {
	AvailabilityScore float64            `json:"availability_score"` // 可用率得分(0-50)
	LatencyScore      float64            `json:"latency_score"`      // 延迟得分(0-30)
	FetchHealthScore  float64            `json:"fetch_health_score"` // 拉取健康得分(0-10)
	RegionScore       float64            `json:"region_score"`       // 地区覆盖得分(0-10)
	AvailableNodes    int                `json:"available_nodes"`    // 可用节点数
	TotalNodes        int                `json:"total_nodes"`        // 总节点数
	MeanLatency       float64            `json:"mean_latency_ms"`    // 平均延迟(ms)
	P95Latency        float64            `json:"p95_latency_ms"`     // P95延迟(ms)
	RegionCount       int                `json:"region_count"`       // 覆盖地区数
	RegionDistribution map[string]int     `json:"region_distribution"` // 地区分布
	HTTPStatus        int                `json:"http_status"`         // 拉取HTTP状态
	ParseSuccessRate  float64            `json:"parse_success_rate"`  // 解析成功率
}

// CalculateScore 计算综合评分(0-100)及维度明细。
// 权重:可用率50% + 延迟30% + 拉取健康10% + 地区覆盖10%。
// nodes为空时返回零分与空维度(无错误,符合"机场无节点"的既定行为)。
func CalculateScore(nodes []*subscription.Node, httpStatus, parseFailures, totalLines int) (float64, *ScoreDimensions) {
	dims := &ScoreDimensions{
		TotalNodes:         len(nodes),
		HTTPStatus:         httpStatus,
		RegionDistribution: make(map[string]int),
	}

	// 节点为空:各维度零分
	if len(nodes) == 0 {
		return 0, dims
	}

	// 可用率维度(50%):可用节点数/总节点数 * 50
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
	dims.AvailabilityScore = availabilityRate * 50

	// 延迟维度(30%):平均延迟与P95延迟分别贡献15分
	// 映射函数(线性):mean≤100ms满分15,≥1000ms零分;P95同理
	// 公式:score = max(0, 15 * (1000 - latency) / 900)
	if len(latencies) > 0 {
		// 计算平均延迟
		sum := 0
		for _, l := range latencies {
			sum += l
		}
		dims.MeanLatency = float64(sum) / float64(len(latencies))
		meanScore := math.Max(0, 15*(1000-dims.MeanLatency)/900)
		if meanScore > 15 {
			meanScore = 15
		}

		// 计算P95延迟(简化:排序后取95分位)
		sortedLatencies := make([]int, len(latencies))
		copy(sortedLatencies, latencies)
		// 简单冒泡排序
		for i := 0; i < len(sortedLatencies); i++ {
			for j := i + 1; j < len(sortedLatencies); j++ {
				if sortedLatencies[i] > sortedLatencies[j] {
					sortedLatencies[i], sortedLatencies[j] = sortedLatencies[j], sortedLatencies[i]
				}
			}
		}
		p95Index := int(float64(len(sortedLatencies)) * 0.95)
		if p95Index >= len(sortedLatencies) {
			p95Index = len(sortedLatencies) - 1
		}
		dims.P95Latency = float64(sortedLatencies[p95Index])
		p95Score := math.Max(0, 15*(1000-dims.P95Latency)/900)
		if p95Score > 15 {
			p95Score = 15
		}
		dims.LatencyScore = meanScore + p95Score
	}

	// 拉取健康维度(10%):HTTP 2xx且解析成功率
	// HTTP非2xx零分;2xx时按解析成功率计算
	if httpStatus >= 200 && httpStatus < 300 {
		if totalLines > 0 {
			parseSuccessCount := totalLines - parseFailures
			dims.ParseSuccessRate = float64(parseSuccessCount) / float64(totalLines)
			dims.FetchHealthScore = dims.ParseSuccessRate * 10
		} else {
			dims.FetchHealthScore = 10 // 无行数信息默认满分
		}
	}

	// 地区覆盖维度(10%):覆盖地区数
	// 优先区(HK/SG/US)各2分,其他区各1分,上限10分
	dims.RegionCount = len(regionSet)
	for region := range regionSet {
		if _, isPriority := PriorityRegions[region]; isPriority {
			dims.RegionScore += 2
		} else {
			dims.RegionScore += 1
		}
	}
	if dims.RegionScore > 10 {
		dims.RegionScore = 10
	}

	overall := dims.AvailabilityScore + dims.LatencyScore + dims.FetchHealthScore + dims.RegionScore
	return overall, dims
}
