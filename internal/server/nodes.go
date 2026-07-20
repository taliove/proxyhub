package server

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

// handleListSelfNodes 列出全部自建节点（含已禁用），供后台管理页
func (s *Server) handleListSelfNodes(w http.ResponseWriter, r *http.Request) {
	nodes, err := s.st.ListAllSelfHostedNodes()
	if err != nil {
		s.logger.Error("list self nodes failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"nodes": nodes})
}

// decodeSelfNode 解析并校验自建节点请求体（协议 + 必填字段）
func decodeSelfNode(r *http.Request) (*store.SelfHostedNode, error) {
	var n store.SelfHostedNode
	if err := json.NewDecoder(r.Body).Decode(&n); err != nil {
		return nil, errors.New("invalid request")
	}
	n.Name = strings.TrimSpace(n.Name)
	n.Server = strings.TrimSpace(n.Server)
	if n.Name == "" || n.Server == "" {
		return nil, errors.New("name and server are required")
	}
	if n.Port <= 0 || n.Port > 65535 {
		return nil, errors.New("invalid port")
	}
	switch n.Protocol {
	case "ss", "trojan", "vmess", "vless":
	default:
		return nil, errors.New("unsupported protocol")
	}
	return &n, nil
}

// handleCreateSelfNode 新增自建节点（默认启用）
func (s *Server) handleCreateSelfNode(w http.ResponseWriter, r *http.Request) {
	n, err := decodeSelfNode(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	n.Enabled = true
	rctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	n.RegionCode = s.resolveSelfNodeRegion(rctx, n)
	if err := s.st.CreateSelfHostedNode(n); err != nil {
		s.logger.Error("create self node failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]bool{"success": true})
}

// handleUpdateSelfNode 编辑自建节点
func (s *Server) handleUpdateSelfNode(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	n, err := decodeSelfNode(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	n.ID = id
	rctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	n.RegionCode = s.resolveSelfNodeRegion(rctx, n)
	if err := s.st.UpdateSelfHostedNode(n); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		s.logger.Error("update self node failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]bool{"success": true})
}

// handleDeleteSelfNode 删除自建节点
func (s *Server) handleDeleteSelfNode(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	if err := s.st.DeleteSelfHostedNode(id); err != nil {
		s.logger.Error("delete self node failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]bool{"success": true})
}

// handleToggleSelfNode 启用/禁用自建节点
func (s *Server) handleToggleSelfNode(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if err := s.st.SetSelfHostedNodeEnabled(id, req.Enabled); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		s.logger.Error("toggle self node failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]bool{"success": true})
}

// handleBlockNode 屏蔽机场节点（按 NodeKey）
func (s *Server) handleBlockNode(w http.ResponseWriter, r *http.Request) {
	key, err := decodeNodeKey(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.st.BlockNode(key); err != nil {
		s.logger.Error("block node failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]bool{"success": true})
}

// handleUnblockNode 取消屏蔽机场节点
func (s *Server) handleUnblockNode(w http.ResponseWriter, r *http.Request) {
	key, err := decodeNodeKey(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.st.UnblockNode(key); err != nil {
		s.logger.Error("unblock node failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]bool{"success": true})
}

// batchBlockRequest 批量屏蔽/取消屏蔽请求体。
// 二选一：显式给出 node_keys，或给出 source（机场名，按当前节点池展开为该机场全部 NodeKey）。
type batchBlockRequest struct {
	NodeKeys []string `json:"node_keys"`
	Source   string   `json:"source"`
}

// resolveBatchKeys 从请求解析出目标 NodeKey 列表。
// 优先用显式 node_keys；否则按 source 从当前节点池收集该机场（自建节点豁免）的全部 NodeKey。
func (s *Server) resolveBatchKeys(req batchBlockRequest) []string {
	if len(req.NodeKeys) > 0 {
		return req.NodeKeys
	}
	source := strings.TrimSpace(req.Source)
	if source == "" {
		return nil
	}
	seen := make(map[string]bool)
	var keys []string
	for _, n := range s.nodes.Nodes() {
		if n.Source != source || n.Source == subscription.SourceSelfHosted {
			continue
		}
		key := n.NodeKey()
		if !seen[key] {
			seen[key] = true
			keys = append(keys, key)
		}
	}
	return keys
}

// handleBatchBlockNodes 批量屏蔽：按机场（source）或显式 node_keys 一次性拉黑，跨刷新持久。
func (s *Server) handleBatchBlockNodes(w http.ResponseWriter, r *http.Request) {
	var req batchBlockRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	keys := s.resolveBatchKeys(req)
	if len(keys) == 0 {
		http.Error(w, "no matching nodes: provide node_keys or a valid source", http.StatusBadRequest)
		return
	}
	if err := s.st.BlockNodes(keys); err != nil {
		s.logger.Error("batch block nodes failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"success": true, "count": len(keys)})
}

// handleBatchUnblockNodes 批量取消屏蔽：按机场（source）或显式 node_keys。
func (s *Server) handleBatchUnblockNodes(w http.ResponseWriter, r *http.Request) {
	var req batchBlockRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	keys := s.resolveBatchKeys(req)
	if len(keys) == 0 {
		http.Error(w, "no matching nodes: provide node_keys or a valid source", http.StatusBadRequest)
		return
	}
	if err := s.st.UnblockNodes(keys); err != nil {
		s.logger.Error("batch unblock nodes failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"success": true, "count": len(keys)})
}

// decodeNodeKey 从请求体解析 node_key（server:port）
func decodeNodeKey(r *http.Request) (string, error) {
	var req struct {
		NodeKey string `json:"node_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return "", errors.New("invalid request")
	}
	key := strings.TrimSpace(req.NodeKey)
	if key == "" {
		return "", errors.New("node_key is required")
	}
	return key, nil
}

// resolveSelfNodeRegion 用真实依赖解析自建节点地区码(带超时,失败不阻断保存)。
func (s *Server) resolveSelfNodeRegion(ctx context.Context, n *store.SelfHostedNode) string {
	recognizer, err := s.st.NewRegionRecognizer()
	if err != nil {
		s.logger.Warn("build region recognizer failed", "error", err)
		return "Unknown"
	}
	deps := regionResolverDeps{
		lookupHost: net.LookupHost,
		geoLookup: func(ctx context.Context, ip string) (string, error) {
			geo, err := s.geo.LookupIP(ctx, ip)
			if err != nil {
				return "", err
			}
			return geo.Country, nil
		},
		recognize: recognizer.Recognize,
	}
	return resolveRegionCode(ctx, n.Server, n.Name, deps)
}

