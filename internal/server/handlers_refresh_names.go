package server

import (
	"encoding/json"
	"net/http"

	"github.com/taliove/proxyhub/internal/geoip"
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
// 按请求者用户空间操作(多租户):只处理本人池与本人自建节点表。
func (s *Server) handleRefreshNames(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	effUID := EffectiveUserID(scope)

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
	airportUpdated := s.refreshAirportNodeNames(effUID, targetKeys)

	// Refresh self-hosted nodes
	selfUpdated := s.refreshSelfHostedNodeNames(effUID, targetKeys)

	total := airportUpdated + selfUpdated
	writeJSON(w, map[string]int{
		"updated": total,
		"total":   total,
	})
}

// refreshAirportNodeNames re-applies standardization to airport nodes in the
// caller's own pool shard (multi-tenant). Returns count of nodes updated.
func (s *Server) refreshAirportNodeNames(userID int64, targetKeys map[string]bool) int {
	if s.nodes == nil {
		return 0
	}

	// Collect airport nodes to refresh
	var toRefresh []*subscription.Node
	for _, n := range s.nodes.NodesForUser(userID) {
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

// refreshSelfHostedNodeNames re-runs region resolution and applies region-based naming
// for the caller's own self-hosted nodes (multi-tenant).
// Region resolution priority: latest exam egress > offline GeoIP > preserve existing.
// For nodes with known region: always renames to "自建{region}" format, overwriting custom names.
// For nodes with Unknown/empty region: preserves existing name.
// Returns count of nodes updated.
func (s *Server) refreshSelfHostedNodeNames(userID int64, targetKeys map[string]bool) int {
	nodes, err := s.st.ListAllSelfHostedNodesByUser(userID)
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

		// Re-run region resolution with priority: egress > GeoIP > preserve
		oldRegion := n.RegionCode
		oldName := n.Name
		n.RegionCode = s.resolveNodeRegion(nodeKey, n.RegionCode, n)

		// Apply region-based naming: if region is known, always rename to standard format
		if n.RegionCode != "" && n.RegionCode != "Unknown" {
			cn := geoip.CountryName(n.RegionCode)
			if cn != "" {
				n.Name = selfNodeNamePrefix + cn
			}
			// If CountryName fails despite having region code, keep old name
		}
		// If region is Unknown or empty, preserve existing name (no change to n.Name)

		// Only update DB if something changed
		if n.RegionCode != oldRegion || n.Name != oldName {
			if err := s.st.UpdateSelfHostedNodeForUser(userID, n); err != nil {
				s.logger.Warn("update self node after refresh failed", "nodeKey", nodeKey, "error", err)
				continue
			}
			// Sync memory pool so /nodes reflects the new name/region immediately
			// (ticket 47): no waiting for the next aggregation refresh.
			s.syncSelfHostedNodeIdentityForUser(userID, nodeKey, n.Name, n.RegionCode)
			updated++
		}
	}

	return updated
}
