package aggregator

import (
	"errors"

	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

// standardizePoolNames 对节点池重算标准化名称(issue #51:机场刷新时自动更新名称,
// 不新增定时器、不外打机场)。按 userID 生效设置(租户级回退全局)决定是否标准化;
// 关闭或依赖数据读取失败时降级为不标准化,返回原池。不修改入参切片。
func (a *Aggregator) standardizePoolNames(userID int64, nodes []*subscription.Node) []*subscription.Node {
	val, err := a.st.GetSettingForUser(userID, "standardize_names")
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			a.logger.Warn("get standardize_names setting failed", "error", err)
		}
		// ErrNotFound = 设置未初始化,按声明默认值 false(与降级语义修复口径一致)
		return nodes
	}
	if val != "true" {
		return nodes
	}

	template := subscription.DefaultNameTemplate
	if t, err := a.st.GetSettingForUser(userID, "name_template"); err == nil && t != "" {
		template = t
	} else if err != nil && !errors.Is(err, store.ErrNotFound) {
		a.logger.Warn("get name_template setting failed", "error", err)
	}

	abbrs, err := a.st.AirportAbbreviations()
	if err != nil {
		a.logger.Warn("build airport abbreviations failed, skipping standardization", "error", err)
		return nodes
	}
	// 自建节点无机场简称,注入固定标签 SELF(与 server.applyStandardization 保持一致)
	abbrs[subscription.SourceSelfHosted] = "SELF"

	regions, err := a.st.RegionInfoMap()
	if err != nil {
		a.logger.Warn("build region info failed, skipping standardization", "error", err)
		return nodes
	}

	// 槽位节点跳过标准化(ADR 0047 / issue #96):名字已被用户接管,刷新重算不得
	// 触碰(修复"standardize_names 开启时刷新冲掉手动名"的缺陷——旧实现无此跳过,
	// StandardizeNodes 无条件覆盖 DisplayName)。读失败降级为不跳过。
	var slotKeys map[string]bool
	if slots, serr := a.st.SlotNameByNodeKeyForUser(userID); serr != nil {
		a.logger.Warn("list name slots failed, standardizing all", "error", serr)
	} else if len(slots) > 0 {
		slotKeys = make(map[string]bool, len(slots))
		for k := range slots {
			slotKeys[k] = true
		}
	}
	free := nodes
	if len(slotKeys) > 0 {
		free = make([]*subscription.Node, 0, len(nodes))
		for _, n := range nodes {
			if slotKeys[n.NodeKey()] {
				continue
			}
			free = append(free, n)
		}
	}
	standardized := subscription.NewStandardizer(template, abbrs, regions).StandardizeNodes(free)
	if len(slotKeys) == 0 {
		return standardized
	}
	// 按 NodeKey 映射回原序列,槽位节点保持原样(其下发名由生成链槽位层覆盖)。
	// 同 NodeKey 多节点(转售/合租机场同 server:port 共存)用队列按序弹出,
	// 各拿各的标准化副本,不共享指针。
	byKey := make(map[string][]*subscription.Node, len(standardized))
	for _, n := range standardized {
		k := n.NodeKey()
		byKey[k] = append(byKey[k], n)
	}
	result := make([]*subscription.Node, 0, len(nodes))
	for _, n := range nodes {
		if q := byKey[n.NodeKey()]; len(q) > 0 {
			result = append(result, q[0])
			byKey[n.NodeKey()] = q[1:]
		} else {
			result = append(result, n)
		}
	}
	return result
}
