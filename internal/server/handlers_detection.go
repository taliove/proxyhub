package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/taliove/proxyhub/internal/detection"
	"github.com/taliove/proxyhub/internal/subscription"
)

// handleTriggerDetection 启动检测任务
func (s *Server) handleTriggerDetection(w http.ResponseWriter, r *http.Request) {
	if s.detectionService == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "detection service not initialized",
		})
		return
	}

	var req DetectionScope
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid request"})
		return
	}

	if err := s.detectionService.TriggerDetection(r.Context(), req); err != nil {
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "started"})
}

// handleCancelDetection 取消当前检测
func (s *Server) handleCancelDetection(w http.ResponseWriter, r *http.Request) {
	if s.detectionService == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "detection service not initialized",
		})
		return
	}

	if err := s.detectionService.CancelDetection(); err != nil {
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "cancelled"})
}

// handleDetectionStatus 查询检测进度
func (s *Server) handleDetectionStatus(w http.ResponseWriter, r *http.Request) {
	if s.detectionService == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "detection service not initialized",
		})
		return
	}

	status := s.detectionService.GetStatus()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

// handleGetDetectionTargets 获取检测目标配置
func (s *Server) handleGetDetectionTargets(w http.ResponseWriter, r *http.Request) {
	targets, err := s.st.GetDetectionTargets()
	if err != nil {
		s.logger.Error("get detection targets failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(targets)
}

// handleSaveDetectionTargets 保存检测目标配置
func (s *Server) handleSaveDetectionTargets(w http.ResponseWriter, r *http.Request) {
	var targets []detection.Target

	if err := json.NewDecoder(r.Body).Decode(&targets); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid request"})
		return
	}

	if err := s.st.SetDetectionTargets(targets); err != nil {
		s.logger.Error("save detection targets failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleTestNode 单节点即时测试:自建按 self_node_id 查库,机场按 node_key 查池。
func (s *Server) handleTestNode(w http.ResponseWriter, r *http.Request) {
	if s.detectionService == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		writeJSON(w, map[string]any{"error": "detection service not initialized"})
		return
	}
	var req struct {
		SelfNodeID int64  `json:"self_node_id"`
		NodeKey    string `json:"node_key"`
		Mode       string `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	// 支持三档：quick/real/bandwidth
	if req.Mode != "quick" && req.Mode != "real" && req.Mode != "bandwidth" {
		req.Mode = "quick"
	}

	node := s.resolveTestNode(req.SelfNodeID, req.NodeKey)
	if node == nil {
		http.NotFound(w, r)
		return
	}

	// bandwidth 模式超时更长
	timeout := 20 * time.Second
	if req.Mode == "bandwidth" {
		timeout = 90 * time.Second
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	result := s.detectionService.TestNode(ctx, node, req.Mode)

	// 持久化到 node_health（统一测试路径）
	if err := s.st.SaveTestResult(node.NodeKey(), node.Name, node.Source, result); err != nil {
		s.logger.Warn("save test result failed", "error", err)
		// 不阻断响应，测试结果仍返回
	}

	// 写回内存池，前端列表无需等下次读库即可见到最新结果（自建节点未入池时返回 false，忽略）
	s.nodes.UpdateNodeTestResult(node.NodeKey(), req.Mode, result.Available, result.Latency, result.DownMbps, result.UpMbps)

	writeJSON(w, result)
}

// resolveTestNode 节点解析公共逻辑:自建查库 ToNode,机场查内存池(返回 nil 表示未找到或参数错误)。
func (s *Server) resolveTestNode(selfNodeID int64, nodeKey string) *subscription.Node {
	switch {
	case selfNodeID > 0:
		all, err := s.st.ListAllSelfHostedNodes()
		if err != nil {
			return nil
		}
		for _, n := range all {
			if n.ID == selfNodeID {
				return n.ToNode()
			}
		}
	case nodeKey != "":
		for _, n := range s.nodes.Nodes() {
			if n.NodeKey() == nodeKey {
				return n
			}
		}
	}
	return nil
}

// handleTestNodeStream 单节点带宽测试流式端点(SSE):实时推送采样点,完成后推 done。
// 只用于 bandwidth 模式(EventSource 只能 GET+query)。
func (s *Server) handleTestNodeStream(w http.ResponseWriter, r *http.Request) {
	if s.detectionService == nil {
		http.Error(w, "detection service not initialized", http.StatusServiceUnavailable)
		return
	}

	// query 解析
	q := r.URL.Query()
	selfNodeIDStr := q.Get("self_node_id")
	nodeKey := q.Get("node_key")

	var selfNodeID int64
	if selfNodeIDStr != "" {
		var err error
		selfNodeID, err = strconv.ParseInt(selfNodeIDStr, 10, 64)
		if err != nil {
			http.Error(w, "invalid self_node_id", http.StatusBadRequest)
			return
		}
	}

	node := s.resolveTestNode(selfNodeID, nodeKey)
	if node == nil {
		http.NotFound(w, r)
		return
	}

	// SSE 响应头
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	// emit SSE 帧(marshal + "data: ...\n\n" + Flush)
	emit := func(phase string, data any) {
		b, _ := json.Marshal(data)
		fmt.Fprintf(w, "data: %s\n\n", b)
		flusher.Flush()
	}

	// 流式测速,采样帧即时推送
	ctx := r.Context()
	result := s.detectionService.TestBandwidthStream(ctx, node, func(sample detection.Sample) {
		emit("sample", sample)
	})

	// 持久化(SaveTestResult + UpdateNodeTestResult,与现有 handleTestNode 一致)
	if err := s.st.SaveTestResult(node.NodeKey(), node.Name, node.Source, result); err != nil {
		s.logger.Warn("save test result failed", "error", err)
	}
	s.nodes.UpdateNodeTestResult(node.NodeKey(), "bandwidth", result.Available, result.Latency, result.DownMbps, result.UpMbps)

	// done 帧(包含最终 TestResult 全字段)
	emit("done", map[string]any{
		"phase":         "done",
		"available":     result.Available,
		"down_mbps":     result.DownMbps,
		"up_mbps":       result.UpMbps,
		"elapsed_ms":    result.ElapsedMs,
		"min_down_mbps": result.MinDownMbps,
		"min_up_mbps":   result.MinUpMbps,
		"error":         result.Error,
	})
}

// handleNodeExamStream 单节点深度体检流式端点(SSE):各段串行,实时推送 sample/section_done/done。
// 与 handleTestNodeStream 同风格(EventSource 只能 GET+query)。本段只跑稳定性,后续段预留。
func (s *Server) handleNodeExamStream(w http.ResponseWriter, r *http.Request) {
	if s.detectionService == nil {
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

	node := s.resolveTestNode(selfNodeID, nodeKey)
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

	emit := func(evt detection.ExamEvent) {
		b, _ := json.Marshal(evt)
		fmt.Fprintf(w, "data: %s\n\n", b)
		flusher.Flush()
	}

	s.detectionService.ExamStream(r.Context(), node, emit)
}
