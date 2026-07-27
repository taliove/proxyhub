package server

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/taliove/proxyhub/internal/subscription"
)

// 本机实测透传端点:浏览器 ← ProxyHub ← 节点 ← 测速端点。
// 浏览器 fetch 本端点(带 node_key),后端经选中节点(detection.ProxyHTTPClient,含直连出口)
// 访问 Cloudflare 测速端点,流式转发响应给浏览器。流量经浏览器(Network 可见大 Size),
// 且经用户下拉选定的节点(不依赖客户端代理选择)。8 并行 fetch 聚合带宽(fast.com 式)。
//
// 与 handleSpeedtestDownload 的差异:后者 ProxyHub 自建发流端点(浏览器直连测回环);
// 本端点经节点透传外部测速端点,测的是选定节点的真实链路。

const (
	// proxySpeedtestDownloadURL 透传下载的 Cloudflare 端点(8MB/连接 × 8 并行 = 64MB,
	// 接近 fast.com 单次测速总量;每连接独立 i 参数规避缓存/CDN 合并)。
	proxySpeedtestDownloadURL = "https://speed.cloudflare.com/__down?bytes=8000000"
	// proxySpeedtestLatencyURL 透传延迟的小请求端点(1KB)。
	proxySpeedtestLatencyURL = "https://speed.cloudflare.com/__down?bytes=1000"
	// proxySpeedtestUploadURL 透传上传的 Cloudflare 端点。
	proxySpeedtestUploadURL = "https://speed.cloudflare.com/__up"
	// proxySpeedtestTimeout 单次透传请求超时(流式下载可长达 30s,用 ctx deadline)。
	proxySpeedtestTimeout = 0
	// proxySpeedtestMaxDuration 透传下载最长时长(防卡死;对齐 detection.DirTimeout)。
	proxySpeedtestMaxDuration = 30 * time.Second
	// proxyBrowserUA 透传请求的浏览器 UA(Cloudflare 对默认 Go UA 返 403)。
	proxyBrowserUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"
)

// resolveProxySpeedtestNode 解析透传测速的节点(复用 resolveTestNode 口径,按属主限定)。
// nodeKey/selfNodeID 都空 = 直连基线(后端直连 Cloudflare,不经节点);解析不到返回 nil。
func (s *Server) resolveProxySpeedtestNode(userID, selfNodeID int64, nodeKey string) *subscription.Node {
	return s.resolveTestNode(userID, selfNodeID, nodeKey)
}

// handleSpeedtestProxyDownload 透传下载:经节点 GET Cloudflare 大文件,流式转发给浏览器。
// query: node_key / self_node_id(都空=直连基线)/ i(连接序号,仅日志区分)。
// 响应:application/octet-stream 流式转发(无 Content-Length,chunked)。
func (s *Server) handleSpeedtestProxyDownload(w http.ResponseWriter, r *http.Request) {
	if s.detectionService == nil {
		http.Error(w, "detection service not initialized", http.StatusServiceUnavailable)
		return
	}
	userScope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	node, err := s.resolveAndValidateNode(w, EffectiveUserID(userScope), q)
	if err != nil {
		return // resolveAndValidateNode 已写错误响应
	}

	// 经节点(或直连)构造 client,GET Cloudflare
	client, err := s.detectionService.ProxyHTTPClient(node, proxySpeedtestTimeout)
	if err != nil {
		s.logger.Warn("proxy download: create client failed", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), proxySpeedtestMaxDuration)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", proxySpeedtestDownloadURL, nil)
	if err != nil {
		http.Error(w, "create download request", http.StatusInternalServerError)
		return
	}
	// 每连接独立 i 参数规避 Cloudflare CDN 缓存合并(8 连接各拉独立 8MB 流)。
	if i := q.Get("i"); i != "" {
		req.URL.RawQuery += "&i=" + i
	}
	req.Header.Set("User-Agent", proxyBrowserUA)

	resp, err := client.Do(req)
	if err != nil {
		s.logger.Warn("proxy download: upstream failed", "error", err)
		http.Error(w, fmt.Sprintf("upstream: %v", err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		s.logger.Warn("proxy download: upstream status", "status", resp.StatusCode)
		http.Error(w, fmt.Sprintf("upstream HTTP %d", resp.StatusCode), http.StatusBadGateway)
		return
	}

	// 流式转发:边读边写给浏览器(无 Content-Length,chunked;Flush 保到达节奏)
	h := w.Header()
	h.Set("Content-Type", "application/octet-stream")
	h.Set("Cache-Control", "no-store, no-transform")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)

	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 32*1024)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		n, err := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				s.logger.Warn("proxy download: write to browser failed", "error", werr)
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err == io.EOF {
			return
		}
		if err != nil {
			s.logger.Warn("proxy download: read upstream failed", "error", err)
			return
		}
	}
}

