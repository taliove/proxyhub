package server

import (
	"encoding/json"
	"net/http"
)

// handleSetNodeFavorite 设置/取消节点收藏(issue #83):POST /api/nodes/{nodeKey}/favorite。
// 收藏是展示层星标(列表标记 + 筛选),服务端持久(node_overrides.favorite),
// 跨刷新/跨设备可见;不参与订阅过滤链,与 #55 精选(下发层)互不影响。
// 按请求者属主写(多租户):同一节点可被不同用户独立收藏。
// 不校验节点是否在池:收藏任意 NodeKey 无害(展示层 join,键消失自然失效),
// 与覆盖层 handleSetNodeOverride 的宽松语义一致。
// 已知边界:禁用态自建节点在前端的行键是合成键 self-node:<id>(不在池),
// 收藏它持久化后,节点启用入池时键变为 server:port[:sni],收藏不跟随
// (留一条永不命中的孤儿行)。影响仅展示层,如需覆盖要在 selfmerge 给禁用行
// 补真实池键,超出本 issue 范围。
func (s *Server) handleSetNodeFavorite(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	nodeKey := r.PathValue("nodeKey")
	if nodeKey == "" {
		http.Error(w, "node_key is required", http.StatusBadRequest)
		return
	}
	// favorite 用指针:缺字段与显式 false 必须可区分,缺字段按 400 拒绝
	// (否则零值 false 会把"忘传字段"静默吞成"取消收藏")。
	var req struct {
		Favorite *bool `json:"favorite"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if req.Favorite == nil {
		http.Error(w, "favorite required", http.StatusBadRequest)
		return
	}

	if err := s.st.SetNodeFavoriteForUser(EffectiveUserID(scope), nodeKey, *req.Favorite); err != nil {
		s.logger.Error("set node favorite failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]any{"success": true})
}
