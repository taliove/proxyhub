package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/taliove/proxyhub/internal/speedtest"
)

// handleSpeedtestProxyTest 后端代理测速:浏览器发起,后端经选中节点(或直连)
// 访问测速端点,测全链路带宽(用户 <-> ProxyHub <-> 节点 <-> 端点)。
// 与 handleSpeedtestDownload 的差异:后者是浏览器直连 ProxyHub 自建发流端点,
// 测的是本机回环(虚高);本端点真正经节点出口,反映用户通过节点的实际体验。
//
// 经节点路径复用 detection.TestSpeedtestStream:它注入直连出口 dialer(TUN 环境下
// 让节点连真实 IP 而非 fake-ip)、带浏览器 UA + 端点 fallback,是体检/快速测速
// 同款成熟逻辑。直连模式(无节点)走 speedtest.RunProxyTest 的标准 client 路径。
//
// 请求体:{"node_key":"...","self_node_id":123,"mode":"full","download_duration_ms":10000,"upload_duration_ms":10000}
// node_key 与 self_node_id 二选一;都不传 = 直连基线。mode: latency|download|upload|full(默认 full)。
func (s *Server) handleSpeedtestProxyTest(w http.ResponseWriter, r *http.Request) {
	var req speedtest.ProxyTestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}

	// 解析节点:复用单节点测试的公共解析逻辑(自建查库 ToNode,机场查内存池)。
	// nodeKey 与 SelfNodeID 都空 = 直连模式(node = nil),不报 400。
	var nodeKeyInt64 int64
	if req.SelfNodeID != nil {
		nodeKeyInt64 = int64(*req.SelfNodeID)
	}
	node := s.resolveTestNode(nodeKeyInt64, req.NodeKey)
	// 给了目标但解析不到 = 资源不存在(404);都没给 = 直连,放行
	if (nodeKeyInt64 > 0 || req.NodeKey != "") && node == nil {
		writeJSONStatus(w, http.StatusNotFound, map[string]string{"error": "node not found"})
		return
	}

	// 默认值
	if req.Mode == "" {
		req.Mode = "full"
	}
	switch req.Mode {
	case "latency", "download", "upload", "full":
	default:
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "invalid mode"})
		return
	}

	// 总超时 30s:经节点下行/上行各 10s + 探测;直连更短。ctx deadline 确保前端取消即终止。
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	// 经节点:复用 detection 的带宽测速(直连出口 + UA + fallback,与体检同款)。
	if node != nil {
		if s.detectionService == nil {
			writeJSONStatus(w, http.StatusServiceUnavailable, map[string]string{"error": "detection service not initialized"})
			return
		}
		tr := s.detectionService.TestSpeedtestStream(ctx, node, nil)
		// down/up 都 0 = 真实失败(连接超时/端点不可达),报错;阈值不达标(有值)不报错,返回实测值。
		if tr.DownMbps == 0 && tr.UpMbps == 0 && tr.Error != "" {
			s.logger.Warn("proxy speedtest failed",
				"node_key", req.NodeKey, "self_node_id", nodeKeyInt64, "error", tr.Error)
			writeJSONStatus(w, http.StatusInternalServerError, map[string]string{"error": tr.Error})
			return
		}
		writeJSON(w, speedtest.ProxyTestResult{
			DownMbps:      tr.DownMbps,
			UpMbps:        tr.UpMbps,
			IdleLatencyMs: float64(tr.Latency), // detection 不测 HTTP RTT,latency 0
			JitterMs:      0,
			ElapsedMs:     tr.ElapsedMs,
		})
		return
	}

	// 直连:标准 client 走 speedtest 自实现(得 latency/jitter + down/up)。
	result, err := speedtest.RunProxyTest(ctx, req, nil)
	if err != nil {
		s.logger.Warn("direct speedtest failed", "error", err)
		writeJSONStatus(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, result)
}
