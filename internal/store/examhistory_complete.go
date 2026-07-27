package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/taliove/proxyhub/internal/detection"
)

// 完整体检口径查询:排除"出网+稳定性"任务(source=stability_check)的缺段报告。
// exam_history 是"最近体检"单一事实源(节点列表稳定性分/总分/体检历史时间线),
// 缺段报告落库后不得抢占这些消费方的"最近"位置;但它们仍是新鲜数据,
// 标签派生等消费方继续走不过滤的 LatestExamHistory。
//
// 本文件为纯新增:来源标记存于 report_json 内(ExamReport.source),无数据库迁移;
// 既有查询函数零改动,降低并行开发的合并冲突面。

// completeExamFilter 完整体检口径 SQL 过滤片段:report_json 无 source 字段
// (历史记录与完整体检)或 source 非 stability_check 即通过。参数位 ? 绑
// detection.ExamSourceStabilityCheck。
const completeExamFilter = `COALESCE(json_extract(report_json, '$.source'), '') <> ?`

// LatestCompleteExamHistory 返回某节点最近一次完整体检口径记录(排除 stability_check);
// 无记录返回 (nil, nil)。
func (s *Store) LatestCompleteExamHistory(nodeKey string) (*ExamHistoryEntry, error) {
	return s.LatestCompleteExamHistoryForUser(0, nodeKey)
}

// LatestCompleteExamHistoryForUser 同 LatestCompleteExamHistory,但按属主过滤(多租户):
// userID>0 只查该用户名下历史(查不到返回 (nil, nil),与他无此节点/无历史同态,
// 不暴露存在性);0 = 全量(超管跨用户视角或旧单用户语义)。
func (s *Store) LatestCompleteExamHistoryForUser(userID int64, nodeKey string) (*ExamHistoryEntry, error) {
	var (
		entry      ExamHistoryEntry
		reportJSON string
		createdStr string
	)
	query := `
		SELECT id, node_key, report_json, created_at
		FROM exam_history
		WHERE node_key = ? AND ` + completeExamFilter
	args := []any{nodeKey, detection.ExamSourceStabilityCheck}
	if userID > 0 {
		query += ` AND user_id = ?`
		args = append(args, userID)
	}
	query += `
		ORDER BY id DESC
		LIMIT 1`
	err := s.db.QueryRow(query, args...).Scan(&entry.ID, &entry.NodeKey, &reportJSON, &createdStr)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query latest complete exam history: %w", err)
	}
	if err := json.Unmarshal([]byte(reportJSON), &entry.Report); err != nil {
		return nil, fmt.Errorf("unmarshal exam report: %w", err)
	}
	entry.CreatedAt = parseTimeOrZero(&createdStr)
	return &entry, nil
}

// LatestCompleteExamScores 批量取多个节点最近完整体检口径的稳定性分,返回 map[nodeKey]score。
// 语义同 LatestExamScores,但每节点的"最近一条"在完整体检口径集合内取:
// stability_check 报告的新鲜分不抢占节点列表 stability_score。
func (s *Store) LatestCompleteExamScores(nodeKeys []string) (map[string]int, error) {
	result := make(map[string]int)
	if len(nodeKeys) == 0 {
		return result, nil
	}

	placeholders := make([]string, len(nodeKeys))
	args := make([]any, 0, len(nodeKeys)+2)
	for i, k := range nodeKeys {
		placeholders[i] = "?"
		args = append(args, k)
	}

	// 子查询在完整体检口径集合内取每节点最大 id(=最近一条完整记录),外层回连取报告 JSON。
	in := joinPlaceholders(placeholders)
	query := fmt.Sprintf(`
		SELECT node_key, report_json FROM exam_history
		WHERE id IN (
			SELECT MAX(id) FROM exam_history
			WHERE node_key IN (%s) AND %s
			GROUP BY node_key
		)`, in, completeExamFilter)
	args = append(args, detection.ExamSourceStabilityCheck)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query latest complete exam scores: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			nodeKey    string
			reportJSON string
		)
		if err := rows.Scan(&nodeKey, &reportJSON); err != nil {
			return nil, fmt.Errorf("scan latest complete exam score: %w", err)
		}
		var report detection.ExamReport
		if err := json.Unmarshal([]byte(reportJSON), &report); err != nil {
			return nil, fmt.Errorf("unmarshal exam report: %w", err)
		}
		if report.Stability != nil {
			result[nodeKey] = report.Stability.Score
		}
	}
	return result, rows.Err()
}

// LatestCompleteExamReports 批量取多个节点最近完整体检口径记录,返回 map[nodeKey]entry。
// 语义同 LatestExamReports,但"最近一条"在完整体检口径集合内取(总分聚合消费方)。
func (s *Store) LatestCompleteExamReports(nodeKeys []string) (map[string]ExamHistoryEntry, error) {
	result := make(map[string]ExamHistoryEntry)
	if len(nodeKeys) == 0 {
		return result, nil
	}

	placeholders := make([]string, len(nodeKeys))
	args := make([]any, 0, len(nodeKeys)+2)
	for i, k := range nodeKeys {
		placeholders[i] = "?"
		args = append(args, k)
	}

	in := joinPlaceholders(placeholders)
	query := fmt.Sprintf(`
		SELECT id, node_key, report_json, created_at FROM exam_history
		WHERE id IN (
			SELECT MAX(id) FROM exam_history
			WHERE node_key IN (%s) AND %s
			GROUP BY node_key
		)`, in, completeExamFilter)
	args = append(args, detection.ExamSourceStabilityCheck)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query latest complete exam reports: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			entry      ExamHistoryEntry
			reportJSON string
			createdStr string
		)
		if err := rows.Scan(&entry.ID, &entry.NodeKey, &reportJSON, &createdStr); err != nil {
			return nil, fmt.Errorf("scan latest complete exam report: %w", err)
		}
		if err := json.Unmarshal([]byte(reportJSON), &entry.Report); err != nil {
			return nil, fmt.Errorf("unmarshal exam report: %w", err)
		}
		entry.CreatedAt = parseTimeOrZero(&createdStr)
		result[entry.NodeKey] = entry
	}
	return result, rows.Err()
}

// ListCompleteExamHistory 返回某节点完整体检口径的历史时间线(时间倒序),
// stability_check 条目被过滤;无记录返回空切片。
func (s *Store) ListCompleteExamHistory(nodeKey string) ([]ExamHistoryEntry, error) {
	return s.ListCompleteExamHistoryForUser(0, nodeKey)
}

// ListCompleteExamHistoryForUser 同 ListCompleteExamHistory,但按属主过滤(多租户);
// userID<=0 返回全量(超管跨用户视角或旧单用户语义)。
func (s *Store) ListCompleteExamHistoryForUser(userID int64, nodeKey string) ([]ExamHistoryEntry, error) {
	query := `
		SELECT id, node_key, report_json, created_at
		FROM exam_history
		WHERE node_key = ? AND ` + completeExamFilter
	args := []any{nodeKey, detection.ExamSourceStabilityCheck}
	if userID > 0 {
		query += ` AND user_id = ?`
		args = append(args, userID)
	}
	query += `
		ORDER BY id DESC`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query complete exam history: %w", err)
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
			return nil, fmt.Errorf("scan complete exam history: %w", err)
		}
		if err := json.Unmarshal([]byte(reportJSON), &entry.Report); err != nil {
			return nil, fmt.Errorf("unmarshal exam report: %w", err)
		}
		entry.CreatedAt = parseTimeOrZero(&createdStr)
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}
