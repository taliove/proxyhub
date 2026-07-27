package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/taliove/proxyhub/internal/store"
	"gopkg.in/yaml.v3"
)

// handleListTemplates lists all templates in the user's library.
// GET /api/templates
func (s *Server) handleListTemplates(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	userID := EffectiveUserID(scope)
	if userID == 0 {
		// Global scope: return empty list (global default is not in any user's library)
		writeJSON(w, map[string]interface{}{"templates": []interface{}{}})
		return
	}

	templates, err := s.st.ListTemplatesForUser(userID)
	if err != nil {
		s.logger.Error("list templates failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Add reference count for each template
	type templateWithRefs struct {
		ID        int64  `json:"id"`
		Name      string `json:"name"`
		IsDefault bool   `json:"is_default"`
		CreatedAt string `json:"created_at"`
		UpdatedAt string `json:"updated_at"`
		RefCount  int    `json:"ref_count"`
	}

	result := make([]templateWithRefs, 0, len(templates))
	for _, t := range templates {
		count, err := s.st.CountEndpointsUsingTemplate(userID, t.Name)
		if err != nil {
			// Degrade to zero rather than failing the whole list; log for diagnosis.
			s.logger.Warn("count template references failed", "template", t.Name, "error", err)
		}
		result = append(result, templateWithRefs{
			ID:        t.ID,
			Name:      t.Name,
			IsDefault: t.IsDefault,
			CreatedAt: t.CreatedAt.Format("2006-01-02 15:04:05"),
			UpdatedAt: t.UpdatedAt.Format("2006-01-02 15:04:05"),
			RefCount:  count,
		})
	}

	writeJSON(w, map[string]interface{}{"templates": result})
}

// handleCreateTemplate creates a new template in the user's library.
// POST /api/templates
func (s *Server) handleCreateTemplate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name    string `json:"name"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		http.Error(w, "template name is required", http.StatusBadRequest)
		return
	}
	if req.Content == "" {
		http.Error(w, "template content is required", http.StatusBadRequest)
		return
	}

	// Validate YAML format
	var probe map[string]any
	if err := yaml.Unmarshal([]byte(req.Content), &probe); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{
			"error": "invalid YAML: " + err.Error(),
		})
		return
	}

	scope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	userID := EffectiveUserID(scope)
	if userID == 0 {
		http.Error(w, "global scope cannot create templates", http.StatusBadRequest)
		return
	}

	tmpl, err := s.st.CreateTemplate(userID, req.Name, req.Content)
	if err != nil {
		s.logger.Error("create template failed", "error", err, "user_id", userID)
		if errors.Is(err, store.ErrQuotaExceeded) {
			writeJSONStatus(w, http.StatusForbidden, map[string]string{
				"error": "template quota exceeded",
			})
			return
		}
		if errors.Is(err, store.ErrDuplicateName) {
			writeJSONStatus(w, http.StatusConflict, map[string]string{
				"error": "template name already exists",
			})
			return
		}
		if errors.Is(err, store.ErrInvalidInput) {
			writeJSONStatus(w, http.StatusBadRequest, map[string]string{
				"error": err.Error(),
			})
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]interface{}{
		"id":         tmpl.ID,
		"name":       tmpl.Name,
		"is_default": tmpl.IsDefault,
		"created_at": tmpl.CreatedAt.Format("2006-01-02 15:04:05"),
	})
}

// handleGetTemplate gets a template by name.
// GET /api/templates/{name}
func (s *Server) handleGetTemplateByName(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		http.Error(w, "template name is required", http.StatusBadRequest)
		return
	}

	scope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	userID := EffectiveUserID(scope)
	if userID == 0 {
		http.Error(w, "global scope cannot access user templates", http.StatusBadRequest)
		return
	}

	tmpl, err := s.st.GetTemplateByName(userID, name)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "template not found", http.StatusNotFound)
			return
		}
		s.logger.Error("get template failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]interface{}{
		"id":         tmpl.ID,
		"name":       tmpl.Name,
		"content":    tmpl.Content,
		"is_default": tmpl.IsDefault,
		"created_at": tmpl.CreatedAt.Format("2006-01-02 15:04:05"),
		"updated_at": tmpl.UpdatedAt.Format("2006-01-02 15:04:05"),
	})
}

// handleUpdateTemplate updates a template's content.
// PUT /api/templates/{name}
func (s *Server) handleUpdateTemplate(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		http.Error(w, "template name is required", http.StatusBadRequest)
		return
	}

	var req struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if req.Content == "" {
		http.Error(w, "template content is required", http.StatusBadRequest)
		return
	}

	// Validate YAML format
	var probe map[string]any
	if err := yaml.Unmarshal([]byte(req.Content), &probe); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{
			"error": "invalid YAML: " + err.Error(),
		})
		return
	}

	scope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	userID := EffectiveUserID(scope)
	if userID == 0 {
		http.Error(w, "global scope cannot update user templates", http.StatusBadRequest)
		return
	}

	if err := s.st.UpdateTemplate(userID, name, req.Content); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "template not found", http.StatusNotFound)
			return
		}
		s.logger.Error("update template failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]bool{"success": true})
}

// handleDeleteTemplate deletes a template from the user's library.
// DELETE /api/templates/{name}
func (s *Server) handleDeleteTemplate(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		http.Error(w, "template name is required", http.StatusBadRequest)
		return
	}

	scope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	userID := EffectiveUserID(scope)
	if userID == 0 {
		http.Error(w, "global scope cannot delete user templates", http.StatusBadRequest)
		return
	}

	// Get reference count before deletion (for response)
	refCount, err := s.st.CountEndpointsUsingTemplate(userID, name)
	if err != nil {
		// Degrade to zero rather than blocking deletion; log for diagnosis.
		s.logger.Warn("count template references failed", "template", name, "error", err)
	}

	if err := s.st.DeleteTemplate(userID, name); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "template not found", http.StatusNotFound)
			return
		}
		s.logger.Error("delete template failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]interface{}{
		"success":   true,
		"ref_count": refCount,
	})
}

// handleSetDefaultTemplate marks a template as the user's default.
// PUT /api/templates/{name}/default
func (s *Server) handleSetDefaultTemplate(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		http.Error(w, "template name is required", http.StatusBadRequest)
		return
	}

	scope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	userID := EffectiveUserID(scope)
	if userID == 0 {
		http.Error(w, "global scope cannot set default user template", http.StatusBadRequest)
		return
	}

	if err := s.st.SetDefaultTemplate(userID, name); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "template not found", http.StatusNotFound)
			return
		}
		s.logger.Error("set default template failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]bool{"success": true})
}
