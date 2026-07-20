package store

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/taliove/proxyhub/internal/detection"
)

// GetDetectionTargets 读取检测目标配置列表
func (s *Store) GetDetectionTargets() ([]detection.Target, error) {
	val, err := s.GetSetting("detection_targets")
	if err != nil {
		if err == ErrNotFound {
			// 未配置则返回空列表(迁移会写入默认值,此处仅作降级)
			return []detection.Target{}, nil
		}
		return nil, fmt.Errorf("get detection_targets setting: %w", err)
	}

	var targets []detection.Target
	if err := json.Unmarshal([]byte(val), &targets); err != nil {
		return nil, fmt.Errorf("unmarshal detection_targets: %w", err)
	}
	return targets, nil
}

// SetDetectionTargets 保存检测目标配置列表
func (s *Store) SetDetectionTargets(targets []detection.Target) error {
	data, err := json.Marshal(targets)
	if err != nil {
		return fmt.Errorf("marshal detection_targets: %w", err)
	}
	return s.SetSetting("detection_targets", string(data))
}

// DetectionResultView 单个节点单个目标的最新检测结果
type DetectionResultView struct {
	TargetName string    `json:"target_name"`
	Available  bool      `json:"available"`
	Latency    int       `json:"latency"`
	Error      string    `json:"error"`
	CheckedAt  time.Time `json:"checked_at"`
	DownMbps   float64   `json:"down_mbps,omitempty"`
	UpMbps     float64   `json:"up_mbps,omitempty"`
}

// GetLatestDetectionResults 查询给定节点的最新多维检测结果。
// 返回 map[nodeKey][]DetectionResultView,每个节点一组(每个 target 最新一条)。
// 只返回真实检测目标(target_name != 'connectivity' 的 TCP 快检记录一并返回,由调用方决定是否展示)。
func (s *Store) GetLatestDetectionResults(nodeKeys []string) (map[string][]DetectionResultView, error) {
	result := make(map[string][]DetectionResultView)
	if len(nodeKeys) == 0 {
		return result, nil
	}

	// 构造 IN 占位符
	placeholders := make([]string, len(nodeKeys))
	args := make([]any, len(nodeKeys))
	for i, k := range nodeKeys {
		placeholders[i] = "?"
		args[i] = k
	}

	// 取每个 (node_key, target_name) 组合最新的一条记录
	query := fmt.Sprintf(`
		SELECT nh.node_key, nh.target_name, nh.available, nh.latency_ms, nh.error, nh.checked_at, nh.down_mbps, nh.up_mbps
		FROM node_health nh
		INNER JOIN (
			SELECT node_key, target_name, MAX(checked_at) AS max_checked
			FROM node_health
			WHERE node_key IN (%s)
			GROUP BY node_key, target_name
		) latest
		ON nh.node_key = latest.node_key
		AND nh.target_name = latest.target_name
		AND nh.checked_at = latest.max_checked
		ORDER BY nh.node_key, nh.target_name
	`, joinPlaceholders(placeholders))

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query latest detection results: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			nodeKey    string
			view       DetectionResultView
			availInt   int
			checkedStr string
		)
		if err := rows.Scan(&nodeKey, &view.TargetName, &availInt, &view.Latency, &view.Error, &checkedStr, &view.DownMbps, &view.UpMbps); err != nil {
			return nil, fmt.Errorf("scan detection result: %w", err)
		}
		view.Available = availInt == 1
		// 时间解析(SQLite 存储格式兼容)
		if t, perr := time.Parse("2006-01-02 15:04:05.999999999-07:00", checkedStr); perr == nil {
			view.CheckedAt = t
		} else if t, perr := time.Parse(time.RFC3339, checkedStr); perr == nil {
			view.CheckedAt = t
		}
		result[nodeKey] = append(result[nodeKey], view)
	}
	return result, rows.Err()
}

// joinPlaceholders 用逗号连接占位符
func joinPlaceholders(placeholders []string) string {
	out := ""
	for i, p := range placeholders {
		if i > 0 {
			out += ","
		}
		out += p
	}
	return out
}

// SaveDetectionResults 批量保存检测结果到 node_health 表
func (s *Store) SaveDetectionResults(results []detection.Result, nodeName, nodeSource string) error {
	if len(results) == 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO node_health (node_key, name, source, target_name, available, latency_ms, checked_at, error, down_mbps, up_mbps)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("prepare statement: %w", err)
	}
	defer stmt.Close()

	now := time.Now()
	for _, r := range results {
		available := 0
		if r.Available {
			available = 1
		}
		_, err := stmt.Exec(
			r.NodeKey,
			nodeName,
			nodeSource,
			r.TargetName,
			available,
			r.Latency,
			now,
			r.Error,
			0.0, // down_mbps (批量检测暂不含带宽)
			0.0, // up_mbps
		)
		if err != nil {
			return fmt.Errorf("insert detection result: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

// SaveTestResult 保存单节点测试结果（quick/real/bandwidth）到 node_health 表
func (s *Store) SaveTestResult(nodeKey, nodeName, nodeSource string, result detection.TestResult) error {
	available := 0
	if result.Available {
		available = 1
	}

	// target_name: quick/real→"connectivity", bandwidth→"bandwidth"
	targetName := "connectivity"
	if result.Mode == "bandwidth" {
		targetName = "bandwidth"
	}

	_, err := s.db.Exec(`
		INSERT INTO node_health (node_key, name, source, target_name, available, latency_ms, checked_at, error, down_mbps, up_mbps)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, nodeKey, nodeName, nodeSource, targetName, available, result.Latency, time.Now(), result.Error, result.DownMbps, result.UpMbps)

	return err
}