// handleSpeedtestProxyLatency 透传延迟:经节点 GET Cloudflare 小请求(1KB),返回小响应给浏览器。
// 浏览器 8 次 fetch 测 RTT 算 latency/jitter。query 同 download。
func (s *Server) handleSpeedtestProxyLatency(w http.ResponseWriter, r *http.Request) {
	if s.detectionService == nil {
		http.Error(w, "detection service not initialized", http.StatusServiceUnavailable)
		return
	}
	userScope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	node, err := s.resolveAndValidateNode(w, EffectiveUserID(userScope), q)
	if err != nil {
		return
	}

	client, err := s.detectionService.ProxyHTTPClient(node, 10*time.Second)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", proxySpeedtestLatencyURL, nil)
	if err != nil {
		http.Error(w, "create latency request", http.StatusInternalServerError)
		return
	}
	req.Header.Set("User-Agent", proxyBrowserUA)

	resp, err := client.Do(req)
	if err != nil {
		s.logger.Warn("proxy latency: upstream failed", "error", err)
		http.Error(w, fmt.Sprintf("upstream: %v", err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		http.Error(w, fmt.Sprintf("upstream HTTP %d", resp.StatusCode), http.StatusBadGateway)
		return
	}

	// 转发小响应(1KB,含 body 使浏览器计 RTT 含数据传输)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(http.StatusOK)
	io.Copy(w, io.LimitReader(resp.Body, 4096))
}

// handleSpeedtestProxyUpload 透传上传:浏览器 POST 数据 → 后端转发到 Cloudflare __up。
// 浏览器 8 并行 POST 随机数据流。query 同 download。
func (s *Server) handleSpeedtestProxyUpload(w http.ResponseWriter, r *http.Request) {
	if s.detectionService == nil {
		http.Error(w, "detection service not initialized", http.StatusServiceUnavailable)
		return
	}
	userScope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	node, err := s.resolveAndValidateNode(w, EffectiveUserID(userScope), q)
	if err != nil {
		return
	}

	// buffer 浏览器 body 后带显式 Content-Length 转发:Cloudflare __up 需已知长度,
	// 且避免浏览器 duplex streaming(chrome HTTP/1.1 拒发)时 Go 客户端以 chunked 转发的坑。
	const maxUploadPerConn = 32 << 20 // 32MB 兜底(8 连接 × 4MB ≈ 32MB 总量)
	body, err := io.ReadAll(io.LimitReader(r.Body, maxUploadPerConn))
	if err != nil {
		s.logger.Warn("proxy upload: read browser body failed", "error", err)
		http.Error(w, "read upload body failed", http.StatusBadRequest)
		return
	}

	client, err := s.detectionService.ProxyHTTPClient(node, proxySpeedtestTimeout)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), proxySpeedtestMaxDuration)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", proxySpeedtestUploadURL, bytes.NewReader(body))
	if err != nil {
		http.Error(w, "create upload request", http.StatusInternalServerError)
		return
	}
	req.Header.Set("User-Agent", proxyBrowserUA)
	req.Header.Set("Content-Type", "application/octet-stream")
	req.ContentLength = int64(len(body))

	resp, err := client.Do(req)
	if err != nil {
		s.logger.Warn("proxy upload: upstream failed", "error", err)
		http.Error(w, fmt.Sprintf("upstream: %v", err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// 透传上游状态与 body(Cloudflare 回 {bytes} 或空)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, io.LimitReader(resp.Body, 4096))
}

// resolveAndValidateNode 解析透传节点:都空=直连基线(返回 nil, nil);
// 给了但解析不到(含命中他人资源)返回 404 错误。成功返回节点(或 nil=直连)。
func (s *Server) resolveAndValidateNode(w http.ResponseWriter, userID int64, q url.Values) (*subscription.Node, error) {
	var selfNodeID int64
	if v := q.Get("self_node_id"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			http.Error(w, "invalid self_node_id", http.StatusBadRequest)
			return nil, fmt.Errorf("bad self_node_id")
		}
		selfNodeID = id
	}
	nodeKey := q.Get("node_key")
	node := s.resolveProxySpeedtestNode(userID, selfNodeID, nodeKey)
	if (selfNodeID > 0 || nodeKey != "") && node == nil {
		http.Error(w, "node not found", http.StatusNotFound)
		return nil, fmt.Errorf("node not found")
	}
	return node, nil
}
