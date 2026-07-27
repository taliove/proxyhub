package server

import (
	"net/http"

	"github.com/taliove/proxyhub/internal/generator"
	"github.com/taliove/proxyhub/internal/subscription"
)

// handleNodeShareURI generates a share URI for a given node by node_key
// Used for QR code generation in the frontend
// 按请求者用户空间隔离(多租户):只在 EffectiveUserID 池分片内解析,
// 命中他人池一律 404——分享链接含凭证,跨租户泄露是红线。
func (s *Server) handleNodeShareURI(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	nodeKey := r.PathValue("nodeKey")
	if nodeKey == "" {
		http.Error(w, "node_key is required", http.StatusBadRequest)
		return
	}

	// Find node in the caller's own pool shard
	var targetNode *subscription.Node
	for _, n := range s.nodes.NodesForUser(EffectiveUserID(scope)) {
		if n.NodeKey() == nodeKey {
			targetNode = n
			break
		}
	}

	if targetNode == nil {
		http.Error(w, "node not found", http.StatusNotFound)
		return
	}

	// Prefer the original share URI preserved at subscription parse time (ticket 56):
	// it carries airport-specific params the generator may drop. Fall back to the
	// generator when absent (self-hosted nodes, or pool restored before this field existed).
	// RawLink 回放时规范化 fragment:机场原文可能用 + 编码空格,Shadowrocket 等客户端
	// 会原样显示 +(fragment 只做 percent-decode)。
	uri := targetNode.RawLink
	if uri != "" {
		uri = generator.NormalizeShareURIFragment(uri)
	} else {
		var err error
		uri, err = generator.GenerateShareURI(targetNode)
		if err != nil {
			s.logger.Error("generate share URI failed", "node_key", nodeKey, "error", err)
			http.Error(w, "failed to generate share URI", http.StatusInternalServerError)
			return
		}
	}

	writeJSON(w, map[string]string{"uri": uri})
}
