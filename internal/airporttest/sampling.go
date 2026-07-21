package airporttest

import (
	"math/rand"

	"github.com/taliove/proxyhub/internal/subscription"
)

// PriorityRegions 优先地区配额(HK/SG/US),每层最多5个
var PriorityRegions = map[string]int{
	"HK": 5,
	"SG": 5,
	"US": 5,
}

// DefaultRegionQuota 其他地区配额,每层最多2个
const DefaultRegionQuota = 2

// SampleNodes 按地区分层抽样:优先区HK/SG/US每层最多5个,其他每层最多2个,
// 层内随机。无地区信息(Region="")归"未知"层,按其他配额抽样。
// full=true时跳过抽样返回全部节点。返回新切片,不改入参。
func SampleNodes(nodes []*subscription.Node, full bool) []*subscription.Node {
	if full || len(nodes) == 0 {
		return nodes
	}

	// 按地区分层
	layers := make(map[string][]*subscription.Node)
	for _, n := range nodes {
		region := n.Region
		if region == "" {
			region = "UNKNOWN"
		}
		layers[region] = append(layers[region], n)
	}

	// 对每层应用配额并随机抽样
	var sampled []*subscription.Node
	for region, candidates := range layers {
		quota := DefaultRegionQuota
		if q, isPriority := PriorityRegions[region]; isPriority {
			quota = q
		}
		if len(candidates) <= quota {
			sampled = append(sampled, candidates...)
		} else {
			// 随机洗牌后取前quota个
			shuffled := make([]*subscription.Node, len(candidates))
			copy(shuffled, candidates)
			rand.Shuffle(len(shuffled), func(i, j int) {
				shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
			})
			sampled = append(sampled, shuffled[:quota]...)
		}
	}
	return sampled
}
