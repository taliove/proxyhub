package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"golang.org/x/crypto/bcrypt"

	"github.com/taliove/proxyhub/internal/aggregator"
	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

// handleSetup 系统初始化
func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	// 检查是否已初始化
	initialized, _ := s.st.IsSystemInitialized()
	if initialized {
		http.Error(w, "system already initialized", http.StatusBadRequest)
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Security struct {
			BanThreshold int    `json:"ban_threshold"`
			BanDuration  string `json:"ban_duration"`
		} `json:"security"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	// 验证用户名：非空，且禁止使用高敏感蜜罐账号（admin/root 等）
	if req.Username == "" {
		http.Error(w, "username is required", http.StatusBadRequest)
		return
	}
	if isHoneypotUsername(req.Username) {
		http.Error(w, "this username is reserved and cannot be used", http.StatusBadRequest)
		return
	}
	if len(req.Password) < 12 {
		http.Error(w, "password must be at least 12 characters", http.StatusBadRequest)
		return
	}

	// 哈希密码
	hashed, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		s.logger.Error("hash password failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	banThreshold := req.Security.BanThreshold
	if banThreshold <= 0 {
		banThreshold = defaultBanThreshold
	}
	banDuration := req.Security.BanDuration
	if banDuration == "" {
		banDuration = defaultBanDuration.String()
	}

	// 保存配置
	settings := map[string]string{
		"admin_user":      req.Username,
		"admin_pass_hash": string(hashed),
		"ban_threshold":   strconv.Itoa(banThreshold),
		"ban_duration":    banDuration,
	}
	if err := s.st.SaveSystemSettings(settings); err != nil {
		s.logger.Error("save settings failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// 标记已初始化
	if err := s.st.MarkSystemInitialized(); err != nil {
		s.logger.Error("mark initialized failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	s.logger.Info("system initialized", "username", req.Username)
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

// handleListAirports 列出机场
func (s *Server) handleListAirports(w http.ResponseWriter, r *http.Request) {
	airports, err := s.st.ListAirportsWithTestRuns(r.Context())
	if err != nil {
		s.logger.Error("list airports failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(airports)
}

// handleCreateAirport 添加机场
func (s *Server) handleCreateAirport(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
		URL  string `json:"url"`
		Abbr string `json:"abbr"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	airport, err := s.st.CreateAirport(req.Name, req.URL)
	if err != nil {
		s.logger.Error("create airport failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Auto-generate abbr if not provided (ADR 0012)
	finalAbbr := req.Abbr
	if finalAbbr == "" {
		// Get all used abbreviations (excluding this new airport which has no abbr yet)
		used, err := s.st.GetUsedAbbrs(-1) // -1 means don't exclude any
		if err != nil {
			s.logger.Warn("get used abbrs failed", "error", err)
			used = make(map[string]bool) // fallback to empty set
		}
		// Generate and deduplicate
		base := subscription.GenerateAbbreviation(req.Name)
		finalAbbr = subscription.NextFreeAbbr(base, used)
	}

	// Save the final abbr (either explicit or auto-generated)
	if err := s.st.UpdateAirport(airport.ID, req.Name, req.URL, finalAbbr); err != nil {
		s.logger.Warn("set airport abbr failed", "id", airport.ID, "error", err)
	} else {
		airport.Abbr = finalAbbr
	}

	s.logger.Info("airport created", "name", req.Name, "abbr", finalAbbr)
	json.NewEncoder(w).Encode(airport)
}

// handleSuggestAbbr 机场简称建议:输入名称返回后端推导简称,复用
// subscription.GenerateAbbreviation 作为单一事实源(拼音/字母首字母规则)。
// 无法推导时 abbr 为空串,仍返回 200——空是有效结果,不是错误。
func (s *Server) handleSuggestAbbr(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	writeJSON(w, map[string]string{"abbr": subscription.GenerateAbbreviation(name)})
}

// handleToggleAirport 启用/禁用机场
func (s *Server) handleToggleAirport(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	airport, err := s.st.GetAirportByID(id)
	if err != nil {
		http.Error(w, "airport not found", http.StatusNotFound)
		return
	}

	if err := s.st.SetAirportEnabled(id, !airport.Enabled); err != nil {
		s.logger.Error("toggle airport failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	s.logger.Info("airport toggled", "id", id, "enabled", !airport.Enabled)
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

// handleDeleteAirport 删除机场
func (s *Server) handleDeleteAirport(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	if err := s.st.DeleteAirport(id); err != nil {
		s.logger.Error("delete airport failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	s.logger.Info("airport deleted", "id", id)
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

// handleGetSettings 获取系统设置
func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.st.GetSystemSettings()
	if err != nil {
		s.logger.Error("get settings failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// 移除密码字段
	delete(settings, "admin_password")

	json.NewEncoder(w).Encode(settings)
}

// handleSaveSettings 保存系统设置
func (s *Server) handleSaveSettings(w http.ResponseWriter, r *http.Request) {
	var settings map[string]string
	if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	// 不允许修改敏感字段
	delete(settings, "admin_user")
	delete(settings, "admin_password")
	delete(settings, "initialized")
	// Site Path 只在初始化/轮换流程中变更,通用设置接口改写会导致管理面自我锁死
	delete(settings, "site_path")

	if err := s.st.SaveSystemSettings(settings); err != nil {
		s.logger.Error("save settings failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	s.logger.Info("settings saved")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

// handleDashboardStats 仪表盘统计
func (s *Server) handleDashboardStats(w http.ResponseWriter, r *http.Request) {
	nodes := s.nodes.Nodes()

	// 统计可用节点
	availableCount := 0
	totalLatency := 0
	for _, n := range nodes {
		if n.Available {
			availableCount++
			totalLatency += n.Latency
		}
	}

	avgLatency := 0
	if availableCount > 0 {
		avgLatency = totalLatency / availableCount
	}

	// 统计订阅地址
	endpoints, _ := s.st.ListEndpoints()

	// 统计机场
	airports, _ := s.st.ListAirports()

	lastUpdate := s.nodes.LastUpdate()
	lastUpdateStr := "-"
	if !lastUpdate.IsZero() {
		lastUpdateStr = lastUpdate.Format("2006-01-02 15:04:05")
	}

	stats := map[string]interface{}{
		"totalNodes":     len(nodes),
		"availableNodes": availableCount,
		"endpoints":      len(endpoints),
		"airports":       len(airports),
		"lastUpdate":     lastUpdateStr,
		"avgLatency":     avgLatency,
	}

	json.NewEncoder(w).Encode(stats)
}

// handleManualRefresh 经 jobs 运行时发起全量刷新任务,返回任务信息供前端进任务中心查看。
// 同 key 重复触发按单实例附加(不再 409);与进行中的单机场刷新冲突时返回 409。
func (s *Server) handleManualRefresh(w http.ResponseWriter, r *http.Request) {
	jobID, key, started, err := s.nodes.StartRefreshJob(store.RefreshTriggerManual)
	if err != nil {
		if errors.Is(err, aggregator.ErrRefreshConflict) {
			http.Error(w, "conflicts with a running single-airport refresh", http.StatusConflict)
			return
		}
		s.logger.Error("trigger refresh failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	s.logger.Info("manual refresh triggered", "job_id", jobID, "key", key, "started", started)
	writeJSON(w, map[string]any{
		"ok":      true,
		"jobId":   jobID,
		"kind":    "refresh",
		"key":     key,
		"started": started,
	})
}

// handleUpdateAirport 更新机场信息
func (s *Server) handleUpdateAirport(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	var req struct {
		Name string `json:"name"`
		URL  string `json:"url"`
		Abbr string `json:"abbr"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	// Auto-generate abbr if cleared (ADR 0012)
	finalAbbr := req.Abbr
	if finalAbbr == "" {
		// Get all used abbreviations (excluding this airport being updated)
		used, err := s.st.GetUsedAbbrs(id)
		if err != nil {
			s.logger.Warn("get used abbrs failed", "error", err)
			used = make(map[string]bool) // fallback to empty set
		}
		// Generate and deduplicate
		base := subscription.GenerateAbbreviation(req.Name)
		finalAbbr = subscription.NextFreeAbbr(base, used)
	}

	if err := s.st.UpdateAirport(id, req.Name, req.URL, finalAbbr); err != nil {
		s.logger.Error("update airport failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	s.logger.Info("airport updated", "id", id, "name", req.Name, "abbr", finalAbbr)

	// Return updated airport with final abbr so frontend can display it
	airport, err := s.st.GetAirportByID(id)
	if err != nil {
		s.logger.Warn("get updated airport failed", "error", err)
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
		return
	}
	json.NewEncoder(w).Encode(airport)
}

// handleAirportRefresh POST /api/airports/{id}/refresh 发起单机场刷新任务
// (只拉取入池,不含健康检查;秒级,用于刚加机场/换订阅 token 后快速可见)。
// 与进行中的全量刷新冲突时返回 409;不同机场的单机场刷新可并行。
func (s *Server) handleAirportRefresh(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	if _, err := s.st.GetAirportByID(id); err != nil {
		http.Error(w, "airport not found", http.StatusNotFound)
		return
	}

	jobID, key, started, err := s.nodes.StartAirportRefreshJob(store.RefreshTriggerManual, id)
	if err != nil {
		if errors.Is(err, aggregator.ErrRefreshConflict) {
			http.Error(w, "conflicts with a running full refresh", http.StatusConflict)
			return
		}
		s.logger.Error("trigger airport refresh failed", "airport_id", id, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	s.logger.Info("airport refresh triggered", "airport_id", id, "job_id", jobID, "started", started)
	writeJSON(w, map[string]any{
		"ok":      true,
		"jobId":   jobID,
		"kind":    "refresh",
		"key":     key,
		"started": started,
	})
}
