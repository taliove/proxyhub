package server

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/taliove/proxyhub/internal/store"
)

// handleListVersions lists version metadata for a template (version number and created_at).
// GET /api/templates/{name}/versions
func (s *Server) handleListVersions(w http.ResponseWriter, r *http.Request) {
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

	versions, err := s.st.ListVersions(userID, name)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "template not found", http.StatusNotFound)
			return
		}
		s.logger.Error("list versions failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Format response with metadata only (no content)
	type versionMeta struct {
		Version   int    `json:"version"`
		CreatedAt string `json:"created_at"`
	}
	result := make([]versionMeta, len(versions))
	for i, v := range versions {
		result[i] = versionMeta{
			Version:   v.Version,
			CreatedAt: v.CreatedAt.Format("2006-01-02 15:04:05"),
		}
	}

	writeJSON(w, map[string]interface{}{"versions": result})
}

// handleGetVersionContent returns the full content of a specific version.
// GET /api/templates/{name}/versions/{version}
func (s *Server) handleGetVersionContent(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		http.Error(w, "template name is required", http.StatusBadRequest)
		return
	}

	versionStr := r.PathValue("version")
	if versionStr == "" {
		http.Error(w, "version is required", http.StatusBadRequest)
		return
	}

	version, err := strconv.Atoi(versionStr)
	if err != nil {
		http.Error(w, "invalid version parameter", http.StatusBadRequest)
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

	v, err := s.st.GetVersionContent(userID, name, version)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "version not found", http.StatusNotFound)
			return
		}
		s.logger.Error("get version content failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]interface{}{
		"version":    v.Version,
		"content":    v.Content,
		"created_at": v.CreatedAt.Format("2006-01-02 15:04:05"),
	})
}
