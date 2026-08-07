package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

// 订阅地址精选 node_picks(spec #70 / issue #79):
// 每个订阅地址可配置精选节点集(NodeKey 数组)。非空时在订阅过滤链最前做
// 候选集替换——池∩精选先行,再流经既有关键词黑白名单/屏蔽/stale/可用性过滤,
// 屏蔽与下架天然仍剔除(显式排除与消亡优先于点名)。空精选 = 未配置 = 零回归。
// NodeKey 记忆:改名(server:port 不变)仍命中,下架自然失效、复活自动恢复。

// endpointNodePicks 解析端点精选原始 JSON 为 NodeKey 列表。
// 空串 = 未配置 -> nil(调用方短路,零回归);解析失败降级为 nil 并告警
// (宁可全量,与过滤链读取失败的降级风格一致,不把订阅打挂)。
func (s *Server) endpointNodePicks(ep *store.Endpoint) []string {
	if ep.NodePicks == "" {
		return nil
	}
	var picks []string
	if err := json.Unmarshal([]byte(ep.NodePicks), &picks); err != nil {
		s.logger.Warn("parse endpoint node picks failed, treating as unconfigured",
			"endpoint", ep.ID, "error", err)
		return nil
	}
	return picks
}

// filterByNodePicks 精选候选集替换:非空 picks 时只保留 NodeKey 命中的节点
// (保持池原顺序;精选是白名单语义,不含排序)。空 picks 原样返回(零回归)。
// 返回新切片,不修改入参。
func filterByNodePicks(nodes []*subscription.Node, picks []string) []*subscription.Node {
	if len(picks) == 0 {
		return nodes
	}
	picked := make(map[string]bool, len(picks))
	for _, key := range picks {
		picked[key] = true
	}
	result := make([]*subscription.Node, 0, len(nodes))
	for _, n := range nodes {
		if picked[n.NodeKey()] {
			result = append(result, n)
		}
	}
	return result
}

// handleUpdateEndpointNodePicks 设置订阅地址的精选节点集(issue #79)。
// PUT /api/endpoints/{id}/node-picks,请求体 {"node_picks":["<NodeKey>",...]};
// 空数组/缺省 = 清空(落库空串,回到全量)。非法请求体 -> 400;
// 端点不存在或属他人 -> 404(ticket 07 属主校验,不暴露存在性)。
func (s *Server) handleUpdateEndpointNodePicks(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	var req struct {
		NodePicks []string `json:"node_picks"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	// 空精选落库为空串(=未配置,零回归);非空落库为规范 JSON 数组。
	raw := ""
	if len(req.NodePicks) > 0 {
		data, err := json.Marshal(req.NodePicks)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		raw = string(data)
	}

	if err := s.st.UpdateEndpointNodePicksForUser(EffectiveUserID(scope), id, raw); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		s.logger.Error("update endpoint node picks failed", "endpoint", id, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}
