package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/taliove/proxyhub/internal/store"
)

// handleListDistributionNodes lists all distribution nodes (including disabled ones for management UI)
func (s *Server) handleListDistributionNodes(w http.ResponseWriter, r *http.Request) {
	nodes, err := s.st.ListAllDistributionNodes()
	if err != nil {
		s.logger.Error("list distribution nodes failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"nodes": nodes})
}

// handleGetDistributionNode retrieves a single distribution node by ID
func (s *Server) handleGetDistributionNode(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	node, err := s.st.GetDistributionNode(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "distribution node not found", http.StatusNotFound)
			return
		}
		s.logger.Error("get distribution node failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	writeJSON(w, node)
}

// handleCreateDistributionNode creates a new distribution node
func (s *Server) handleCreateDistributionNode(w http.ResponseWriter, r *http.Request) {
	var node store.DistributionNode
	if err := json.NewDecoder(r.Body).Decode(&node); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	// Set default values
	if node.LBStrategy == "" {
		node.LBStrategy = "random"
	}
	node.Enabled = true

	if err := s.st.CreateDistributionNode(&node); err != nil {
		s.logger.Error("create distribution node failed", "error", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Reload distribution manager if enabled
	if s.distributionMgr != nil {
		cfg, err := s.st.GetDistributionConfig()
		if err == nil && cfg.Enabled {
			nodes := s.nodes.Nodes()
			if err := s.distributionMgr.Reload(r.Context(), nodes); err != nil {
				s.logger.Error("reload distribution manager failed", "error", err)
			}
		}
	}

	s.logger.Info("distribution node created", "id", node.ID, "name", node.Name, "path", node.DistributionPath)
	writeJSON(w, node)
}

// handleUpdateDistributionNode updates an existing distribution node
func (s *Server) handleUpdateDistributionNode(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	var node store.DistributionNode
	if err := json.NewDecoder(r.Body).Decode(&node); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	node.ID = id
	if err := s.st.UpdateDistributionNode(&node); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "distribution node not found", http.StatusNotFound)
			return
		}
		s.logger.Error("update distribution node failed", "error", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Reload distribution manager if enabled
	if s.distributionMgr != nil {
		cfg, err := s.st.GetDistributionConfig()
		if err == nil && cfg.Enabled {
			nodes := s.nodes.Nodes()
			if err := s.distributionMgr.Reload(r.Context(), nodes); err != nil {
				s.logger.Error("reload distribution manager failed", "error", err)
			}
		}
	}

	s.logger.Info("distribution node updated", "id", id, "name", node.Name)
	writeJSON(w, map[string]bool{"ok": true})
}

// handleDeleteDistributionNode deletes a distribution node
func (s *Server) handleDeleteDistributionNode(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	if err := s.st.DeleteDistributionNode(id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "distribution node not found", http.StatusNotFound)
			return
		}
		s.logger.Error("delete distribution node failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Reload distribution manager if enabled
	if s.distributionMgr != nil {
		cfg, err := s.st.GetDistributionConfig()
		if err == nil && cfg.Enabled {
			nodes := s.nodes.Nodes()
			if err := s.distributionMgr.Reload(r.Context(), nodes); err != nil {
				s.logger.Error("reload distribution manager failed", "error", err)
			}
		}
	}

	s.logger.Info("distribution node deleted", "id", id)
	writeJSON(w, map[string]bool{"ok": true})
}

// handleToggleDistributionNode enables or disables a distribution node
func (s *Server) handleToggleDistributionNode(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	node, err := s.st.GetDistributionNode(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "distribution node not found", http.StatusNotFound)
			return
		}
		s.logger.Error("get distribution node failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Toggle enabled state
	newEnabled := !node.Enabled
	if err := s.st.SetDistributionNodeEnabled(id, newEnabled); err != nil {
		s.logger.Error("toggle distribution node failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Reload distribution manager if enabled
	if s.distributionMgr != nil {
		cfg, err := s.st.GetDistributionConfig()
		if err == nil && cfg.Enabled {
			nodes := s.nodes.Nodes()
			if err := s.distributionMgr.Reload(r.Context(), nodes); err != nil {
				s.logger.Error("reload distribution manager failed", "error", err)
			}
		}
	}

	s.logger.Info("distribution node toggled", "id", id, "enabled", newEnabled)
	writeJSON(w, map[string]interface{}{"ok": true, "enabled": newEnabled})
}
