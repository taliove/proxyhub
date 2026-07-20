package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/taliove/proxyhub/internal/store"
)

// handleGetDistributionConfig 获取流量分发全局配置
func (s *Server) handleGetDistributionConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.st.GetDistributionConfig()
	if err != nil {
		s.logger.Error("get distribution config failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, cfg)
}

// handleUpdateDistributionConfig 更新流量分发全局配置
func (s *Server) handleUpdateDistributionConfig(w http.ResponseWriter, r *http.Request) {
	// 读取现有配置
	existing, err := s.st.GetDistributionConfig()
	if err != nil {
		s.logger.Error("get existing config failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// 解析请求的部分更新
	var updates map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	// 合并更新到现有配置
	if v, ok := updates["enabled"]; ok {
		if enabled, ok := v.(bool); ok {
			existing.Enabled = enabled
		}
	}
	if v, ok := updates["listen_port"]; ok {
		if port, ok := v.(float64); ok {
			existing.ListenPort = int(port)
		}
	}
	if v, ok := updates["domain"]; ok {
		if domain, ok := v.(string); ok {
			existing.Domain = domain
		}
	}
	if v, ok := updates["protocol"]; ok {
		if protocol, ok := v.(string); ok {
			existing.Protocol = protocol
		}
	}
	if v, ok := updates["network"]; ok {
		if network, ok := v.(string); ok {
			existing.Network = network
		}
	}
	if v, ok := updates["uuid"]; ok {
		if uuid, ok := v.(string); ok {
			existing.UUID = uuid
		}
	}
	if v, ok := updates["tls"]; ok {
		if tls, ok := v.(bool); ok {
			existing.TLS = tls
		}
	}
	if v, ok := updates["cert_path"]; ok {
		if certPath, ok := v.(string); ok {
			existing.CertPath = certPath
		}
	}
	if v, ok := updates["key_path"]; ok {
		if keyPath, ok := v.(string); ok {
			existing.KeyPath = keyPath
		}
	}

	// 验证更新后的配置
	if existing.ListenPort < 1 || existing.ListenPort > 65535 {
		http.Error(w, "listen_port must be between 1 and 65535", http.StatusBadRequest)
		return
	}
	if existing.Protocol != "" {
		validProtocols := map[string]bool{"vless": true, "vmess": true, "trojan": true}
		if !validProtocols[existing.Protocol] {
			http.Error(w, "protocol must be one of: vless, vmess, trojan", http.StatusBadRequest)
			return
		}
	}
	if existing.Network != "" {
		validNetworks := map[string]bool{"tcp": true, "ws": true, "grpc": true}
		if !validNetworks[existing.Network] {
			http.Error(w, "network must be one of: tcp, ws, grpc", http.StatusBadRequest)
			return
		}
	}

	// 保存更新后的配置
	if err := s.st.SaveDistributionConfig(existing); err != nil {
		s.logger.Error("save distribution config failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Reload distribution manager if it exists
	if s.distributionMgr != nil {
		nodes := s.nodes.Nodes()
		if err := s.distributionMgr.Reload(r.Context(), nodes); err != nil {
			s.logger.Error("reload distribution manager failed", "error", err)
			// Don't fail the request, config is already saved
		}
	}

	s.logger.Info("distribution config updated", "enabled", existing.Enabled, "port", existing.ListenPort)
	writeJSON(w, map[string]bool{"ok": true})
}

// handleListDistributionPaths 列出所有分发路径
func (s *Server) handleListDistributionPaths(w http.ResponseWriter, r *http.Request) {
	paths, err := s.st.ListDistributionPaths()
	if err != nil {
		s.logger.Error("list distribution paths failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, paths)
}

// handleGetDistributionPath 获取单个分发路径
func (s *Server) handleGetDistributionPath(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	path, err := s.st.GetDistributionPath(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "path not found", http.StatusNotFound)
			return
		}
		s.logger.Error("get distribution path failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, path)
}

// handleCreateDistributionPath 创建分发路径
func (s *Server) handleCreateDistributionPath(w http.ResponseWriter, r *http.Request) {
	var req store.DistributionPath
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	// Validate
	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	if req.Path == "" {
		http.Error(w, "path is required", http.StatusBadRequest)
		return
	}
	if req.LBStrategy == "" {
		req.LBStrategy = "random"
	}
	validStrategies := map[string]bool{"random": true, "round_robin": true, "least_conn": true}
	if !validStrategies[req.LBStrategy] {
		http.Error(w, "lb_strategy must be one of: random, round_robin, least_conn", http.StatusBadRequest)
		return
	}

	path, err := s.st.CreateDistributionPath(&req)
	if err != nil {
		s.logger.Error("create distribution path failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Reload distribution manager if enabled
	if s.distributionMgr != nil {
		nodes := s.nodes.Nodes()
		if err := s.distributionMgr.Reload(r.Context(), nodes); err != nil {
			s.logger.Error("reload distribution manager failed", "error", err)
		}
	}

	s.logger.Info("distribution path created", "id", path.ID, "name", path.Name, "path", path.Path)
	writeJSON(w, path)
}

// handleUpdateDistributionPath 更新分发路径
func (s *Server) handleUpdateDistributionPath(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	var req store.DistributionPath
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	// Validate
	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	if req.Path == "" {
		http.Error(w, "path is required", http.StatusBadRequest)
		return
	}
	if req.LBStrategy != "" {
		validStrategies := map[string]bool{"random": true, "round_robin": true, "least_conn": true}
		if !validStrategies[req.LBStrategy] {
			http.Error(w, "lb_strategy must be one of: random, round_robin, least_conn", http.StatusBadRequest)
			return
		}
	}

	req.ID = id
	if err := s.st.UpdateDistributionPath(&req); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "path not found", http.StatusNotFound)
			return
		}
		s.logger.Error("update distribution path failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Reload distribution manager if enabled
	if s.distributionMgr != nil {
		nodes := s.nodes.Nodes()
		if err := s.distributionMgr.Reload(r.Context(), nodes); err != nil {
			s.logger.Error("reload distribution manager failed", "error", err)
		}
	}

	s.logger.Info("distribution path updated", "id", id, "name", req.Name)
	writeJSON(w, map[string]bool{"ok": true})
}

// handleDeleteDistributionPath 删除分发路径
func (s *Server) handleDeleteDistributionPath(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	if err := s.st.DeleteDistributionPath(id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "path not found", http.StatusNotFound)
			return
		}
		s.logger.Error("delete distribution path failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Reload distribution manager if enabled
	if s.distributionMgr != nil {
		nodes := s.nodes.Nodes()
		if err := s.distributionMgr.Reload(r.Context(), nodes); err != nil {
			s.logger.Error("reload distribution manager failed", "error", err)
		}
	}

	s.logger.Info("distribution path deleted", "id", id)
	writeJSON(w, map[string]bool{"ok": true})
}

// handleToggleDistributionPath 启用/禁用分发路径
func (s *Server) handleToggleDistributionPath(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	path, err := s.st.GetDistributionPath(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "path not found", http.StatusNotFound)
			return
		}
		s.logger.Error("get distribution path failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Toggle enabled state
	path.Enabled = !path.Enabled
	if err := s.st.UpdateDistributionPath(path); err != nil {
		s.logger.Error("toggle distribution path failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Reload distribution manager if enabled
	if s.distributionMgr != nil {
		nodes := s.nodes.Nodes()
		if err := s.distributionMgr.Reload(r.Context(), nodes); err != nil {
			s.logger.Error("reload distribution manager failed", "error", err)
		}
	}

	s.logger.Info("distribution path toggled", "id", id, "enabled", path.Enabled)
	writeJSON(w, map[string]bool{"ok": true, "enabled": path.Enabled})
}

// handleGetDistributionStats 获取分发统计（按时间范围聚合）
func (s *Server) handleGetDistributionStats(w http.ResponseWriter, r *http.Request) {
	// Parse query params
	fromStr := r.URL.Query().Get("from")
	toStr := r.URL.Query().Get("to")

	var from, to time.Time
	var err error

	if fromStr != "" {
		from, err = time.Parse(time.RFC3339, fromStr)
		if err != nil {
			http.Error(w, "invalid from timestamp", http.StatusBadRequest)
			return
		}
	} else {
		from = time.Now().Add(-24 * time.Hour)
	}

	if toStr != "" {
		to, err = time.Parse(time.RFC3339, toStr)
		if err != nil {
			http.Error(w, "invalid to timestamp", http.StatusBadRequest)
			return
		}
	} else {
		to = time.Now()
	}

	// Get all paths
	paths, err := s.st.ListDistributionPaths()
	if err != nil {
		s.logger.Error("list distribution paths failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Aggregate stats for all paths
	type PathStats struct {
		PathID           int64  `json:"path_id"`
		PathName         string `json:"path_name"`
		Path             string `json:"path"`
		TotalUpload      int64  `json:"total_upload"`
		TotalDownload    int64  `json:"total_download"`
		TotalConnections int64  `json:"total_connections"`
	}

	stats := make([]PathStats, 0, len(paths))
	for _, path := range paths {
		stats = append(stats, PathStats{
			PathID:           path.ID,
			PathName:         path.Name,
			Path:             path.Path,
			TotalUpload:      path.TotalUpload,
			TotalDownload:    path.TotalDownload,
			TotalConnections: path.TotalConnections,
		})
	}

	result := map[string]interface{}{
		"from":  from.Format(time.RFC3339),
		"to":    to.Format(time.RFC3339),
		"paths": stats,
	}

	writeJSON(w, result)
}

// handleRestartXray 重启 Xray 进程
func (s *Server) handleRestartXray(w http.ResponseWriter, r *http.Request) {
	if s.distributionMgr == nil {
		http.Error(w, "distribution manager not initialized", http.StatusServiceUnavailable)
		return
	}

	nodes := s.nodes.Nodes()
	if err := s.distributionMgr.Reload(r.Context(), nodes); err != nil {
		s.logger.Error("restart xray failed", "error", err)
		http.Error(w, "failed to restart xray", http.StatusInternalServerError)
		return
	}

	s.logger.Info("xray restarted")
	writeJSON(w, map[string]bool{"ok": true})
}

// handleXrayStatus 获取 Xray 进程状态
func (s *Server) handleXrayStatus(w http.ResponseWriter, r *http.Request) {
	if s.distributionMgr == nil {
		writeJSON(w, map[string]interface{}{
			"initialized": false,
			"running":     false,
		})
		return
	}

	running := s.distributionMgr.IsRunning()
	writeJSON(w, map[string]interface{}{
		"initialized": true,
		"running":     running,
	})
}
