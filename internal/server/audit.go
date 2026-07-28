package server

import (
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/taliove/proxyhub/internal/store"
)

// defaultAuditPageSize 审计事件列表的默认分页大小
const defaultAuditPageSize = 50

// recordAudit 写一条审计事件；失败只记日志、不阻断登录流程。
func (s *Server) recordAudit(eventType, ip, username, detail, userAgent string) {
	if err := s.st.RecordAuditEvent(eventType, ip, username, detail, userAgent); err != nil {
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

	// Attach geo information to each event
	enrichedEvents := make([]map[string]any, 0, len(events))
	for _, event := range events {
		enriched := map[string]any{
			"id":         event.ID,
			"event_type": event.EventType,
			"ip":         event.IP,
			"username":   event.Username,
			"detail":     event.Detail,
			"user_agent": event.UserAgent,
			"created_at": event.CreatedAt,
			"geo": map[string]string{
				"country": "",
				"region":  "",
				"city":    "",
				"isp":     "",
			},
		}

		// Attach geo info if available (private/loopback IPs degrade to empty strings)
		if event.IP != "" && !isPrivateOrLoopback(event.IP) {
			if geo, err := s.st.GetGeo(event.IP); err == nil && geo != nil {
				enriched["geo"] = map[string]string{
					"country": geo.Country,
					"region":  geo.Region,
					"city":    geo.City,
					"isp":     geo.ISP,
				}
			}
		}

		enrichedEvents = append(enrichedEvents, enriched)
	}

	writeJSON(w, map[string]any{"events": enrichedEvents, "total": total})
}

// isPrivateOrLoopback checks if an IP is private, loopback, or reserved
func isPrivateOrLoopback(ip string) bool {
	parsed := parseIP(ip)
	if parsed == nil {
		return true // treat invalid as private
	}
	return parsed.IsPrivate() || parsed.IsLoopback() || parsed.IsUnspecified()
}

// parseIP is a helper to parse IP strings
func parseIP(s string) *net.IP {
	ip := net.ParseIP(s)
	return &ip
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

// handleBanIP 手动封禁 IP
func (s *Server) handleBanIP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IP       string `json:"ip"`
		Duration string `json:"duration"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.IP) == "" {
		http.Error(w, "ip is required", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Duration) == "" {
		http.Error(w, "duration is required", http.StatusBadRequest)
		return
	}

	// Parse duration: support time.Duration format (1h, 24h) or "permanent"
	var duration time.Duration
	var detail string
	if req.Duration == "permanent" {
		// Use 100 years as permanent (consistent with existing convention)
		duration = 100 * 365 * 24 * time.Hour
		detail = "永久封禁"
	} else {
		d, err := time.ParseDuration(req.Duration)
		if err != nil {
			http.Error(w, "invalid duration format", http.StatusBadRequest)
			return
		}
		if d <= 0 {
			http.Error(w, "duration must be positive", http.StatusBadRequest)
			return
		}
		duration = d
		detail = "封禁 " + req.Duration
	}

	now := time.Now()
	bannedUntil, err := s.st.BanIP(req.IP, duration, now)
	if err != nil {
		s.logger.Error("ban ip failed", "ip", req.IP, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Record audit event with user agent
	s.recordAudit("ip_banned", s.clientIP(r), "",
		detail+"，封禁至 "+bannedUntil.Format("2006-01-02 15:04:05"),
		r.UserAgent())

	writeJSON(w, map[string]any{
		"success":      true,
		"ip":           req.IP,
		"banned_until": bannedUntil.Format(time.RFC3339),
	})
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
