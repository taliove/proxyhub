package store

import (
	"fmt"
	"time"
)

// PullRecord 单次拉取记录
type PullRecord struct {
	EndpointID int64
	IP         string
	UserAgent  string
	// Status 本次拉取的结果(pull-guard ticket 01);空串按 PullStatusOK 记录,
	// 让 ticket 之前的调用点(只在成功下发后记录)语义不变。
	Status string
}

// RecordPull 记录一次订阅拉取(含被拦请求:状态见 pull_status.go)
func (s *Store) RecordPull(rec PullRecord) error {
	if rec.IP == "" {
		return fmt.Errorf("ip is required")
	}
	status := rec.Status
	if status == "" {
		status = PullStatusOK
	}
	if !IsValidPullStatus(status) {
		return fmt.Errorf("%w: unknown pull status %q", ErrInvalidInput, rec.Status)
	}
	_, err := s.db.Exec(
		`INSERT INTO pull_logs (endpoint_id, ip, user_agent, status) VALUES (?, ?, ?, ?)`,
		rec.EndpointID, rec.IP, rec.UserAgent, status,
	)
	if err != nil {
		return fmt.Errorf("insert pull log: %w", err)
	}
	return nil
}

// IPStat 按 IP + 拉取状态聚合的拉取统计。
// 同一 IP 的成功拉取与被拦尝试是不同的行(Status 区分),前端据此展示明细。
type IPStat struct {
	IP       string    `json:"ip"`
	Status   string    `json:"status"`
	Count    int       `json:"count"`
	LastPull time.Time `json:"last_pull"`
	Country  string    `json:"country"`
	Region   string    `json:"region"`
	City     string    `json:"city"`
	ISP      string    `json:"isp"`
}

// EndpointStats 某个订阅地址的统计（按 IP + 状态分组，附带地理信息）
func (s *Store) EndpointStats(endpointID int64) ([]*IPStat, error) {
	rows, err := s.db.Query(`
		SELECT p.ip, p.status, COUNT(*) AS cnt, MAX(p.pulled_at) AS last_pull,
		       COALESCE(g.country, ''), COALESCE(g.region, ''),
		       COALESCE(g.city, ''), COALESCE(g.isp, '')
		FROM pull_logs p
		LEFT JOIN ip_geo g ON g.ip = p.ip
		WHERE p.endpoint_id = ?
		GROUP BY p.ip, p.status
		ORDER BY cnt DESC`, endpointID)
	if err != nil {
		return nil, fmt.Errorf("query endpoint stats: %w", err)
	}
	defer rows.Close()

	var stats []*IPStat
	for rows.Next() {
		var st IPStat
		var lastPull string
		if err := rows.Scan(&st.IP, &st.Status, &st.Count, &lastPull,
			&st.Country, &st.Region, &st.City, &st.ISP); err != nil {
			return nil, fmt.Errorf("scan stat: %w", err)
		}
		st.LastPull = parseSQLiteTime(lastPull)
		stats = append(stats, &st)
	}
	return stats, rows.Err()
}

// parseSQLiteTime 解析 SQLite CURRENT_TIMESTAMP 产生的时间字符串。
// 聚合函数（如 MAX）会丢失列类型信息，驱动返回原始字符串。
func parseSQLiteTime(s string) time.Time {
	for _, layout := range []string{
		"2006-01-02 15:04:05",
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999-07:00",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// GeoInfo IP 地理位置信息
type GeoInfo struct {
	IP      string `json:"ip"`
	Country string `json:"country"`
	Region  string `json:"region"`
	City    string `json:"city"`
	ISP     string `json:"isp"`
}

// SaveGeo 缓存 IP 地理位置
func (s *Store) SaveGeo(geo GeoInfo) error {
	if geo.IP == "" {
		return fmt.Errorf("ip is required")
	}
	_, err := s.db.Exec(`
		INSERT INTO ip_geo (ip, country, region, city, isp, resolved_at)
		VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(ip) DO UPDATE SET
			country = excluded.country, region = excluded.region,
			city = excluded.city, isp = excluded.isp,
			resolved_at = CURRENT_TIMESTAMP`,
		geo.IP, geo.Country, geo.Region, geo.City, geo.ISP)
	if err != nil {
		return fmt.Errorf("save geo: %w", err)
	}
	return nil
}

// GetGeo 查询缓存的 IP 地理位置，未命中返回 ErrNotFound
func (s *Store) GetGeo(ip string) (*GeoInfo, error) {
	var geo GeoInfo
	err := s.db.QueryRow(
		`SELECT ip, country, region, city, isp FROM ip_geo WHERE ip = ?`, ip,
	).Scan(&geo.IP, &geo.Country, &geo.Region, &geo.City, &geo.ISP)
	if err != nil {
		if isNoRows(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get geo: %w", err)
	}
	return &geo, nil
}

// UnresolvedIPs 返回还没有地理信息的 IP 列表（供后台异步解析）
func (s *Store) UnresolvedIPs(limit int) ([]string, error) {
	rows, err := s.db.Query(`
		SELECT DISTINCT p.ip FROM pull_logs p
		LEFT JOIN ip_geo g ON g.ip = p.ip
		WHERE g.ip IS NULL
		LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("query unresolved ips: %w", err)
	}
	defer rows.Close()

	var ips []string
	for rows.Next() {
		var ip string
		if err := rows.Scan(&ip); err != nil {
			return nil, fmt.Errorf("scan ip: %w", err)
		}
		ips = append(ips, ip)
	}
	return ips, rows.Err()
}
