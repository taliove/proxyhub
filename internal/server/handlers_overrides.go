package server

import (
	"encoding/json"
	"net/http"

	"github.com/taliove/proxyhub/internal/subscription"
)

// decodeNodeKeyRequest 解析并验证包含 node_key 的 JSON 请求体
func decodeNodeKeyRequest(r *http.Request) (string, error) {
	var req struct {
		NodeKey string `json:"node_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return "", err
	}
	if req.NodeKey == "" {
		return "", http.ErrMissingBoundary // 重用标准错误表示缺失字段
	}
	return req.NodeKey, nil
}

// handleSetNodeOverride 设置机场节点覆盖层（display_name/region）
func (s *Server) handleSetNodeOverride(w http.ResponseWriter, r *http.Request) {
	var req struct {
		NodeKey     string `json:"node_key"`
		DisplayName string `json:"display_name"`
		Region      string `json:"region"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if req.NodeKey == "" {
		http.Error(w, "node_key required", http.StatusBadRequest)
		return
	}

	if err := s.st.SetNodeOverride(req.NodeKey, req.DisplayName, req.Region); err != nil {
		s.logger.Error("set node override failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]any{"success": true})
}

// handleClearNodeOverride 清除机场节点覆盖层
func (s *Server) handleClearNodeOverride(w http.ResponseWriter, r *http.Request) {
	nodeKey, err := decodeNodeKeyRequest(r)
	if err != nil {
		http.Error(w, "invalid request or missing node_key", http.StatusBadRequest)
		return
	}

	if err := s.st.ClearNodeOverride(nodeKey); err != nil {
		s.logger.Error("clear node override failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]any{"success": true})
}

// handleCleanupNodes 批量清理失败节点（机场→屏蔽，自建→禁用/删除）
func (s *Server) handleCleanupNodes(w http.ResponseWriter, r *http.Request) {
	var req struct {
		NodeKeys []string `json:"node_keys"`
		Action   string   `json:"action"` // "block" | "disable" | "delete"
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if len(req.NodeKeys) == 0 {
		http.Error(w, "node_keys required", http.StatusBadRequest)
		return
	}
	if req.Action != "block" && req.Action != "disable" && req.Action != "delete" {
		req.Action = "block" // 默认屏蔽
	}

	// 按 source 分类节点：自建节点走禁用/删除，机场节点走屏蔽。
	// 关键：自建节点也在内存池中（Source==self-hosted），必须按 source 判定，
	// 不能只看"是否在池中"——否则自建节点会被误判为机场节点走屏蔽。
	var airportKeys []string
	var selfNodeIDs []int64

	allNodes := s.nodes.Nodes()
	selfNodes, _ := s.st.ListAllSelfHostedNodes()

	// 建 NodeKey → source 索引，用于判定节点类型
	sourceByKey := make(map[string]string, len(allNodes))
	for _, n := range allNodes {
		sourceByKey[n.NodeKey()] = n.Source
	}

	for _, key := range req.NodeKeys {
		// 自建节点：按 NodeKey 反查 self_node id
		if sourceByKey[key] == subscription.SourceSelfHosted {
			for _, sn := range selfNodes {
				if sn.ToNode().NodeKey() == key {
					selfNodeIDs = append(selfNodeIDs, sn.ID)
					break
				}
			}
			continue
		}
		// 机场节点：屏蔽
		airportKeys = append(airportKeys, key)
	}

	var blocked, disabled, deleted int

	// 机场节点：屏蔽
	for _, key := range airportKeys {
		if err := s.st.BlockNode(key); err != nil {
			s.logger.Warn("block node failed", "key", key, "error", err)
		} else {
			blocked++
		}
	}

	// 自建节点：禁用或删除
	for _, id := range selfNodeIDs {
		if req.Action == "delete" {
			if err := s.st.DeleteSelfHostedNode(id); err != nil {
				s.logger.Warn("delete self node failed", "id", id, "error", err)
			} else {
				deleted++
			}
		} else { // disable
			if err := s.st.SetSelfHostedNodeEnabled(id, false); err != nil {
				s.logger.Warn("disable self node failed", "id", id, "error", err)
			} else {
				disabled++
			}
		}
	}

	writeJSON(w, map[string]any{
		"success": true,
		"blocked": blocked,
		"disabled": disabled,
		"deleted": deleted,
	})
}
