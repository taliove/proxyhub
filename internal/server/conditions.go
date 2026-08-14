package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subfilter"
	"github.com/taliove/proxyhub/internal/subscription"
)

// applyConditions 按订阅地址的节点范围条件过滤节点池(见 internal/subfilter)。
// /sub 与后台预览共用,保证所见即所得。空条件短路返回原池(零回归)。
// 解析失败降级为不过滤(宁可多给节点,与全局过滤链的降级风格一致)。
//
// 注意:条件对所有节点统一生效(含自建节点)。自建节点在全局卫生过滤(白/黑名单/屏蔽)中
// 是 FailBack 安全网而豁免;但 conditions 是用户对本订阅地址的显式取范围意图,应可预测地收窄。
// 默认(空条件)完整保留现状,含自建 FailBack。
func (s *Server) applyConditions(nodes []*subscription.Node, ep *store.Endpoint) []*subscription.Node {
	cond, err := subfilter.Parse(ep.Conditions)
	if err != nil {
		s.logger.Warn("parse endpoint conditions failed, skipping condition filter",
			"endpoint", ep.ID, "error", err)
		return nodes
	}
	return s.applyConditionsResolved(nodes, cond)
}

// applyConditionsResolved 用已解析的条件过滤节点池。tag 维度按需拉取 node_tags;
// 拉取失败时丢弃 tag 维度(保留机场/地区/关键词维度),而非把订阅打空。
func (s *Server) applyConditionsResolved(nodes []*subscription.Node, cond subfilter.Conditions) []*subscription.Node {
	if cond.IsEmpty() {
		return nodes
	}
	if !cond.NeedsTags() {
		return subfilter.Filter(nodes, nil, cond)
	}
	tagsByKey, ok := s.collectNodeTags(nodes)
	if !ok {
		// 标签数据读不出:丢弃 tag 维度,用其余维度过滤(降级=宁可多给节点)。
		// 构造新条件而非原地改,保持不可变。
		cond = subfilter.Conditions{Airports: cond.Airports, Regions: cond.Regions, Keyword: cond.Keyword}
		return subfilter.Filter(nodes, nil, cond)
	}
	return subfilter.Filter(nodes, tagsByKey, cond)
}

// collectNodeTags 批量拉取节点池的自动标签(NodeKey -> tags)。分块调用 ListNodeTags,
// 规避 SQLite 变量数上限。任一分块失败返回 ok=false,交由调用方降级。
func (s *Server) collectNodeTags(nodes []*subscription.Node) (map[string][]string, bool) {
	keys := make([]string, 0, len(nodes))
	for _, n := range nodes {
		keys = append(keys, n.NodeKey())
	}
	const chunk = 500
	result := make(map[string][]string, len(keys))
	for i := 0; i < len(keys); i += chunk {
		end := i + chunk
		if end > len(keys) {
			end = len(keys)
		}
		part, err := s.st.ListNodeTags(keys[i:end])
		if err != nil {
			s.logger.Warn("list node tags for conditions failed, dropping tag filter", "error", err)
			return nil, false
		}
		for k, v := range part {
			result[k] = v
		}
	}
	return result, true
}

// handleUpdateEndpointConditions 设置订阅地址的节点范围条件。请求体即 Conditions 对象。
// 空条件落库为空串(表示全量);非法请求体 -> 400;端点不存在 -> 404。
// ticket 07: 校验属主,行属他人 404。
func (s *Server) handleUpdateEndpointConditions(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	var cond subfilter.Conditions
	if err := json.NewDecoder(r.Body).Decode(&cond); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	raw := ""
	if !cond.IsEmpty() {
		raw, err = cond.Marshal()
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	}

	if err := s.st.UpdateEndpointConditionsForUser(EffectiveUserID(scope), id, raw); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		// JSON 已在上方校验,走到这里只剩真实 DB 错误:服务端故障回 500,不外泄底层信息。
		s.logger.Error("update endpoint conditions failed", "endpoint", id, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handlePreviewConditions 预览一组(未保存的)条件在当前节点池上的命中数,用于编辑时实时反馈。
// total = 全局过滤链后的可下发节点数;count = 再套用条件后命中的节点数。
// 返回 count + 前 N(20)个命中节点明细(名称/地区/延迟/带宽/来源/标签),便于条件配置时预览具体节点。
// 走与 /sub 同一条链,保证所见即所得。ticket 07: 按当前用户视角的节点池。
func (s *Server) handlePreviewConditions(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	effUID := EffectiveUserID(scope)
	var cond subfilter.Conditions
	if err := json.NewDecoder(r.Body).Decode(&cond); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	// 条件预览无端点上下文,精选维度不参与(spec #70:精选是按已保存端点的候选集替换)。
	// 宕机免疫(issue #101)与实发同口径:免疫死节点在预览中也可见/可命中。
	// 逐级计数(issue #35):池被过滤链清空时,调用方能定位是哪一道清零。
	filtered, stages := s.filteredNodesForDeliveryWithStats(s.nodes.NodesForUser(effUID), effUID, nil, s.monitorImmuneKeys(effUID))
	matched := s.applyConditionsResolved(filtered, cond)

	// 取前 20 个命中节点的明细(多于 20 时截断,避免返回体过大;count 保留真实命中数)
	const previewLimit = 20
	detailNodes := matched
	if len(detailNodes) > previewLimit {
		detailNodes = matched[:previewLimit]
	}

	// 拉取节点标签用于明细展示(拉取失败时标签字段留空,不影响其余字段)
	nodeTags, _ := s.collectNodeTags(detailNodes)

	// 组装明细(预览不需要 blocked/unlockResults/稳定性分,传 nil)
	nodeDetails := toNodeViews(detailNodes, nil, nil, nodeTags, nil)

	writeJSON(w, map[string]any{
		"total":  len(filtered),
		"count":  len(matched),
		"nodes":  nodeDetails,
		"stages": stages,
	})
}

