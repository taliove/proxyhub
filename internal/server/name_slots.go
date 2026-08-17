package server

import (
	"sort"
	"strings"

	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

// slotRenderDeps 槽位名渲染依赖(机场简称表 + 地区信息表),与标准化同源。
type slotRenderDeps struct {
	abbrs   map[string]string
	regions map[string]subscription.RegionInfo
}

// loadSlotRenderDeps 读渲染依赖;任一读失败返回 nil(调用方降级为字面量)。
func (s *Server) loadSlotRenderDeps() *slotRenderDeps {
	abbrs, err := s.st.AirportAbbreviations()
	if err != nil {
		s.logger.Warn("build airport abbreviations failed, slot names render as literal", "error", err)
		return nil
	}
	abbrs[subscription.SourceSelfHosted] = "SELF"
	regions, err := s.st.RegionInfoMap()
	if err != nil {
		s.logger.Warn("build region info failed, slot names render as literal", "error", err)
		return nil
	}
	return &slotRenderDeps{abbrs: abbrs, regions: regions}
}

// renderSlotName 渲染单个槽位名:无变量原样返回;index 为 {index} 的取值
// (两位数字,调用方负责编号;非编号路径恒传 1)。
func renderSlotName(name string, n *subscription.Node, deps *slotRenderDeps, index int) string {
	if deps == nil || !strings.Contains(name, "{") {
		return name
	}
	return subscription.NewStandardizer(name, deps.abbrs, deps.regions).Format(n, index)
}

// applyNameSlots 名称槽位层(ADR 0047 / issue #96):命名链"精选 alias > 槽位名 >
// 模板标准化 > 原名"中的槽位层,在标准化之后、精选 alias 之前应用。
// 命中节点浅拷贝置 DisplayName=槽位名,绝不改入参(池共享指针)。
// 无槽位或读取失败时原样返回(零回归/降级,同 resolveNameConfig 哲学)。
//
// 槽位名支持模板变量(含 "{" 即按挂载节点渲染):
// {emoji} {region} {region_code} {source} {source_abbr} {original_name} 与标准化
// 同一变量表;{index} 是槽位序号——渲染前缀({index} 之前部分)相同的多个槽位
// 按 (槽位名, 槽位 ID) 排序从 01 自动往后排(issue #113:同模板同前缀按创建顺序
// 编号,ID tiebreak 保证跨请求确定;跨地区前缀各自成组),避免撞名。
// 渲染依赖读取失败时降级为用原始字面量。
func (s *Server) applyNameSlots(nodes []*subscription.Node, userID int64) []*subscription.Node {
	slots, err := s.st.AssignedNameSlotsForUser(userID)
	if err != nil {
		s.logger.Warn("list name slots failed, skipping slot overlay", "error", err)
		return nodes
	}
	if len(slots) == 0 {
		return nodes
	}

	needRender := false
	needIndex := false
	for _, sl := range slots {
		if strings.Contains(sl.Name, "{") {
			needRender = true
			if store.SlotNameHasIndex(sl.Name) {
				needIndex = true
			}
		}
	}
	var deps *slotRenderDeps
	if needRender {
		deps = s.loadSlotRenderDeps()
	}

	byKey := make(map[string]*subscription.Node, len(nodes))
	for _, n := range nodes {
		byKey[n.NodeKey()] = n
	}
	indexOf := map[string]int{} // nodeKey -> 序号
	if needIndex && deps != nil {
		indexOf = computeSlotIndices(slots, byKey, deps)
	}

	nameOf := make(map[string]string, len(slots))
	for _, sl := range slots {
		nameOf[sl.NodeKey] = sl.Name
	}

	result := make([]*subscription.Node, 0, len(nodes))
	for _, n := range nodes {
		name, ok := nameOf[n.NodeKey()]
		if !ok {
			result = append(result, n)
			continue
		}
		idx := 1
		if i, ok := indexOf[n.NodeKey()]; ok {
			idx = i
		}
		name = renderSlotName(name, n, deps, idx)
		cp := *n // 浅拷贝,避免污染池共享节点
		cp.DisplayName = name
		result = append(result, &cp)
	}
	return result
}

// computeSlotIndices {index} 编号(issue #113),订阅生成与槽位列表预览共用
// 同一求值路径(WYSIWYG):前缀(渲染后的 {index} 之前部分)分组,组内按
// (槽位名, 槽位 ID) 排序从 1 起编号——同模板同前缀按创建顺序(ID 序)编号,
// 跨地区前缀各自成组;空槽与不在候选集内的槽位不参与编号。
func computeSlotIndices(slots []store.NameSlot, byKey map[string]*subscription.Node, deps *slotRenderDeps) map[string]int {
	type entry struct {
		nodeKey  string
		slotName string
		id       int64
		prefix   string
	}
	entries := []entry{}
	for _, sl := range slots {
		n, ok := byKey[sl.NodeKey]
		if !ok || !store.SlotNameHasIndex(sl.Name) {
			continue
		}
		// 前缀 = {index} 之前的渲染结果(其后的字面部分不参与分组)
		prefixTmpl := strings.SplitN(sl.Name, store.SlotIndexPlaceholder, 2)[0]
		prefix := renderSlotName(prefixTmpl, n, deps, 1)
		entries = append(entries, entry{sl.NodeKey, sl.Name, sl.ID, prefix})
	}
	// 排序键 (前缀, 槽位名, 槽位 ID):同前缀不同模板按名排(既有行为),
	// 同模板按创建顺序(ID)排;全键确定,跨请求不漂移。
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].prefix != entries[j].prefix {
			return entries[i].prefix < entries[j].prefix
		}
		if entries[i].slotName != entries[j].slotName {
			return entries[i].slotName < entries[j].slotName
		}
		return entries[i].id < entries[j].id
	})
	groupIdx := map[string]int{}
	indexOf := make(map[string]int, len(entries))
	for _, e := range entries {
		groupIdx[e.prefix]++
		indexOf[e.nodeKey] = groupIdx[e.prefix]
	}
	return indexOf
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
