package server

import (
	"encoding/json"
	"net/http"

	"gopkg.in/yaml.v3"
)

// handleGetTemplate 返回当前生效的 Clash 配置模板（用户已保存的，或内嵌默认）。
func (s *Server) handleGetTemplate(w http.ResponseWriter, r *http.Request) {
	tmpl, err := s.st.GetClashTemplate()
	if err != nil {
		s.logger.Error("get clash template failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"template": tmpl})
}

// handleSaveTemplate 保存用户编辑的 Clash 配置模板。
// 保存前校验 YAML 格式，格式错误返回 400，避免落库后订阅生成失效。
func (s *Server) handleSaveTemplate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Template string `json:"template"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if req.Template == "" {
		http.Error(w, "template is required", http.StatusBadRequest)
		return
	}

	// 校验 YAML 格式：解析失败说明用户改坏了，拒绝保存并回传原因
	var probe map[string]any
	if err := yaml.Unmarshal([]byte(req.Template), &probe); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{
			"error": "invalid YAML: " + err.Error(),
		})
		return
	}

	if err := s.st.SetClashTemplate(req.Template); err != nil {
		s.logger.Error("save clash template failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]bool{"success": true})
}

// handleResetTemplate 把配置模板恢复为内嵌默认模板。
func (s *Server) handleResetTemplate(w http.ResponseWriter, r *http.Request) {
	if err := s.st.ResetClashTemplate(); err != nil {
		s.logger.Error("reset clash template failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]bool{"success": true})
}
