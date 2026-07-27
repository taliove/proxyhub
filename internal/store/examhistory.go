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
	// JobID 产出本记录的 jobs 任务 id(ticket 0022;0 = 任务结果关联前的旧数据)。
	JobID     int64                `json:"job_id"`
	CreatedAt time.Time            `json:"created_at"`
}

// SaveExamHistory 追加一条体检历史并修剪该节点至最近 examHistoryRetention 条。
// 只由体检"成功完成"的收口调用(失败/取消不落盘,语义见 handler)。
// report 按设计只含聚合指标,不含节点会话凭证。
// 不关联任务(job_id=0);任务链路的写入用 SaveExamHistoryWithJob。
func (s *Store) SaveExamHistory(nodeKey string, report detection.ExamReport) error {
	return s.SaveExamHistoryWithJob(nodeKey, report, 0)
}

// SaveExamHistoryWithJob 同 SaveExamHistory,但记录产出它的 jobs 任务 id
// (ticket 0022 任务结果关联;jobID=0 表示未关联,与旧数据同口径)。
// 等价于 SaveExamHistoryWithJobForUser(0, ...)(未归属桶,旧语义)。
func (s *Store) SaveExamHistoryWithJob(nodeKey string, report detection.ExamReport, jobID int64) error {
	return s.SaveExamHistoryWithJobForUser(0, nodeKey, report, jobID)
}

// SaveExamHistoryWithJobForUser 同 SaveExamHistoryWithJob,但记录属主 user_id(多租户):
// 历史按 (user_id, node_key) 分桶,读侧按属主过滤;修剪也按属主分桶互不占额。
func (s *Store) SaveExamHistoryWithJobForUser(userID int64, nodeKey string, report detection.ExamReport, jobID int64) error {
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
		`INSERT INTO exam_history (node_key, report_json, job_id, created_at, user_id) VALUES (?, ?, ?, ?, ?)`,
		nodeKey, string(reportJSON), jobID, time.Now(), userID,
	); err != nil {
		return fmt.Errorf("insert exam history: %w", err)
	}

	// 修剪:仅保留该桶(属主+节点)最近 examHistoryRetention 条(id 单调递增=写入顺序),其余删除。
	if _, err := tx.Exec(`
		DELETE FROM exam_history
		WHERE user_id = ? AND node_key = ?
		  AND id NOT IN (
			SELECT id FROM exam_history
			WHERE user_id = ? AND node_key = ?
			ORDER BY id DESC
			LIMIT ?
		)`, userID, nodeKey, userID, nodeKey, examHistoryRetention); err != nil {
		return fmt.Errorf("trim exam history: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit exam history: %w", err)
	}
	return nil
}

// examHistoryColumns 体检历史查询的统一列清单(所有读路径一致,配合 scanExamHistory)。
const examHistoryColumns = `id, node_key, report_json, job_id, created_at`

// scanExamHistory 从一行扫描出 ExamHistoryEntry(供 QueryRow/Rows 复用)。
func scanExamHistory(row rowScanner) (*ExamHistoryEntry, error) {
	var (
		entry      ExamHistoryEntry
		reportJSON string
		createdStr string
	)
	if err := row.Scan(&entry.ID, &entry.NodeKey, &reportJSON, &entry.JobID, &createdStr); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(reportJSON), &entry.Report); err != nil {
		return nil, fmt.Errorf("unmarshal exam report: %w", err)
	}
	entry.CreatedAt = parseTimeOrZero(&createdStr)
	return &entry, nil
}

// LatestExamHistory 返回某节点最近一次体检记录;无记录返回 (nil, nil)。
func (s *Store) LatestExamHistory(nodeKey string) (*ExamHistoryEntry, error) {
	entry, err := scanExamHistory(s.db.QueryRow(`
		SELECT `+examHistoryColumns+`
		FROM exam_history
		WHERE node_key = ?
		ORDER BY id DESC
		LIMIT 1`, nodeKey))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query latest exam history: %w", err)
	}
	return entry, nil
}

