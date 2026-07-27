package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/taliove/proxyhub/internal/detection"
	"github.com/taliove/proxyhub/internal/subscription"
)

// 本文件是"出网+稳定性"检查(动作2)的 HTTP 入口:批量形态(任务化,作用于勾选集)
// 与单节点形态(行内 SSE)共用同一语义——exam 出网段画像 + 稳定性评分,不含解锁/测速。
// 结果带 source=stability_check 落 exam_history;"最近体检"消费方只认完整体检口径,不被抢占。

// handleBatchStability 启动批量"出网+稳定性"任务:node_keys 为空则对全部节点检查。
// 按请求者用户空间(多租户):"全部"= 本人池分片;任务按属主分片,互不干扰。
func (s *Server) handleBatchStability(w http.ResponseWriter, r *http.Request) {
	if s.detectionService == nil || s.batchStabilityJobs == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]string{"error": "detection service not initialized"})
		return
	}
	userScope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	effUID := EffectiveUserID(userScope)

	var req struct {
		NodeKeys []string `json:"node_keys"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}

	// node_keys 为空时对本人池全部节点检查
	pool := s.nodes.NodesForUser(effUID)
	nodeKeys := req.NodeKeys
	scope := "selected"
	if len(nodeKeys) == 0 {
		scope = "all"
		for _, n := range pool {
			nodeKeys = append(nodeKeys, n.NodeKey())
		}
	}

	// 收集活节点(含凭证,限本人池)
	var nodes []*subscription.Node
	for _, nk := range nodeKeys {
		for _, n := range pool {
			if n.NodeKey() == nk {
				nodes = append(nodes, n)
				break
			}
		}
	}

	key, err := s.batchStabilityJobs.StartFor(effUID, nodeKeys, nodes, scope)
	if err != nil {
		s.logger.Error("start batch stability failed", "error", err)
		writeJSONStatus(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, map[string]string{"status": "started", "key": key})
}

// handleBatchStabilityStream 订阅批量"出网+稳定性"任务事件流(SSE):回放 + 直播。
func (s *Server) handleBatchStabilityStream(w http.ResponseWriter, r *http.Request) {
	if s.detectionService == nil || s.batchStabilityJobs == nil {
		http.Error(w, "detection service not initialized", http.StatusServiceUnavailable)
		return
	}

	userScope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	// 任务 key 固定为 "batch_stability"(按属主分片单例)
	key := "batch_stability"

	sub, err := s.batchStabilityJobs.SubscribeFor(EffectiveUserID(userScope), key)
	if err != nil {
		http.Error(w, "no active batch stability check", http.StatusNotFound)
		return
	}
	defer sub.Close()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	writeFrame := func(data []byte) {
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	for _, ev := range sub.Replay {
		writeFrame(ev.Data)
	}

	for {
		select {
		case <-r.Context().Done():
			return
		case ev, ok := <-sub.Live:
			if !ok {
				return
			}
			writeFrame(ev.Data)
		}
	}
}

// handleBatchStabilityCancel 取消批量"出网+稳定性"任务。
func (s *Server) handleBatchStabilityCancel(w http.ResponseWriter, r *http.Request) {
	if s.detectionService == nil || s.batchStabilityJobs == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]string{"error": "detection service not initialized"})
		return
	}

	userScope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	key := "batch_stability"
	if !s.batchStabilityJobs.CancelFor(EffectiveUserID(userScope), key) {
		writeJSONStatus(w, http.StatusConflict, map[string]string{"error": "no active batch stability check"})
		return
	}

	writeJSON(w, map[string]string{"status": "cancelled"})
}

// handleNodeStabilityStream 单节点"出网+稳定性"检查流式端点(SSE):任务化模型,
// 与单节点深度体检同构——无进行中任务则启动、有则附加;附加先回放再转直播;
// 连接断开任务仍在后台跑,可重连续传。落历史(带来源标记)由任务生命周期负责。
func (s *Server) handleNodeStabilityStream(w http.ResponseWriter, r *http.Request) {
	if s.detectionService == nil || s.stabilityExamJobs == nil {
		http.Error(w, "detection service not initialized", http.StatusServiceUnavailable)
		return
	}

	q := r.URL.Query()
	nodeKey := q.Get("node_key")
	var selfNodeID int64
	if v := q.Get("self_node_id"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			http.Error(w, "invalid self_node_id", http.StatusBadRequest)
			return
		}
		selfNodeID = id
	}

	userScope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	effUID := EffectiveUserID(userScope)
	node := s.resolveTestNode(effUID, selfNodeID, nodeKey)
	if node == nil {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	writeFrame := func(f detection.ExamFrame) {
		b, err := json.Marshal(f)
		if err != nil {
			s.logger.Warn("marshal stability exam frame failed", "error", err)
			return
		}
		fmt.Fprintf(w, "data: %s\n\n", b)
		flusher.Flush()
	}

	// force=1:"重新检查"语义,已收口的旧任务丢弃重开(进行中的任务不受影响,仍附加)。
	// 任务按属主分片(多租户):同节点不同用户各自单实例。
	var sub *detection.ExamSubscription
	if q.Get("force") == "1" {
		sub = s.stabilityExamJobs.OpenForceFor(effUID, node.NodeKey(), node)
	} else {
		sub = s.stabilityExamJobs.OpenFor(effUID, node.NodeKey(), node)
	}
	defer sub.Close()

	for _, f := range sub.Replay {
		writeFrame(f)
	}

	for {
		select {
		case <-r.Context().Done():
			return
		case f, ok := <-sub.Live:
			if !ok {
				return
			}
			writeFrame(f)
		}
	}
}

// handleNodeStabilityCancel 取消某节点的进行中"出网+稳定性"检查:不落历史。无进行中任务返回 409。
func (s *Server) handleNodeStabilityCancel(w http.ResponseWriter, r *http.Request) {
	if s.detectionService == nil || s.stabilityExamJobs == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]string{"error": "detection service not initialized"})
		return
	}

	userScope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	effUID := EffectiveUserID(userScope)
	nodeKey := s.resolveExamNodeKey(effUID, r)
	if nodeKey == "" {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "missing node_key or self_node_id"})
		return
	}

	if !s.stabilityExamJobs.CancelFor(effUID, nodeKey) {
		writeJSONStatus(w, http.StatusConflict, map[string]string{"error": "no active stability check"})
		return
	}

	writeJSON(w, map[string]string{"status": "cancelled"})
}
