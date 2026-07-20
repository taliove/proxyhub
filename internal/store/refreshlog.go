package store

import (
	"database/sql"
	"fmt"
	"time"
)

// 刷新触发方式
const (
	RefreshTriggerManual    = "manual"
	RefreshTriggerScheduled = "scheduled"
	RefreshTriggerStartup   = "startup"
)

// 刷新状态
const (
	RefreshStatusRunning = "running"
	RefreshStatusSuccess = "success"
	RefreshStatusPartial = "partial"
	RefreshStatusFailed  = "failed"
)

// MaxRefreshRuns 刷新历史保留条数，超出后按最旧清理（事件一并删除）
const MaxRefreshRuns = 50

// RefreshRun 一次聚合刷新的记录
type RefreshRun struct {
	ID             int64      `json:"id"`
	Trigger        string     `json:"trigger"`
	Status         string     `json:"status"`
	TotalNodes     int        `json:"total_nodes"`
	AvailableNodes int        `json:"available_nodes"`
	FinalNodes     int        `json:"final_nodes"`
	Error          string     `json:"error"`
	StartedAt      time.Time  `json:"started_at"`
	FinishedAt     *time.Time `json:"finished_at"`
}

// RefreshEvent 刷新过程中的一条结构化事件
type RefreshEvent struct {
	ID        int64     `json:"id"`
	RunID     int64     `json:"run_id"`
	Level     string    `json:"level"`
	Stage     string    `json:"stage"`
	Message   string    `json:"message"`
	Data      string    `json:"data"`
	CreatedAt time.Time `json:"created_at"`
}

// CreateRefreshRun 新建一条刷新记录，并在同一事务内清理超限的旧记录
func (s *Store) CreateRefreshRun(trigger string) (*RefreshRun, error) {
	now := time.Now()
	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin refresh run tx: %w", err)
	}
	defer tx.Rollback()

	result, err := tx.Exec(
		`INSERT INTO refresh_runs (trigger_type, status, started_at) VALUES (?, ?, ?)`,
		trigger, RefreshStatusRunning, now)
	if err != nil {
		return nil, fmt.Errorf("insert refresh run: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("get refresh run id: %w", err)
	}

	if err := cleanupRefreshRuns(tx); err != nil {
		return nil, fmt.Errorf("cleanup refresh runs: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit refresh run: %w", err)
	}

	return &RefreshRun{
		ID:        id,
		Trigger:   trigger,
		Status:    RefreshStatusRunning,
		StartedAt: now,
	}, nil
}

// FinishRefreshRun 标记刷新完成并写入汇总
func (s *Store) FinishRefreshRun(id int64, status string, total, available, final int, errMsg string) error {
	_, err := s.db.Exec(
		`UPDATE refresh_runs
		 SET status = ?, total_nodes = ?, available_nodes = ?, final_nodes = ?, error = ?, finished_at = ?
		 WHERE id = ?`,
		status, total, available, final, errMsg, time.Now(), id)
	if err != nil {
		return fmt.Errorf("finish refresh run: %w", err)
	}
	return nil
}

// GetRefreshRun 获取单条刷新记录
func (s *Store) GetRefreshRun(id int64) (*RefreshRun, error) {
	row := s.db.QueryRow(
		`SELECT id, trigger_type, status, total_nodes, available_nodes, final_nodes, error, started_at, finished_at
		 FROM refresh_runs WHERE id = ?`, id)
	run, err := scanRefreshRun(row)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query refresh run: %w", err)
	}
	return run, nil
}

// ListRefreshRuns 按时间倒序列出刷新记录
func (s *Store) ListRefreshRuns(limit int) ([]*RefreshRun, error) {
	rows, err := s.db.Query(
		`SELECT id, trigger_type, status, total_nodes, available_nodes, final_nodes, error, started_at, finished_at
		 FROM refresh_runs ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("query refresh runs: %w", err)
	}
	defer rows.Close()

	var runs []*RefreshRun
	for rows.Next() {
		run, err := scanRefreshRun(rows)
		if err != nil {
			return nil, fmt.Errorf("scan refresh run: %w", err)
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

// AppendRefreshEvent 追加一条刷新事件
func (s *Store) AppendRefreshEvent(runID int64, level, stage, message, data string) error {
	_, err := s.db.Exec(
		`INSERT INTO refresh_events (run_id, level, stage, message, data, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		runID, level, stage, message, data, time.Now())
	if err != nil {
		return fmt.Errorf("insert refresh event: %w", err)
	}
	return nil
}

// ListRefreshEvents 按写入顺序列出某次刷新的全部事件
func (s *Store) ListRefreshEvents(runID int64) ([]*RefreshEvent, error) {
	rows, err := s.db.Query(
		`SELECT id, run_id, level, stage, message, data, created_at
		 FROM refresh_events WHERE run_id = ? ORDER BY id ASC`, runID)
	if err != nil {
		return nil, fmt.Errorf("query refresh events: %w", err)
	}
	defer rows.Close()

	var events []*RefreshEvent
	for rows.Next() {
		var e RefreshEvent
		if err := rows.Scan(&e.ID, &e.RunID, &e.Level, &e.Stage, &e.Message, &e.Data, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan refresh event: %w", err)
		}
		events = append(events, &e)
	}
	return events, rows.Err()
}

// cleanupRefreshRuns 在事务内删除超出保留上限的旧记录及其事件
func cleanupRefreshRuns(tx *sql.Tx) error {
	if _, err := tx.Exec(
		`DELETE FROM refresh_events WHERE run_id NOT IN
		 (SELECT id FROM refresh_runs ORDER BY id DESC LIMIT ?)`, MaxRefreshRuns); err != nil {
		return err
	}
	if _, err := tx.Exec(
		`DELETE FROM refresh_runs WHERE id NOT IN
		 (SELECT id FROM refresh_runs ORDER BY id DESC LIMIT ?)`, MaxRefreshRuns); err != nil {
		return err
	}
	return nil
}

func scanRefreshRun(row rowScanner) (*RefreshRun, error) {
	var run RefreshRun
	var finishedAt sql.NullTime
	if err := row.Scan(&run.ID, &run.Trigger, &run.Status,
		&run.TotalNodes, &run.AvailableNodes, &run.FinalNodes,
		&run.Error, &run.StartedAt, &finishedAt); err != nil {
		return nil, err
	}
	if finishedAt.Valid {
		run.FinishedAt = &finishedAt.Time
	}
	return &run, nil
}
