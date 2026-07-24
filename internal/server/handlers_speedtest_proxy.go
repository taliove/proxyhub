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

	// ctx 120s:经节点各阶段最多重试 3 次(latency ~24s + bandwidth ~75s),直连更短。
	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()

	// 经节点:latency 与 bandwidth 各自最多重试 3 次,直到成功;3 次失败则保留已测
	// (latency 置 0 / 带宽 0)继续后续阶段,仅全部失败才报 error。避免"跑一半报错就什么都没了"。
	if node != nil {
		if s.detectionService == nil {
			emitErr("detection service not initialized")
			return
		}
		// latency 重试 3 次
		latencyMs, jitterMs := 0, 0.0
		for attempt := 1; attempt <= 3; attempt++ {
			if lat, jit, err := s.detectionService.TestNodeLatency(ctx, node, 8); err == nil {
				latencyMs, jitterMs = lat, jit
				break
			} else if attempt == 3 {
				s.logger.Warn("proxy speedtest latency failed after 3 attempts",
					"node_key", nodeKey, "self_node_id", selfNodeID, "error", err)
			}
		}
		emit(map[string]any{"phase": "latency", "idle_latency_ms": latencyMs, "jitter_ms": jitterMs})

		// bandwidth 重试 3 次(全失败才重试;部分成功即停)。重试时 sample 帧重新滚动。
		var tr detection.TestResult
		for attempt := 1; attempt <= 3; attempt++ {
			tr = s.detectionService.TestSpeedtestStream(ctx, node, func(sample detection.Sample) {
				emit(sample)
			})
			// 成功或部分成功(任一方向有值)即停;全失败才重试
			if tr.Error == "" || tr.DownMbps > 0 || tr.UpMbps > 0 {
				break
			}
			if attempt < 3 {
				s.logger.Warn("proxy speedtest bandwidth failed, retrying",
					"node_key", nodeKey, "attempt", attempt, "error", tr.Error)
			} else {
				s.logger.Warn("proxy speedtest bandwidth failed after 3 attempts",
					"node_key", nodeKey, "self_node_id", selfNodeID, "error", tr.Error)
			}
		}
		// 全失败(latency+带宽都 0)才 error;否则 emit done 保留已测值
		if tr.DownMbps == 0 && tr.UpMbps == 0 && tr.Error != "" && latencyMs == 0 && jitterMs == 0 {
			emitErr(tr.Error)
			return
		}
		emit(map[string]any{
			"phase":           "done",
			"down_mbps":       tr.DownMbps,
			"up_mbps":         tr.UpMbps,
			"idle_latency_ms": latencyMs,
			"jitter_ms":       jitterMs,
			"elapsed_ms":      tr.ElapsedMs,
		})
		return
	}

	// 直连:latency + bandwidth(speedtest.RunProxyTest,onLatency/onSample 实时帧)。
	// 整体重试 3 次:err 时重试(端点波动),成功即停;3 次失败报 error。
	req := speedtest.ProxyTestRequest{Mode: mode}
	onLatency := func(latMs, jitMs float64) {
		emit(map[string]any{"phase": "latency", "idle_latency_ms": latMs, "jitter_ms": jitMs})
	}
	onSample := func(sample detection.Sample) {
		emit(sample)
	}
	var result *speedtest.ProxyTestResult
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		r, err := speedtest.RunProxyTest(ctx, req, nil, onLatency, onSample)
		if err == nil {
			result = r
			break
		}
		lastErr = err
		if attempt < 3 {
			s.logger.Warn("direct speedtest failed, retrying", "attempt", attempt, "error", err)
		} else {
			s.logger.Warn("direct speedtest failed after 3 attempts", "error", err)
		}
	}
	if result == nil {
		emitErr(lastErr.Error())
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
