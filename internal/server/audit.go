package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/taliove/proxyhub/internal/store"
)

// defaultAuditPageSize 审计事件列表的默认分页大小
const defaultAuditPageSize = 50

// recordAudit 写一条审计事件；失败只记日志、不阻断登录流程。
func (s *Server) recordAudit(eventType, ip, username, detail string) {
	if err := s.st.RecordAuditEvent(eventType, ip, username, detail); err != nil {
		s.logger.Warn("record audit event failed", "type", eventType, "error", err)
	}
}

// timeRangeSince 把前端的时间范围参数（24h/7d/30d/all）转成起始时间。all 返回零值（不过滤）。
func timeRangeSince(rangeStr string) time.Time {
	now := time.Now()
	switch rangeStr {
	case "24h":
		return now.Add(-24 * time.Hour)
	case "7d":
		return now.AddDate(0, 0, -7)
	case "30d":
		return now.AddDate(0, 0, -30)
	default: // all 或空
		return time.Time{}
	}
}

// handleAuditEvents 查询审计事件流水（过滤 + 分页）
func (s *Server) handleAuditEvents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	filter := store.AuditFilter{
		IP:    strings.TrimSpace(q.Get("ip")),
		Since: timeRangeSince(q.Get("time_range")),
	}
	if et := q.Get("event_type"); et != "" {
		filter.EventTypes = strings.Split(et, ",")
	}

	limit := parseIntDefault(q.Get("limit"), defaultAuditPageSize)
	offset := parseIntDefault(q.Get("offset"), 0)

	events, total, err := s.st.ListAuditEvents(filter, limit, offset)
	if err != nil {
		s.logger.Error("list audit events failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"events": events, "total": total})
}

// handleBannedIPs 列出当前封禁 IP
func (s *Server) handleBannedIPs(w http.ResponseWriter, r *http.Request) {
	banned, err := s.st.ListBannedIPs()
	if err != nil {
		s.logger.Error("list banned ips failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"banned": banned})
}

// handleUnbanIP 手动解封 IP
func (s *Server) handleUnbanIP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IP string `json:"ip"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.IP) == "" {
		http.Error(w, "ip is required", http.StatusBadRequest)
		return
	}
	if err := s.st.UnbanIP(req.IP); err != nil {
		s.logger.Error("unban ip failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]bool{"success": true})
}

// parseIntDefault 解析整数，失败或非正数时返回默认值
func parseIntDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return def
	}
	return n
}
