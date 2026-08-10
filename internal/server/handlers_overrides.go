package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/taliove/proxyhub/internal/aggregator"
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

// handleSetNodeOverride 设置机场节点覆盖层(region)。
// 按请求者属主写(多租户):同一节点可被不同用户独立覆盖,互不串扰。
// display_name 自 ADR 0047(issue #96)起拒收:命名统一走名称槽位(/api/slots),
// 避免"两套命名来源"并存。注意:旧客户端发空串是静默 no-op(不再清空任何值);
// 迁移落选行的 display_name 残留只能经槽位认领或 DELETE 整行清理。
func (s *Server) handleSetNodeOverride(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
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
	if req.DisplayName != "" {
		http.Error(w, "display_name moved to name slots, use /api/slots", http.StatusBadRequest)
		return
	}

	if err := s.st.SetNodeRegionOverrideForUser(EffectiveUserID(scope), req.NodeKey, req.Region); err != nil {
		s.logger.Error("set node override failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]any{"success": true})
}

// handleClearNodeOverride 清除机场节点覆盖层(只清请求者自己名下的覆盖行)。
func (s *Server) handleClearNodeOverride(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	nodeKey, err := decodeNodeKeyRequest(r)
	if err != nil {
		http.Error(w, "invalid request or missing node_key", http.StatusBadRequest)
		return
	}

	if err := s.st.ClearNodeOverrideForUser(EffectiveUserID(scope), nodeKey); err != nil {
		s.logger.Error("clear node override failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]any{"success": true})
}

// handleCleanupNodes 批量清理失败节点（机场→屏蔽，自建→禁用/删除）。
// 按请求者用户空间操作(多租户):类型判定用本人池,自建节点只查本人表,
// 屏蔽写本人名单,禁用/删除带属主校验。
func (s *Server) handleCleanupNodes(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	effUID := EffectiveUserID(scope)

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

	allNodes := s.nodes.NodesForUser(effUID)
	selfNodes, _ := s.st.ListAllSelfHostedNodesByUser(effUID)

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

	// 机场节点：屏蔽(写本人名单)
	for _, key := range airportKeys {
		if err := s.st.BlockNodeForUser(effUID, key); err != nil {
			s.logger.Warn("block node failed", "key", key, "error", err)
		} else {
			blocked++
		}
	}

	// 自建节点：禁用或删除(带属主校验,行属他人不落)
	for _, id := range selfNodeIDs {
		if req.Action == "delete" {
			if err := s.st.DeleteSelfHostedNodeForUser(effUID, id); err != nil {
				s.logger.Warn("delete self node failed", "id", id, "error", err)
			} else {
				deleted++
			}
		} else { // disable
			if err := s.st.SetSelfHostedNodeEnabledForUser(effUID, id, false); err != nil {
				s.logger.Warn("disable self node failed", "id", id, "error", err)
			} else {
				disabled++
			}
		}
	}

	writeJSON(w, map[string]any{
		"success":  true,
		"blocked":  blocked,
		"disabled": disabled,
		"deleted":  deleted,
	})
}

// handlePurgeAirportNodes 一键清空机场节点(内存池+DB 双清,自建节点豁免,
// 屏蔽名单/名称覆盖保留;CONTEXT.md「机场节点清空」)。
// 并发语义(拒绝而非等待):有刷新任务进行中返回 409,由前端提示稍后重试——
// 否则进行中的刷新可能在清空后把旧节点写回池。
// 按请求者池分片清空(多租户):只清自己池,不动他人池。
func (s *Server) handlePurgeAirportNodes(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	removed, err := s.nodes.PurgeAirportNodesForUser(EffectiveUserID(scope))
	if errors.Is(err, aggregator.ErrPurgeConflict) {
		http.Error(w, "有刷新任务进行中,请稍后重试", http.StatusConflict)
		return
	}
	if err != nil {
		s.logger.Error("purge airport nodes failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]any{"success": true, "removed": removed})
}
