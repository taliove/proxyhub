package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/taliove/proxyhub/internal/detection"
)

// examHistoryRetention 每节点保留的最近体检记录条数,写入后超出即修剪。
const examHistoryRetention = 50

// ExamHistoryEntry 一条深度体检历史记录(报告已从 JSON 解析为结构)。
type ExamHistoryEntry struct {
	ID        int64                `json:"id"`
	NodeKey   string               `json:"node_key"`
	Report    detection.ExamReport `json:"report"`
	CreatedAt time.Time            `json:"created_at"`
}

// SaveExamHistory 追加一条体检历史并修剪该节点至最近 examHistoryRetention 条。
// 只由体检"成功完成"的收口调用(失败/取消不落盘,语义见 handler)。
// report 按设计只含聚合指标,不含节点会话凭证。
func (s *Store) SaveExamHistory(nodeKey string, report detection.ExamReport) error {
	reportJSON, err := json.Marshal(report)
	if err != nil {
		return fmt.Errorf("marshal exam report: %w", err)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin exam history tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(
		`INSERT INTO exam_history (node_key, report_json, created_at) VALUES (?, ?, ?)`,
		nodeKey, string(reportJSON), time.Now(),
	); err != nil {
		return fmt.Errorf("insert exam history: %w", err)
	}

	// 修剪:仅保留该节点最近 examHistoryRetention 条(id 单调递增=写入顺序),其余删除。
	if _, err := tx.Exec(`
		DELETE FROM exam_history
		WHERE node_key = ?
		  AND id NOT IN (
			SELECT id FROM exam_history
			WHERE node_key = ?
			ORDER BY id DESC
			LIMIT ?
		)`, nodeKey, nodeKey, examHistoryRetention); err != nil {
		return fmt.Errorf("trim exam history: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit exam history: %w", err)
	}
	return nil
}

// LatestExamHistory 返回某节点最近一次体检记录;无记录返回 (nil, nil)。
func (s *Store) LatestExamHistory(nodeKey string) (*ExamHistoryEntry, error) {
	var (
		entry      ExamHistoryEntry
		reportJSON string
		createdStr string
	)
	err := s.db.QueryRow(`
		SELECT id, node_key, report_json, created_at
		FROM exam_history
		WHERE node_key = ?
		ORDER BY id DESC
		LIMIT 1`, nodeKey).Scan(&entry.ID, &entry.NodeKey, &reportJSON, &createdStr)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query latest exam history: %w", err)
	}
	if err := json.Unmarshal([]byte(reportJSON), &entry.Report); err != nil {
		return nil, fmt.Errorf("unmarshal exam report: %w", err)
	}
	entry.CreatedAt = parseTimeOrZero(&createdStr)
	return &entry, nil
}

// LatestExamScores 批量取多个节点最近一次体检的稳定性分,返回 map[nodeKey]score。
// 每节点只取最新一条(id 最大);无体检记录、或报告无稳定性段的节点不出现在结果中
// (调用方按"缺省=无分"处理,同 ListNodeTags 的空态约定)。
// 分数 0 是合法的"差"档,保留在结果中(不与"无分"混淆)。
func (s *Store) LatestExamScores(nodeKeys []string) (map[string]int, error) {
	result := make(map[string]int)
	if len(nodeKeys) == 0 {
		return result, nil
	}

	placeholders := make([]string, len(nodeKeys))
	args := make([]any, len(nodeKeys))
	for i, k := range nodeKeys {
		placeholders[i] = "?"
		args[i] = k
	}

	// 子查询取每节点最大 id(=最近一条),外层回连取报告 JSON。
	in := joinPlaceholders(placeholders)
	query := fmt.Sprintf(`
		SELECT node_key, report_json FROM exam_history
		WHERE id IN (
			SELECT MAX(id) FROM exam_history
			WHERE node_key IN (%s)
			GROUP BY node_key
		)`, in)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query latest exam scores: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			nodeKey    string
			reportJSON string
		)
		if err := rows.Scan(&nodeKey, &reportJSON); err != nil {
			return nil, fmt.Errorf("scan latest exam score: %w", err)
		}
		var report detection.ExamReport
		if err := json.Unmarshal([]byte(reportJSON), &report); err != nil {
			return nil, fmt.Errorf("unmarshal exam report: %w", err)
		}
		// 只对含稳定性段的报告透出分数;无该段的节点视为"无分"。
		if report.Stability != nil {
			result[nodeKey] = report.Stability.Score
		}
	}
	return result, rows.Err()
}

// ListExamHistory 返回某节点体检历史(时间倒序);无记录返回空切片。
func (s *Store) ListExamHistory(nodeKey string) ([]ExamHistoryEntry, error) {
	rows, err := s.db.Query(`
		SELECT id, node_key, report_json, created_at
		FROM exam_history
		WHERE node_key = ?
		ORDER BY id DESC`, nodeKey)
	if err != nil {
		return nil, fmt.Errorf("query exam history: %w", err)
	}
	defer rows.Close()

	entries := make([]ExamHistoryEntry, 0)
	for rows.Next() {
		var (
			entry      ExamHistoryEntry
			reportJSON string
			createdStr string
		)
		if err := rows.Scan(&entry.ID, &entry.NodeKey, &reportJSON, &createdStr); err != nil {
			return nil, fmt.Errorf("scan exam history: %w", err)
		}
		if err := json.Unmarshal([]byte(reportJSON), &entry.Report); err != nil {
			return nil, fmt.Errorf("unmarshal exam report: %w", err)
		}
		entry.CreatedAt = parseTimeOrZero(&createdStr)
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}
