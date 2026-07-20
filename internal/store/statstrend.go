package store

import (
	"fmt"
	"time"
)

// GlobalStats 返回全局访问汇总：总拉取数、独立 IP 数、活跃订阅数（最近 24h 有拉取的地址数）。
func (s *Store) GlobalStats() (totalPulls, uniqueIPs, activeEndpoints int, err error) {
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM pull_logs`).Scan(&totalPulls); err != nil {
		return 0, 0, 0, fmt.Errorf("count total pulls: %w", err)
	}
	if err = s.db.QueryRow(`SELECT COUNT(DISTINCT ip) FROM pull_logs`).Scan(&uniqueIPs); err != nil {
		return 0, 0, 0, fmt.Errorf("count unique ips: %w", err)
	}
	// 活跃订阅：最近 24h 有拉取的不同 endpoint_id 数
	since := time.Now().UTC().Add(-24 * time.Hour).Format("2006-01-02 15:04:05")
	if err = s.db.QueryRow(
		`SELECT COUNT(DISTINCT endpoint_id) FROM pull_logs WHERE datetime(pulled_at) >= datetime(?)`,
		since).Scan(&activeEndpoints); err != nil {
		return 0, 0, 0, fmt.Errorf("count active endpoints: %w", err)
	}
	return totalPulls, uniqueIPs, activeEndpoints, nil
}

// TrendPoint 趋势图数据点：某天某订阅地址的拉取次数
type TrendPoint struct {
	Date       string `json:"date"` // YYYY-MM-DD
	EndpointID int64  `json:"endpoint_id"`
	Alias      string `json:"alias"`
	Count      int    `json:"count"`
}

// PullTrend 返回最近 days 天每天每个订阅地址的拉取次数（按日期升序），用于趋势图。
func (s *Store) PullTrend(days int) ([]TrendPoint, error) {
	since := time.Now().UTC().AddDate(0, 0, -days).Format("2006-01-02 15:04:05")
	rows, err := s.db.Query(`
		SELECT date(p.pulled_at) AS d, p.endpoint_id, COALESCE(e.alias, ''), COUNT(*)
		FROM pull_logs p
		LEFT JOIN endpoints e ON e.id = p.endpoint_id
		WHERE datetime(p.pulled_at) >= datetime(?)
		GROUP BY d, p.endpoint_id
		ORDER BY d ASC`, since)
	if err != nil {
		return nil, fmt.Errorf("query pull trend: %w", err)
	}
	defer rows.Close()

	var points []TrendPoint
	for rows.Next() {
		var p TrendPoint
		if err := rows.Scan(&p.Date, &p.EndpointID, &p.Alias, &p.Count); err != nil {
			return nil, fmt.Errorf("scan trend point: %w", err)
		}
		points = append(points, p)
	}
	return points, rows.Err()
}