// ExamHistoryByJob 返回某节点由指定 jobs 任务产出的最新一条体检记录(ticket 0022
// 任务结果精确关联);jobID<=0 或无匹配返回 (nil, nil)。
func (s *Store) ExamHistoryByJob(nodeKey string, jobID int64) (*ExamHistoryEntry, error) {
	if jobID <= 0 {
		return nil, nil
	}
	entry, err := scanExamHistory(s.db.QueryRow(`
		SELECT `+examHistoryColumns+`
		FROM exam_history
		WHERE node_key = ? AND job_id = ?
		ORDER BY id DESC
		LIMIT 1`, nodeKey, jobID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query exam history by job: %w", err)
	}
	return entry, nil
}

// LatestExamHistoryInWindow 返回某节点在时间窗 [start, end](闭区间)内最新一条
// "旧数据"(job_id=0)体检记录;ticket 0022 时间窗回退用——只匹配任务结果关联前
// 的口径,带 job_id 的新数据不参与回退(避免把并发任务的产出误归本任务)。
// 窗口内无记录返回 (nil, nil)。
func (s *Store) LatestExamHistoryInWindow(nodeKey string, start, end time.Time) (*ExamHistoryEntry, error) {
	entry, err := scanExamHistory(s.db.QueryRow(`
		SELECT `+examHistoryColumns+`
		FROM exam_history
		WHERE node_key = ? AND job_id = 0 AND created_at >= ? AND created_at <= ?
		ORDER BY id DESC
		LIMIT 1`, nodeKey, start, end))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query exam history in window: %w", err)
	}
	return entry, nil
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

// LatestExamReports 批量取多个节点最近一次体检记录,返回 map[nodeKey]entry。
// 每节点只取最新一条(id 最大);无体检记录的节点不出现在结果中
// (调用方按"缺省=未体检"处理,同 LatestExamScores 的空态约定)。
// 与 LatestExamScores 的区别:本方法返回 report 本体(含无稳定性段的报告),
// 供管理面聚合接口透传给前端算分,分数提取与否由调用方决定。
func (s *Store) LatestExamReports(nodeKeys []string) (map[string]ExamHistoryEntry, error) {
	result := make(map[string]ExamHistoryEntry)
	if len(nodeKeys) == 0 {
		return result, nil
	}

	placeholders := make([]string, len(nodeKeys))
	args := make([]any, len(nodeKeys))
	for i, k := range nodeKeys {
		placeholders[i] = "?"
		args[i] = k
	}

	// 子查询取每节点最大 id(=最近一条),外层回连取报告 JSON(同 LatestExamScores)。
	in := joinPlaceholders(placeholders)
	query := fmt.Sprintf(`
		SELECT `+examHistoryColumns+` FROM exam_history
		WHERE id IN (
			SELECT MAX(id) FROM exam_history
			WHERE node_key IN (%s)
			GROUP BY node_key
		)`, in)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query latest exam reports: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		entry, err := scanExamHistory(rows)
		if err != nil {
			return nil, fmt.Errorf("scan latest exam report: %w", err)
		}
		result[entry.NodeKey] = *entry
	}
	return result, rows.Err()
}

// ListExamHistory 返回某节点体检历史(时间倒序);无记录返回空切片。
func (s *Store) ListExamHistory(nodeKey string) ([]ExamHistoryEntry, error) {
	rows, err := s.db.Query(`
		SELECT `+examHistoryColumns+`
		FROM exam_history
		WHERE node_key = ?
		ORDER BY id DESC`, nodeKey)
	if err != nil {
		return nil, fmt.Errorf("query exam history: %w", err)
	}
	defer rows.Close()

	entries := make([]ExamHistoryEntry, 0)
	for rows.Next() {
		entry, err := scanExamHistory(rows)
		if err != nil {
			return nil, fmt.Errorf("scan exam history: %w", err)
		}
		entries = append(entries, *entry)
	}
	return entries, rows.Err()
}
