package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/taliove/proxyhub/internal/speedtest"
)

// handleSpeedtestProxyTest 后端代理测速:浏览器发起,后端经选中节点(或直连)
// 访问 Cloudflare 测速端点,测全链路带宽(用户 <-> ProxyHub <-> 节点 <-> 端点)。
// 与 handleSpeedtestDownload 的差异:后者是浏览器直连 ProxyHub 自建发流端点,
// 测的是本机回环(虚高);本端点真正经节点出口,反映用户通过节点的实际体验。
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

	// 总超时 30s:延迟 + 下行 + 上行各阶段经同一 client,client.Timeout 已设 30s。
	// 这里再套一层 context deadline,确保前端取消时立即终止测速。
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	result, err := speedtest.RunProxyTest(ctx, req, node)
	if err != nil {
		s.logger.Warn("proxy speedtest failed",
			"node_key", req.NodeKey, "self_node_id", nodeKeyInt64, "error", err)
		writeJSONStatus(w, http.StatusInternalServerError, map[string]string{
			"error": err.Error(),
		})
		return
	}
	writeJSON(w, result)
}
