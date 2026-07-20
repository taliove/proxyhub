package server

import "net/http"

// handleGlobalStats 全局访问汇总：总拉取数、独立 IP 数、活跃订阅数 + 节点池按地区统计
func (s *Server) handleGlobalStats(w http.ResponseWriter, r *http.Request) {
	total, uniqueIPs, active, err := s.st.GlobalStats()
	if err != nil {
		s.logger.Error("global stats failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// 补充节点池按地区统计
	byRegion := make(map[string]int)
	nodes := s.nodes.Nodes()
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

// handlePullTrend 拉取趋势：最近 N 天每天每个订阅地址的拉取次数
func (s *Server) handlePullTrend(w http.ResponseWriter, r *http.Request) {
	days := parseIntDefault(r.URL.Query().Get("days"), 7)
	if days <= 0 || days > 90 {
		days = 7
	}
	trend, err := s.st.PullTrend(days)
	if err != nil {
		s.logger.Error("pull trend failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"trend": trend})
}
