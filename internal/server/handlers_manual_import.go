package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/taliove/proxyhub/internal/aggregator"
	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

// manualImportMaxBytes 粘贴导入请求体上限(约 200 条分享链接 ~80KB,1MiB 余量充足;
// spec-manual-airport-import 遗留待决 3 定夺)。
const manualImportMaxBytes = 1 << 20

// airportUsageRequest 机场用量信息的手填载荷(手动机场创建/编辑/粘贴导入共用)。
// 全部指针以区分"未提供"(不动既有值)与"提供"(覆盖,含清零)。
// 用户按机场面板读数填"剩余/总量";存储模型是 upload/download/total,
// 换算:已用 = 总量 - 剩余(钳制 ≥0)全部计 download,上行未知计 0。
type airportUsageRequest struct {
	UsageRemaining *int64  `json:"usage_remaining"`
	UsageTotal     *int64  `json:"usage_total"`
	UsageExpire    *int64  `json:"usage_expire"` // unix 秒;0 = 未知
	WebPageURL     *string `json:"web_page_url"`
}

// provided 报告是否有任一用量字段被提供。
func (u *airportUsageRequest) provided() bool {
	return u.UsageRemaining != nil || u.UsageTotal != nil || u.UsageExpire != nil || u.WebPageURL != nil
}

// toUsageInfo 换算为存储模型的用量信息。
func (u *airportUsageRequest) toUsageInfo() *subscription.UsageInfo {
	var remaining, total, expire int64
	var webPageURL string
	if u.UsageRemaining != nil {
		remaining = *u.UsageRemaining
	}
	if u.UsageTotal != nil {
		total = *u.UsageTotal
	}
	if u.UsageExpire != nil {
		expire = *u.UsageExpire
	}
	if u.WebPageURL != nil {
		webPageURL = *u.WebPageURL
	}
	used := total - remaining
	if used < 0 {
		used = 0
	}
	return &subscription.UsageInfo{
		Upload:     0,
		Download:   used,
		Total:      total,
		Expire:     expire,
		WebPageURL: webPageURL,
	}
}

// handleImportAirport POST /api/airports/{id}/import 粘贴导入(同步 HTTP,非任务)。
//
// 两种来源都可用(2026-07-29 用户实测拍板):手动机场粘贴是唯一的节点来源;
// 拉取型机场粘贴是一次性导入——下次 URL 刷新成功自然以 URL 内容覆盖回来
// (同一单机场 upsert 语义),用于 URL 暂时不可达但手上有导出内容的场景。
//
// 凭证红线:粘贴内容含节点凭证——不落库、不进日志、不进 jobs params;
// 解析出的节点经单机场 upsert 入池(语义同单机场刷新,不跑健康检查)。
// 响应 {imported, failures[]}:部分行解析失败不阻断,失败行逐行报告(行号+原因)。
func (s *Server) handleImportAirport(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	effUID := EffectiveUserID(scope)
	airport, err := s.st.GetAirportByIDForUser(effUID, id)
	if err != nil {
		http.Error(w, "airport not found", http.StatusNotFound)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, manualImportMaxBytes)
	var req struct {
		Content string `json:"content"`
		airportUsageRequest
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			http.Error(w, "content too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		http.Error(w, "empty content", http.StatusBadRequest)
		return
	}

	// 凭证红线:req.Content 从解析起只在内存流转,进程结束即消失。
	decoded := subscription.DecodeSubscription([]byte(req.Content))
	parsed := subscription.ParseWithStats(decoded, airport.Name)
	// 行内重复链接后条覆盖前条(同 NodeKey upsert 语义)
	nodes := subscription.DedupeByNodeKey(parsed.Nodes)

	failures := parsed.Failures
	if failures == nil {
		failures = []subscription.LineFailure{}
	}
	if len(nodes) == 0 {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{
			"error":    "no valid nodes found",
			"imported": 0,
			"failures": failures,
		})
		return
	}

	if err := s.nodes.ImportManualAirportNodes(r.Context(), airport, nodes); err != nil {
		if errors.Is(err, aggregator.ErrRefreshConflict) {
			http.Error(w, "conflicts with a running refresh or airport test", http.StatusConflict)
			return
		}
		s.logger.Error("import airport nodes failed", "airport_id", id, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// 用量信息可选同贴,仅手动机场生效(拉取型机场的用量由响应头自动捕获,
	// 随贴字段一律忽略不报错——与设置接口忽略非适用键的惯例一致,design-airports.md)。
	// 顺序(Check L3):先入池成功再写用量,避免冲突 409 时用量已被覆写。
	if airport.SourceType == store.AirportSourceManual && req.provided() {
		if err := s.st.SetAirportUsageForUser(effUID, id, req.toUsageInfo()); err != nil {
			s.logger.Error("update airport usage failed", "airport_id", id, "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	}

	s.logger.Info("manual airport imported", "airport_id", id, "imported", len(nodes), "parse_failures", parsed.ParseFailures)
	writeJSON(w, map[string]any{
		"imported": len(nodes),
		"failures": failures,
	})
}
