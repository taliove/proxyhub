package subscription

import "time"

// MergePool 合并旧池与新 fetch 的节点池，实现 carry-forward 逻辑（修复刷新抹掉检测状态的 bug）。
//
// 对新池中的每个节点：
//   - 若旧池存在同 NodeKey，carry forward 检测状态（DetectionLastCheck/Available/Latency/带宽结果）
//   - 连接参数（server/port/协议/密码等）用新值（机场可能更新）
//   - LastSeen 更新为当前时间，Stale 清零
//
// 旧池中新池没有的机场节点：
//   - 标记 Stale=true，保留 LastSeen（已有值）
//   - 附加到结果尾部
//   - 自建节点（Source==SourceSelfHosted）不参与 stale 逻辑（由聚合注入保证在场）
//
// 返回合并后的完整池（active 在前，stale 在后），顺序稳定。
func MergePool(oldPool, newPool []*Node) []*Node {
	// 1. 旧池按 NodeKey 索引
	oldByKey := make(map[string]*Node, len(oldPool))
	for _, n := range oldPool {
		oldByKey[n.NodeKey()] = n
	}

	now := time.Now()

	// 2. 处理新池：carry forward 检测状态
	result := make([]*Node, 0, len(newPool)+len(oldPool)/4) // 预估容量
	newKeys := make(map[string]bool, len(newPool))

	for _, newNode := range newPool {
		key := newNode.NodeKey()
		newKeys[key] = true

		if oldNode, exists := oldByKey[key]; exists {
			// 同 NodeKey 存在：carry forward 检测状态，连接参数用新值
			newNode.DetectionLastCheck = oldNode.DetectionLastCheck
			// 判定来源与 DetectionLastCheck 同生命周期,一并 carry forward(ticket 0016)
			newNode.DetectionKind = oldNode.DetectionKind
			// 失败原因与判定来源同生命周期,一并 carry forward(ticket 0017)
			newNode.DetectionFailReason = oldNode.DetectionFailReason
			newNode.DetectionFailDetail = oldNode.DetectionFailDetail
			// 只在旧节点有真实检测结果时才 carry forward Available/Latency
			// （DetectionLastCheck 非零表示做过真实检测）
			if !oldNode.DetectionLastCheck.IsZero() {
				newNode.Available = oldNode.Available
				newNode.Latency = oldNode.Latency
			}
			// 带宽结果无条件 carry forward
			newNode.BandwidthDownMbps = oldNode.BandwidthDownMbps
			newNode.BandwidthUpMbps = oldNode.BandwidthUpMbps
			newNode.BandwidthCheck = oldNode.BandwidthCheck
		}

		// 更新 LastSeen，清零 Stale（active 节点）
		newNode.LastSeen = now
		newNode.Stale = false

		result = append(result, newNode)
	}

	// 3. 旧池中新池没有的机场节点：标记 stale 并附加到尾部
	for _, oldNode := range oldPool {
		key := oldNode.NodeKey()
		if newKeys[key] {
			continue // 已在新池，跳过
		}
		// 自建节点不参与 stale（由聚合注入保证在场，不会"消失"）
		if oldNode.Source == SourceSelfHosted {
			continue
		}

		// 机场节点消失：标记 stale，保留所有状态（含检测结果）
		oldNode.Stale = true
		// LastSeen 保持旧值（最后一次见到的时间）
		if oldNode.LastSeen.IsZero() {
			oldNode.LastSeen = now // 兜底：旧行未记录时用当前
		}
		result = append(result, oldNode)
	}

	return result
}
