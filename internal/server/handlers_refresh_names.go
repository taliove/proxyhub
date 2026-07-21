package server

import (
	"encoding/json"
	"net/http"

	"github.com/taliove/proxyhub/internal/subscription"
)

// refreshNamesRequest defines the request body for name refresh endpoint
type refreshNamesRequest struct {
	NodeKeys []string `json:"node_keys"` // Optional: specific nodes to refresh; empty = all
}

// handleRefreshNames re-runs region recognition and name standardization for selected nodes.
// For airport nodes: re-applies standardization using current region (which may have been updated by health check).
// For self-hosted nodes: re-runs GeoIP resolution and name fallback.
// Returns {updated, total} count.
func (s *Server) handleRefreshNames(w http.ResponseWriter, r *http.Request) {
	var req refreshNamesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	// Build target set (nil = all nodes)
	var targetKeys map[string]bool
	if len(req.NodeKeys) > 0 {
		targetKeys = make(map[string]bool, len(req.NodeKeys))
		for _, key := range req.NodeKeys {
			targetKeys[key] = true
		}
	}

	// Refresh airport nodes
	airportUpdated := s.refreshAirportNodeNames(targetKeys)

	// Refresh self-hosted nodes
	selfUpdated := s.refreshSelfHostedNodeNames(targetKeys)

	total := airportUpdated + selfUpdated
	writeJSON(w, map[string]int{
		"updated": total,
		"total":   total,
	})
}

// refreshAirportNodeNames re-applies standardization to airport nodes in the memory pool.
// Returns count of nodes updated.
func (s *Server) refreshAirportNodeNames(targetKeys map[string]bool) int {
	if s.nodes == nil {
		return 0
	}

	// Collect airport nodes to refresh
	var toRefresh []*subscription.Node
	for _, n := range s.nodes.Nodes() {
		if n.Source == subscription.SourceSelfHosted {
			continue // Skip self-hosted nodes
		}
		if targetKeys != nil && !targetKeys[n.NodeKey()] {
			continue // Not in target set
		}
		toRefresh = append(toRefresh, n)
	}

	if len(toRefresh) == 0 {
		return 0
	}

	// Re-apply standardization using the same logic as subscription generation
	standardized := s.applyStandardization(toRefresh, true, subscription.DefaultNameTemplate)

	// Update DisplayName in the node pool (mutation is safe here as we own the pool)
	for i, original := range toRefresh {
		if i < len(standardized) {
			original.DisplayName = standardized[i].DisplayName
		}
	}

	return len(toRefresh)
}

// refreshSelfHostedNodeNames re-runs GeoIP resolution and name fallback for self-hosted nodes.
// Returns count of nodes updated.
func (s *Server) refreshSelfHostedNodeNames(targetKeys map[string]bool) int {
	nodes, err := s.st.ListAllSelfHostedNodes()
	if err != nil {
		s.logger.Warn("list self nodes for refresh failed", "error", err)
		return 0
	}

	updated := 0
	for _, n := range nodes {
		nodeKey := n.ToNode().NodeKey()
		if targetKeys != nil && !targetKeys[nodeKey] {
			continue
		}

		// Re-run region resolution
		oldRegion := n.RegionCode
		n.RegionCode = s.resolveSelfNodeRegion(n)

		// Re-run name fallback (will only set name if currently empty or region changed)
		// Save original name to detect changes
		oldName := n.Name
		n.Name = "" // Temporarily clear to trigger fallback
		if err := applySelfNodeNameFallback(n); err != nil {
			// If fallback fails, restore original and skip
			n.Name = oldName
			n.RegionCode = oldRegion
			continue
		}

		// Only update DB if something changed
		if n.RegionCode != oldRegion || n.Name != oldName {
			if err := s.st.UpdateSelfHostedNode(n); err != nil {
				s.logger.Warn("update self node after refresh failed", "nodeKey", nodeKey, "error", err)
				continue
			}
			updated++
		}
	}

	return updated
}
