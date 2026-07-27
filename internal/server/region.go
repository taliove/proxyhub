package server

import (
	"encoding/json"
	"net/http"
)

// handleGetRegionWhitelist 获取地区白名单配置(视角驱动,租户级设置回退链):
// 超管未 impersonate = 全局值;普通用户/impersonate = 本人覆盖(回退全局默认)。
func (s *Server) handleGetRegionWhitelist(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	val, err := s.st.GetSettingForUser(viewScopeUserID(scope), "region_whitelist")
	if err != nil {
		// 无覆盖且无全局默认 = 空白名单(不过滤),与旧"设置项不存在"语义一致
		val = ""
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

// handleSetRegionWhitelist 设置地区白名单(视角驱动):
// 超管未 impersonate = 写全局默认;普通用户/impersonate = 写本人覆盖。
func (s *Server) handleSetRegionWhitelist(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	uid := viewScopeUserID(scope)
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

	if uid == 0 {
		err = s.st.SetSetting("region_whitelist", string(data))
	} else {
		err = s.st.SetUserSetting(uid, "region_whitelist", string(data))
	}
	if err != nil {
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
