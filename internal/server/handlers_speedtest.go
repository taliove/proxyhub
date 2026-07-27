package server

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/taliove/proxyhub/internal/speedtest"
	"github.com/taliove/proxyhub/internal/store"
)

// 本机实测(浏览器端 fast.com 式测速)管理面端点,见 ticket 0032。
// 全部挂 /api/speedtest/* 且过 requireAuth:下行发流是带宽放大器,
// 挂公开面等于开流量自助机(spec 遗留待决 1 已定:接受同机换地址重登录)。

const (
	// maxSpeedtestNodeKeyLen node_key 标注长度上限(NodeKey 形如 server:port)。
	maxSpeedtestNodeKeyLen = 256
	// maxSpeedtestClientInfoLen 客户端自报信息长度上限。
	maxSpeedtestClientInfoLen = 512
)

// handleSpeedtestPing 延迟探测:极小响应体,浏览器多次小请求算 RTT/抖动。
func (s *Server) handleSpeedtestPing(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte("ok"))
}

// parseDownloadDuration 解析下行时长参数(duration_ms),钳制到 [Min, Max]。
// 上限 MaxDownloadDuration 保证单次请求在全局 30s WriteTimeout 内结束。
func parseDownloadDuration(r *http.Request) time.Duration {
	ms, err := strconv.Atoi(r.URL.Query().Get("duration_ms"))
	if err != nil {
		return speedtest.DefaultDownloadDuration
	}
	d := time.Duration(ms) * time.Millisecond
	if d < speedtest.MinDownloadDuration {
		return speedtest.MinDownloadDuration
	}
	if d > speedtest.MaxDownloadDuration {
		return speedtest.MaxDownloadDuration
	}
	return d
}

// handleSpeedtestDownload 下行发流:不可压缩随机字节 + 显式禁压缩,
// 时长/字节双上限,单次请求必在 30s 全局 WriteTimeout 内结束。
// 不设 Content-Length:chunked 流式下发,浏览器按到达节奏实时测速。
func (s *Server) handleSpeedtestDownload(w http.ResponseWriter, r *http.Request) {
	block, err := speedtest.NewRandomBlock(speedtest.DownloadBlockSize)
	if err != nil {
		s.logger.Error("speedtest download random block failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	duration := parseDownloadDuration(r)

	h := w.Header()
	h.Set("Content-Type", "application/octet-stream")
	// no-store 防缓存;no-transform 防中间层(Caddy gzip / 节点压缩)变换测速流
	// (压缩会让浏览器端字节数虚高)。不显式设 Content-Encoding(缺省即 identity,
	// 显式写 "identity" 非 RFC 推荐用法)。
	h.Set("Cache-Control", "no-store, no-transform")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)

	// 写流错误多为客户端中断(测到一半取消),不是服务端故障,记 Warn 即可
	if _, err := speedtest.StreamDownload(r.Context(), w, block, time.Now().Add(duration), speedtest.DownloadMaxBytes); err != nil {
		s.logger.Warn("speedtest download stream ended early", "error", err)
	}
}

// handleSpeedtestUpload 上行收流:读丢弃计数,返回收到的字节数。
// 时长由客户端控制(到点停止发送即 EOF);maxBytes 是客户端未停流时的服务端兜底。
// 不设 ContentLength 的 chunked 上传与已知长度上传同样适用。
func (s *Server) handleSpeedtestUpload(w http.ResponseWriter, r *http.Request) {
	n, err := speedtest.CountUpload(r.Body, speedtest.MaxUploadBytes)
	if err != nil {
		s.logger.Warn("speedtest upload read failed", "error", err)
		http.Error(w, "upload read failed", http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"bytes": n})
}

// handleSaveSpeedtestResult 落库一条实测结果:数值必须有限且非负,
// node_key 空 = 直连/未标注(独立修剪桶),写入即修剪该桶至最近 50 条。
// 按请求者属主分桶(多租户):历史互不可见、修剪互不占额。
func (s *Server) handleSaveSpeedtestResult(w http.ResponseWriter, r *http.Request) {
	userScope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	var req struct {
		NodeKey       string  `json:"node_key"`
		DownMbps      float64 `json:"down_mbps"`
		UpMbps        float64 `json:"up_mbps"`
		IdleLatencyMs float64 `json:"idle_latency_ms"`
		JitterMs      float64 `json:"jitter_ms"`
		ClientInfo    string  `json:"client_info"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	for _, v := range []float64{req.DownMbps, req.UpMbps, req.IdleLatencyMs, req.JitterMs} {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
			writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "metrics must be finite and non-negative"})
			return
		}
	}
	if len(req.NodeKey) > maxSpeedtestNodeKeyLen || len(req.ClientInfo) > maxSpeedtestClientInfoLen {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "node_key or client_info too long"})
		return
	}

	id, err := s.st.SaveSpeedtestResultForUser(EffectiveUserID(userScope), store.SpeedtestResult{
		NodeKey:       req.NodeKey,
		DownMbps:      req.DownMbps,
		UpMbps:        req.UpMbps,
		IdleLatencyMs: req.IdleLatencyMs,
		JitterMs:      req.JitterMs,
		ClientInfo:    req.ClientInfo,
	})
	if err != nil {
		s.logger.Error("save speedtest result failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"id": id})
}

// handleListSpeedtestResults 查询实测历史(时间倒序)。
// node_key 参数缺省 = 全量;存在但为空 = 只看直连;具体值 = 该节点历史(孤儿历史照常读出)。
// 按请求者属主过滤(多租户):他人历史不可见。
func (s *Server) handleListSpeedtestResults(w http.ResponseWriter, r *http.Request) {
	userScope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	var filter *string
	if vals, ok := r.URL.Query()["node_key"]; ok && len(vals) > 0 {
		v := vals[0]
		filter = &v
	}
	list, err := s.st.ListSpeedtestResultsForUser(EffectiveUserID(userScope), filter)
	if err != nil {
		s.logger.Error("list speedtest results failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, list)
}

// handleDeleteSpeedtestResult 按 id 删除一条实测历史;不存在或属他人 404(多租户)。
func (s *Server) handleDeleteSpeedtestResult(w http.ResponseWriter, r *http.Request) {
	userScope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}
	if err := s.st.DeleteSpeedtestResultForUser(EffectiveUserID(userScope), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeJSONStatus(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		s.logger.Error("delete speedtest result failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"deleted": true})
}
