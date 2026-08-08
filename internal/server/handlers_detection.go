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

// handleTriggerDetection 启动检测任务(jobs 运行时)
func (s *Server) handleTriggerDetection(w http.ResponseWriter, r *http.Request) {
	if s.detectionJobs == nil {
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

	if err := s.detectionJobs.TriggerDetection(r.Context(), req); err != nil {
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "started"})
}

// handleCancelDetection 取消当前检测
func (s *Server) handleCancelDetection(w http.ResponseWriter, r *http.Request) {
	if s.detectionJobs == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "detection service not initialized",
		})
		return
	}

	if err := s.detectionJobs.CancelDetection(); err != nil {
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "cancelled"})
}

// handleDetectionStatus 查询检测进度
func (s *Server) handleDetectionStatus(w http.ResponseWriter, r *http.Request) {
	if s.detectionJobs == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "detection service not initialized",
		})
		return
	}

	status := s.detectionJobs.GetStatus()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

// handleGetDetectionTargets 获取检测目标配置(视角驱动):
// 超管未 impersonate = 全局配置;普通用户/impersonate = 本人覆盖(回退全局默认)。
func (s *Server) handleGetDetectionTargets(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	targets, err := s.st.GetDetectionTargetsForUser(viewScopeUserID(scope))
	if err != nil {
		s.logger.Error("get detection targets failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(targets)
}

// handleSaveDetectionTargets 保存检测目标配置(视角驱动):
// 超管未 impersonate = 写全局;普通用户/impersonate = 写本人覆盖(user_settings)。
func (s *Server) handleSaveDetectionTargets(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	uid := viewScopeUserID(scope)
	var targets []detection.Target

	if err := json.NewDecoder(r.Body).Decode(&targets); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid request"})
		return
	}

	var err error
	if uid == 0 {
		err = s.st.SetDetectionTargets(targets)
	} else {
		err = s.st.SetDetectionTargetsForUser(uid, targets)
	}
	if err != nil {
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
	// 支持四档：quick/real/bandwidth/speedtest(快速测速:基准下行 + 保留上行)
	if req.Mode != "quick" && req.Mode != "real" && req.Mode != "bandwidth" && req.Mode != "speedtest" {
		req.Mode = "quick"
	}

	// 未给目标是参数错误(400);给了但解析不到才是资源不存在(404)
	if req.SelfNodeID <= 0 && req.NodeKey == "" {
		http.Error(w, "missing target: self_node_id or node_key required", http.StatusBadRequest)
		return
	}
	scope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	effUID := EffectiveUserID(scope)
	node := s.resolveTestNode(effUID, req.SelfNodeID, req.NodeKey)
	if node == nil {
		http.NotFound(w, r)
		return
	}

	// bandwidth/speedtest 模式超时更长
	timeout := 20 * time.Second
	if req.Mode == "bandwidth" || req.Mode == "speedtest" {
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

	// 写回内存池，前端列表无需等下次读库即可见到最新结果（自建节点未入池时返回 false，忽略）。
	// 用 result.Mode 而非 req.Mode:speedtest 档结果 Mode=bandwidth,写回走带宽字段分支。
	s.nodes.UpdateNodeTestResultForUser(effUID, node.NodeKey(), result.Mode, result.Available, result.Latency, result.DownMbps, result.UpMbps, result.FailReason, result.Error)

	writeJSON(w, result)
}

// resolveTestNode 节点解析公共逻辑:自建查库 ToNode,机场查内存池(返回 nil 表示未找到或参数错误)。
// 按属主限定用户空间(多租户):自建查本人表,机场查本人池分片;命中他人资源一律 nil(404)。
// nodeKey 分支查 serve-time 合并后的池(mergeSelfHosted):自建节点不入原始池,
// 只经合并出现——测速页等只按 node_key 寻址的调用方(不带 self_node_id)对自建
// 节点会 404(生产实测:proxy-latency 对自建节点 404)。
func (s *Server) resolveTestNode(userID, selfNodeID int64, nodeKey string) *subscription.Node {
	switch {
	case selfNodeID > 0:
		all, err := s.st.ListAllSelfHostedNodesByUser(userID)
		if err != nil {
			return nil
		}
		for _, n := range all {
			if n.ID == selfNodeID {
				return n.ToNode()
			}
		}
	case nodeKey != "":
		for _, n := range s.mergeSelfHosted(s.nodes.NodesForUser(userID), userID) {
			if n.NodeKey() == nodeKey {
				return n
			}
		}
	}
	return nil
}

// handleTestNodeStream 单节点带宽测试流式端点(SSE):实时推送采样点,完成后推 done。
// 只用于带宽类模式(EventSource 只能 GET+query)。
// mode=speedtest 时走快速测速档(基准端点 Cloudflare __down/__up,采样帧契约不变);
// 缺省保持 legacy bandwidth 口径(双档并存,现有 UX 零破坏)。
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

	scope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	effUID := EffectiveUserID(scope)
	node := s.resolveTestNode(effUID, selfNodeID, nodeKey)
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

	// 流式测速,采样帧即时推送(双档:mode=speedtest 走基准端点,缺省 legacy)
	ctx := r.Context()
	var result detection.TestResult
	if q.Get("mode") == "speedtest" {
		result = s.detectionService.TestSpeedtestStream(ctx, node, func(sample detection.Sample) {
			emit("sample", sample)
		})
	} else {
		result = s.detectionService.TestBandwidthStream(ctx, node, func(sample detection.Sample) {
			emit("sample", sample)
		})
	}

	// 持久化(SaveTestResult + UpdateNodeTestResult,与现有 handleTestNode 一致)
	if err := s.st.SaveTestResult(node.NodeKey(), node.Name, node.Source, result); err != nil {
		s.logger.Warn("save test result failed", "error", err)
	}
	s.nodes.UpdateNodeTestResultForUser(effUID, node.NodeKey(), "bandwidth", result.Available, result.Latency, result.DownMbps, result.UpMbps, "", "")

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

// handleNodeExamStream 单节点深度体检流式端点(SSE):任务化模型。
// 该节点无进行中任务则启动、有则附加;附加先回放缓冲事件(带序号)再转直播。
// 连接断开(r.Context() 取消)只结束本次 SSE,任务仍在后台跑,可重连续传。
// 落历史由任务生命周期负责(见 ExamJobManager),此处不再落盘。
func (s *Server) handleNodeExamStream(w http.ResponseWriter, r *http.Request) {
	if s.detectionService == nil || s.examJobs == nil {
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

	scope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	effUID := EffectiveUserID(scope)
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
			s.logger.Warn("marshal exam frame failed", "error", err)
			return
		}
		fmt.Fprintf(w, "data: %s\n\n", b)
		flusher.Flush()
	}

	// force=1:"重新体检"语义,已收口的旧任务丢弃重开(进行中的任务不受影响,仍附加)。
	// 任务按属主分片(多租户):同节点不同用户各自单实例。
	var sub *detection.ExamSubscription
	if q.Get("force") == "1" {
		sub = s.examJobs.OpenForceFor(effUID, node.NodeKey(), node)
	} else {
		sub = s.examJobs.OpenFor(effUID, node.NodeKey(), node)
	}
	defer sub.Close()

	// 先回放缓冲事件(附加语义),再转直播。
	for _, f := range sub.Replay {
		writeFrame(f)
	}

	for {
		select {
		case <-r.Context().Done():
			// 连接断开:结束本次 SSE,任务继续在后台运行。
			return
		case f, ok := <-sub.Live:
			if !ok {
				// 任务收口,通道关闭。
				return
			}
			writeFrame(f)
		}
	}
}

// handleNodeExamCancel 取消某节点的进行中体检任务:取消任务 ctx、推 cancelled 事件、不落历史。
// 无进行中任务返回 409。
func (s *Server) handleNodeExamCancel(w http.ResponseWriter, r *http.Request) {
	if s.detectionService == nil || s.examJobs == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]string{"error": "detection service not initialized"})
		return
	}

	scope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	effUID := EffectiveUserID(scope)
	nodeKey := s.resolveExamNodeKey(effUID, r)
	if nodeKey == "" {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "missing node_key or self_node_id"})
		return
	}

	if !s.examJobs.CancelFor(effUID, nodeKey) {
		writeJSONStatus(w, http.StatusConflict, map[string]string{"error": "no active exam"})
		return
	}

	writeJSON(w, map[string]string{"status": "cancelled"})
}

