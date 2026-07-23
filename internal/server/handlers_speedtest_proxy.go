package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/taliove/proxyhub/internal/detection"
	"github.com/taliove/proxyhub/internal/speedtest"
)

// handleSpeedtestProxyTest 后端代理测速 SSE 流式端点(fast.com 式实时数字)。
// 浏览器用 EventSource 订阅,后端依次推送 latency/sample/done/error 帧:
//   latency {phase:"latency", idle_latency_ms, jitter_ms}
//   sample {phase:"download"|"upload", mbps, elapsed_ms}  (每 ~300ms 一帧,实时跳动)
//   done   {phase:"done", down_mbps, up_mbps, idle_latency_ms, jitter_ms, elapsed_ms}
//   error  {phase:"error", error}
//
// 经节点路径复用 detection(TestNodeLatency + TestSpeedtestStream,注入直连出口 +
// 浏览器 UA + 端点 fallback,与体检同款);直连路径走 speedtest.RunProxyTest(标准
// client + UA + Linode fallback,得 latency/jitter + down/up)。
//
// query: node_key / self_node_id(二选一,都空=直连基线) / mode(latency|download|upload|full,默认 full)。
func (s *Server) handleSpeedtestProxyTest(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var nodeKey string
	var selfNodeID int64
	if v := q.Get("self_node_id"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "invalid self_node_id"})
			return
		}
		selfNodeID = id
	}
	nodeKey = q.Get("node_key")
	mode := q.Get("mode")
	if mode == "" {
		mode = "full"
	}
	switch mode {
	case "latency", "download", "upload", "full":
	default:
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "invalid mode"})
		return
	}

	node := s.resolveTestNode(selfNodeID, nodeKey)
	if (selfNodeID > 0 || nodeKey != "") && node == nil {
		writeJSONStatus(w, http.StatusNotFound, map[string]string{"error": "node not found"})
		return
	}

	// SSE 头(抄 handleTestNodeStream:X-Accel-Buffering:no 禁 nginx 缓冲,每帧 Flush)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]string{"error": "streaming not supported"})
		return
	}
	emit := func(data any) {
		b, _ := json.Marshal(data)
		fmt.Fprintf(w, "data: %s\n\n", b)
		flusher.Flush()
	}
	emitErr := func(errMsg string) {
		emit(map[string]string{"phase": "error", "error": errMsg})
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	// 经节点:latency(detection.TestNodeLatency)+ bandwidth(detection.TestSpeedtestStream)
	if node != nil {
		if s.detectionService == nil {
			emitErr("detection service not initialized")
			return
		}
		latencyMs, jitterMs, err := s.detectionService.TestNodeLatency(ctx, node, 8)
		if err != nil {
			s.logger.Warn("proxy speedtest latency failed",
				"node_key", nodeKey, "self_node_id", selfNodeID, "error", err)
			emitErr(err.Error())
			return
		}
		emit(map[string]any{"phase": "latency", "idle_latency_ms": latencyMs, "jitter_ms": jitterMs})

		tr := s.detectionService.TestSpeedtestStream(ctx, node, func(sample detection.Sample) {
			emit(sample)
		})
		if tr.DownMbps == 0 && tr.UpMbps == 0 && tr.Error != "" {
			s.logger.Warn("proxy speedtest bandwidth failed",
				"node_key", nodeKey, "self_node_id", selfNodeID, "error", tr.Error)
			emitErr(tr.Error)
			return
		}
		emit(map[string]any{
			"phase":            "done",
			"down_mbps":        tr.DownMbps,
			"up_mbps":          tr.UpMbps,
			"idle_latency_ms":  latencyMs,
			"jitter_ms":        jitterMs,
			"elapsed_ms":       tr.ElapsedMs,
		})
		return
	}

	// 直连:latency + bandwidth(speedtest.RunProxyTest,onLatency/onSample 实时帧)
	req := speedtest.ProxyTestRequest{Mode: mode}
	result, err := speedtest.RunProxyTest(ctx, req, nil,
		func(latMs, jitMs float64) {
			emit(map[string]any{"phase": "latency", "idle_latency_ms": latMs, "jitter_ms": jitMs})
		},
		func(sample detection.Sample) {
			emit(sample)
		},
	)
	if err != nil {
		s.logger.Warn("direct speedtest failed", "error", err)
		emitErr(err.Error())
		return
	}
	emit(map[string]any{
		"phase":           "done",
		"down_mbps":       result.DownMbps,
		"up_mbps":         result.UpMbps,
		"idle_latency_ms": result.IdleLatencyMs,
		"jitter_ms":       result.JitterMs,
		"elapsed_ms":      result.ElapsedMs,
	})
}
