package server

import (
	"net/http"
)

// viewScopeUserID 拉取统计的用户口径(多租户):超管未 impersonate 时为 0
// (全局口径,单管理员时代语义);普通用户与 impersonate 视角按 EffectiveUserID。
func viewScopeUserID(scope UserScope) int64 {
	if scope.IsSuperAdmin() && scope.ActingUserID == 0 {
		return 0
	}
	return EffectiveUserID(scope)
}

// handleGlobalStats 访问汇总：总拉取数、独立 IP 数、活跃订阅数 + 节点池按地区统计。
// 普通用户只见自己名下订阅地址的统计与自己池的地区分布。
func (s *Server) handleGlobalStats(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	uid := viewScopeUserID(scope)

	total, uniqueIPs, active, err := s.st.GlobalStatsByUser(uid)
	if err != nil {
		s.logger.Error("global stats failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// 补充节点池按地区统计(同一用户口径:uid=0 超管看合并池)
	byRegion := make(map[string]int)
	nodes := s.nodes.NodesForUser(uid)
	for _, node := range nodes {
		byRegion[node.Region]++
	}

	writeJSON(w, map[string]any{
		"total_pulls":      total,
		"unique_ips":       uniqueIPs,
		"active_endpoints": active,
		"byRegion":         byRegion,
	})
}

// handlePullTrend 拉取趋势：最近 N 天每天每个订阅地址的拉取次数(同一用户口径)。
func (s *Server) handlePullTrend(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	days := parseIntDefault(r.URL.Query().Get("days"), 7)
	if days <= 0 || days > 90 {
		days = 7
	}
	trend, err := s.st.PullTrendByUser(viewScopeUserID(scope), days)
	if err != nil {
		s.logger.Error("pull trend failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"trend": trend})
}