// resolveExamNodeKey 从 query 解析体检历史的 node_key:显式 node_key 优先,
// 否则用 self_node_id 解析出对应节点的 NodeKey(节点已不在池中则返回空)。
// 显式 node_key 原样返回:读侧已按属主过滤历史(多租户),key 本身不构成泄露;
// self_node_id 路径按属主限定(他人自建节点解析不到)。
func (s *Server) resolveExamNodeKey(userID int64, r *http.Request) string {
	q := r.URL.Query()
	if nk := q.Get("node_key"); nk != "" {
		return nk
	}
	if v := q.Get("self_node_id"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			if node := s.resolveTestNode(userID, id, ""); node != nil {
				return node.NodeKey()
			}
		}
	}
	return ""
}

// handleGetExamLatest 查询某节点最近一次深度体检报告(完整体检口径:排除"出网+稳定性"
// 任务的缺段报告)。无历史返回 JSON null(200),不报错。
func (s *Server) handleGetExamLatest(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	nodeKey := s.resolveExamNodeKey(EffectiveUserID(scope), r)
	if nodeKey == "" {
		http.Error(w, "missing node_key or self_node_id", http.StatusBadRequest)
		return
	}
	entry, err := s.st.LatestCompleteExamHistoryForUser(EffectiveUserID(scope), nodeKey)
	if err != nil {
		s.logger.Error("get latest exam history failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, entry) // entry 可能为 nil -> null,前端据此判定"暂无体检"
}

// handleGetExamHistory 查询某节点深度体检历史(时间倒序,完整体检口径:排除"出网+稳定性"
// 任务的缺段报告)。无历史返回空数组(200),不报错。
func (s *Server) handleGetExamHistory(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	nodeKey := s.resolveExamNodeKey(EffectiveUserID(scope), r)
	if nodeKey == "" {
		http.Error(w, "missing node_key or self_node_id", http.StatusBadRequest)
		return
	}
	list, err := s.st.ListCompleteExamHistoryForUser(EffectiveUserID(scope), nodeKey)
	if err != nil {
		s.logger.Error("list exam history failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, list)
}

// handleBatchExam 启动批量体检:node_keys 为空则对全部节点体检。
// 按请求者用户空间(多租户):"全部"= 本人池分片;任务按属主分片,互不干扰。
func (s *Server) handleBatchExam(w http.ResponseWriter, r *http.Request) {
	if s.detectionService == nil || s.batchExamJobs == nil {
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
		Mode     string   `json:"mode"` // simplified(默认)| full(完整四段)
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}

	// mode 校验:空按 simplified(老客户端不变);未知值 400,不静默降级。
	if req.Mode != "" && req.Mode != detection.BatchExamModeSimplified && req.Mode != detection.BatchExamModeFull {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "invalid mode: want simplified or full"})
		return
	}

	// node_keys 为空时对本人池全部节点体检
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

	key, err := s.batchExamJobs.StartFor(effUID, nodeKeys, nodes, scope, req.Mode)
	if err != nil {
		s.logger.Error("start batch exam failed", "error", err)
		writeJSONStatus(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, map[string]string{"status": "started", "key": key})
}

// handleBatchExamStream 订阅批量体检任务事件流(SSE):回放 + 直播。
func (s *Server) handleBatchExamStream(w http.ResponseWriter, r *http.Request) {
	if s.detectionService == nil || s.batchExamJobs == nil {
		http.Error(w, "detection service not initialized", http.StatusServiceUnavailable)
		return
	}

	userScope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	// 任务 key 固定为 "batch_exam"(按属主分片单例)
	key := "batch_exam"

	sub, err := s.batchExamJobs.SubscribeFor(EffectiveUserID(userScope), key)
	if err != nil {
		http.Error(w, "no active batch exam", http.StatusNotFound)
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

	// 回放缓冲事件
	for _, ev := range sub.Replay {
		writeFrame(ev.Data)
	}

	// 转直播
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

// handleBatchExamCancel 取消批量体检任务。
func (s *Server) handleBatchExamCancel(w http.ResponseWriter, r *http.Request) {
	if s.detectionService == nil || s.batchExamJobs == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]string{"error": "detection service not initialized"})
		return
	}

	userScope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	// 任务 key 固定为 "batch_exam"(按属主分片单例)
	key := "batch_exam"

	if !s.batchExamJobs.CancelFor(EffectiveUserID(userScope), key) {
		writeJSONStatus(w, http.StatusConflict, map[string]string{"error": "no active batch exam"})
		return
	}

	writeJSON(w, map[string]string{"status": "cancelled"})
}

// handleBatchSpeedtest 启动批量快速测速:对勾选节点逐个测基准下行(与体检基准行同口径)。
// node_keys 为空则对全部节点测速。
// 按请求者用户空间(多租户):"全部"= 本人池分片;任务按属主分片,互不干扰。
func (s *Server) handleBatchSpeedtest(w http.ResponseWriter, r *http.Request) {
	if s.detectionService == nil || s.speedtestJobs == nil {
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

	// node_keys 为空时对本人池全部节点测速
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

	key, err := s.speedtestJobs.StartFor(effUID, nodeKeys, nodes, scope)
	if err != nil {
		s.logger.Error("start batch speedtest failed", "error", err)
		writeJSONStatus(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, map[string]string{"status": "started", "key": key})
}

// handleBatchSpeedtestStream 订阅批量快速测速任务事件流(SSE):回放 + 直播。
func (s *Server) handleBatchSpeedtestStream(w http.ResponseWriter, r *http.Request) {
	if s.detectionService == nil || s.speedtestJobs == nil {
		http.Error(w, "detection service not initialized", http.StatusServiceUnavailable)
		return
	}

	userScope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	// 任务 key 固定为 "batch_speedtest"(按属主分片单例)
	key := "batch_speedtest"

	sub, err := s.speedtestJobs.SubscribeFor(EffectiveUserID(userScope), key)
	if err != nil {
		http.Error(w, "no active batch speedtest", http.StatusNotFound)
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

	// 回放缓冲事件
	for _, ev := range sub.Replay {
		writeFrame(ev.Data)
	}

	// 转直播
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

// handleBatchSpeedtestCancel 取消批量快速测速任务。
func (s *Server) handleBatchSpeedtestCancel(w http.ResponseWriter, r *http.Request) {
	if s.detectionService == nil || s.speedtestJobs == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]string{"error": "detection service not initialized"})
		return
	}

	userScope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	// 任务 key 固定为 "batch_speedtest"(按属主分片单例)
	key := "batch_speedtest"

	if !s.speedtestJobs.CancelFor(EffectiveUserID(userScope), key) {
		writeJSONStatus(w, http.StatusConflict, map[string]string{"error": "no active batch speedtest"})
		return
	}

	writeJSON(w, map[string]string{"status": "cancelled"})
}
