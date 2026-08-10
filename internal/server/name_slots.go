package server

import (
	"github.com/taliove/proxyhub/internal/subscription"
)

// applyNameSlots 名称槽位层(ADR 0047 / issue #96):命名链"精选 alias > 槽位名 >
// 模板标准化 > 原名"中的槽位层,在标准化之后、精选 alias 之前应用。
// 命中节点浅拷贝置 DisplayName=槽位名,绝不改入参(池共享指针)。
// 无槽位或读取失败时原样返回(零回归/降级,同 resolveNameConfig 哲学)。
func (s *Server) applyNameSlots(nodes []*subscription.Node, userID int64) []*subscription.Node {
	slots, err := s.st.SlotNameByNodeKeyForUser(userID)
	if err != nil {
		s.logger.Warn("list name slots failed, skipping slot overlay", "error", err)
		return nodes
	}
	if len(slots) == 0 {
		return nodes
	}
	result := make([]*subscription.Node, 0, len(nodes))
	for _, n := range nodes {
		name, ok := slots[n.NodeKey()]
		if !ok {
			result = append(result, n)
			continue
		}
		cp := *n // 浅拷贝,避免污染池共享节点
		cp.DisplayName = name
		result = append(result, &cp)
	}
	return result
}

// slotKeySetForUser 读槽位占用的 node_key 集合(标准化跳过用);读失败返回 nil
// (降级为不跳过——宁可多标准化,也不让命名链崩)。
func (s *Server) slotKeySetForUser(userID int64) map[string]bool {
	slots, err := s.st.SlotNameByNodeKeyForUser(userID)
	if err != nil {
		s.logger.Warn("list name slots failed, standardizing all", "error", err)
		return nil
	}
	if len(slots) == 0 {
		return nil
	}
	keys := make(map[string]bool, len(slots))
	for k := range slots {
		keys[k] = true
	}
	return keys
}
