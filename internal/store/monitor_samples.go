package store

import "time"

// 订阅节点监控打点(ADR 0047 / issue #99)。按 node_key 物理维度存储
// (跨用户去重后只探一次);保留 7 天,由 nodemon 每轮 prune。

// MonitorSample 一条探测打点(读侧视图)
type MonitorSample struct {
	NodeKey   string    `json:"node_key"`
	OK        bool      `json:"ok"`
	LatencyMs int       `json:"latency_ms"`
	CheckedAt time.Time `json:"checked_at"`
}

// SaveMonitorSample 写入一条探测打点。checked_at 用 RFC3339 落库
// (与 SQL datetime() 比较对齐,见 store 时间格式教训)。
func (s *Store) SaveMonitorSample(nodeKey string, ok bool, latencyMs int, checkedAt time.Time) error {
	_, err := s.db.Exec(
		`INSERT INTO node_monitor_samples (node_key, ok, latency_ms, checked_at) VALUES (?, ?, ?, ?)`,
		nodeKey, boolToInt(ok), latencyMs, checkedAt.UTC().Format(time.RFC3339))
	return err
}

// PruneMonitorSamples 清理早于 before 的打点(每轮调用,7 天窗口外数据)
func (s *Store) PruneMonitorSamples(before time.Time) error {
	_, err := s.db.Exec(
		`DELETE FROM node_monitor_samples WHERE checked_at < ?`,
		before.UTC().Format(time.RFC3339))
	return err
}

// ListMonitorSamples 读节点最近打点(新到旧,limit 截断),供节点详情/趋势
// (issue #103)与测试断言。
func (s *Store) ListMonitorSamples(nodeKey string, limit int) ([]MonitorSample, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Query(
		`SELECT node_key, ok, latency_ms, checked_at FROM node_monitor_samples
		 WHERE node_key = ? ORDER BY checked_at DESC, id DESC LIMIT ?`,
		nodeKey, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := []MonitorSample{}
	for rows.Next() {
		var smp MonitorSample
		var ok int
		var checkedStr string
		if err := rows.Scan(&smp.NodeKey, &ok, &smp.LatencyMs, &checkedStr); err != nil {
			return nil, err
		}
		smp.OK = ok != 0
		smp.CheckedAt = parseSlotTime(checkedStr)
		result = append(result, smp)
	}
	return result, rows.Err()
}
