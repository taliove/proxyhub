package server

import (
	"strings"

	"github.com/taliove/proxyhub/internal/subscription"
)

// applyNameSlots 名称槽位层(ADR 0047 / issue #96):命名链"精选 alias > 槽位名 >
// 模板标准化 > 原名"中的槽位层,在标准化之后、精选 alias 之前应用。
// 命中节点浅拷贝置 DisplayName=槽位名,绝不改入参(池共享指针)。
// 无槽位或读取失败时原样返回(零回归/降级,同 resolveNameConfig 哲学)。
//
// 槽位名支持模板变量(issue #104 方向):含 "{" 的名字按当前挂载节点渲染
// ({emoji} {region} {region_code} {source} {source_abbr} {original_name}),
// 与标准化模板同一变量表——"主节点-{region}"这类名字在转移给其他地区的
// 节点后自动跟随。渲染依赖数据读取失败时降级为用原始字面量。
func (s *Server) applyNameSlots(nodes []*subscription.Node, userID int64) []*subscription.Node {
	slots, err := s.st.SlotNameByNodeKeyForUser(userID)
	if err != nil {
		s.logger.Warn("list name slots failed, skipping slot overlay", "error", err)
		return nodes
	}
	if len(slots) == 0 {
		return nodes
	}

	needRender := false
	for _, name := range slots {
		if strings.Contains(name, "{") {
			needRender = true
			break
		}
	}
	var abbrs map[string]string
	var regions map[string]subscription.RegionInfo
	if needRender {
		var err error
		abbrs, err = s.st.AirportAbbreviations()
		if err != nil {
			s.logger.Warn("build airport abbreviations failed, slot names render as literal", "error", err)
			needRender = false
		} else {
			abbrs[subscription.SourceSelfHosted] = "SELF"
			regions, err = s.st.RegionInfoMap()
			if err != nil {
				s.logger.Warn("build region info failed, slot names render as literal", "error", err)
				needRender = false
			}
		}
	}

	result := make([]*subscription.Node, 0, len(nodes))
	for _, n := range nodes {
		name, ok := slots[n.NodeKey()]
		if !ok {
			result = append(result, n)
			continue
		}
		if needRender && strings.Contains(name, "{") {
			// 变量渲染与标准化同一 Standardizer;{index} 对槽位无意义,恒为 01
			name = subscription.NewStandardizer(name, abbrs, regions).Format(n, 1)
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
