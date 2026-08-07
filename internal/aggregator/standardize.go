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

	return subscription.NewStandardizer(template, abbrs, regions).StandardizeNodes(nodes)
}
