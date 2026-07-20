package server

import (
	"encoding/json"
	"net/http"
)

// handleGetRegionWhitelist 获取地区白名单配置
func (s *Server) handleGetRegionWhitelist(w http.ResponseWriter, r *http.Request) {
	val, err := s.st.GetSetting("region_whitelist")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var whitelist []string
	if val != "" {
		if err := json.Unmarshal([]byte(val), &whitelist); err != nil {
			http.Error(w, "invalid whitelist format", http.StatusInternalServerError)
			return
		}
	}

	writeJSON(w, map[string]any{"whitelist": whitelist})
}

// handleSetRegionWhitelist 设置地区白名单
func (s *Server) handleSetRegionWhitelist(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Whitelist []string `json:"whitelist"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	// 验证地区代码（可选：从 region_rules 验证）
	data, err := json.Marshal(req.Whitelist)
	if err != nil {
		http.Error(w, "marshal whitelist failed", http.StatusInternalServerError)
		return
	}

	if err := s.st.SetSetting("region_whitelist", string(data)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.logger.Info("region whitelist updated", "whitelist", req.Whitelist)
	writeJSON(w, map[string]any{"ok": true})
}

// handleListRegions 列出所有可用地区
func (s *Server) handleListRegions(w http.ResponseWriter, r *http.Request) {
	regions, err := s.st.ListRegions()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"regions": regions})
}
