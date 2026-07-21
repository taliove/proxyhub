package server

import (
	"net/http"

	"github.com/taliove/proxyhub/internal/generator"
	"github.com/taliove/proxyhub/internal/subscription"
)

// handleNodeShareURI generates a share URI for a given node by node_key
// Used for QR code generation in the frontend
func (s *Server) handleNodeShareURI(w http.ResponseWriter, r *http.Request) {
	nodeKey := r.PathValue("nodeKey")
	if nodeKey == "" {
		http.Error(w, "node_key is required", http.StatusBadRequest)
		return
	}

	// Find node in current pool
	var targetNode *subscription.Node
	for _, n := range s.nodes.Nodes() {
		if n.NodeKey() == nodeKey {
			targetNode = n
			break
		}
	}

	if targetNode == nil {
		http.Error(w, "node not found", http.StatusNotFound)
		return
	}

	// Generate share URI using generator package
	uri, err := generator.GenerateShareURI(targetNode)
	if err != nil {
		s.logger.Error("generate share URI failed", "node_key", nodeKey, "error", err)
		http.Error(w, "failed to generate share URI", http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]string{"uri": uri})
}
