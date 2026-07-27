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
	// RefreshStatusCancelled 任务被取消(中断于当前阶段,已拉取部分照常入池)
	RefreshStatusCancelled = "cancelled"
)

// MaxRefreshRuns 刷新历史保留条数，超出后按最旧清理（事件一并删除）
const MaxRefreshRuns = 50

// RefreshRun 一次聚合刷新的记录
type RefreshRun struct {
	ID             int64      `json:"id"`
	Trigger        string     `json:"trigger"`
	// JobID 关联的 jobs 表任务 id(刷新任务化后回填;0 = 任务化前的旧记录或未关联)
	JobID          int64      `json:"job_id"`
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

// CreateRefreshRun 新建一条刷新记录，并在同一事务内清理超限的旧记录。
// jobID 为关联的 jobs 任务 id(刷新任务化),无关联传 0。
func (s *Store) CreateRefreshRun(trigger string, jobID int64) (*RefreshRun, error) {
	return s.CreateRefreshRunForUser(0, trigger, jobID)
}

// CreateRefreshRunForUser 与 CreateRefreshRun 同语义,但记录属主 user_id(多租户):
// 刷新历史按用户隔离,0 = 全局(定时/启动刷新,聚合全部机场)。
func (s *Store) CreateRefreshRunForUser(userID int64, trigger string, jobID int64) (*RefreshRun, error) {
	now := time.Now()
	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin refresh run tx: %w", err)
	}
	defer tx.Rollback()

	result, err := tx.Exec(
		`INSERT INTO refresh_runs (trigger_type, status, job_id, started_at, user_id) VALUES (?, ?, ?, ?, ?)`,
		trigger, RefreshStatusRunning, jobID, now, userID)
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
		JobID:     jobID,
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
	return s.GetRefreshRunByUser(0, id)
}

// GetRefreshRunByUser 与 GetRefreshRun 同语义,但按属主过滤(多租户):
// userID>0 时行属他人返回 ErrNotFound,不暴露存在性;0 = 不过滤(超管全局视角)。
func (s *Store) GetRefreshRunByUser(userID, id int64) (*RefreshRun, error) {
	query := `SELECT id, trigger_type, status, total_nodes, available_nodes, final_nodes, error, started_at, finished_at, job_id
		 FROM refresh_runs WHERE id = ?`
	args := []any{id}
	if userID > 0 {
		query += ` AND user_id = ?`
		args = append(args, userID)
	}
	row := s.db.QueryRow(query, args...)
	run, err := scanRefreshRun(row)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query refresh run: %w", err)
	}
	return run, nil
}

// GetRefreshRunByJobID 按 jobs 任务 id 反查关联的刷新记录(ticket 0022 任务结果端点);
// 无关联记录返回 (nil, nil)。
func (s *Store) GetRefreshRunByJobID(jobID int64) (*RefreshRun, error) {
	row := s.db.QueryRow(
		`SELECT id, trigger_type, status, total_nodes, available_nodes, final_nodes, error, started_at, finished_at, job_id
		 FROM refresh_runs WHERE job_id = ? ORDER BY id DESC LIMIT 1`, jobID)
	run, err := scanRefreshRun(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query refresh run by job: %w", err)
	}
	return run, nil
}

// ListRefreshRuns 按时间倒序列出刷新记录
func (s *Store) ListRefreshRuns(limit int) ([]*RefreshRun, error) {
	return s.ListRefreshRunsByUser(0, limit)
}

// ListRefreshRunsByUser 与 ListRefreshRuns 同语义,但按属主过滤(多租户):
// userID>0 只列该用户的刷新记录;0 = 全量(超管全局视角)。
func (s *Store) ListRefreshRunsByUser(userID int64, limit int) ([]*RefreshRun, error) {
	query := `SELECT id, trigger_type, status, total_nodes, available_nodes, final_nodes, error, started_at, finished_at, job_id
		 FROM refresh_runs`
	args := []any{}
	if userID > 0 {
		query += ` WHERE user_id = ?`
		args = append(args, userID)
	}
	query += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.Query(query, args...)
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

// RefreshFetchDiag 一次刷新中单个机场的结构化拉取诊断(ticket 0018)。
// 口径与机场测试 RunDiagnostic 对齐;拉取失败时 Error 非空、HTTPStatus 可能为 0(网络错误)。
type RefreshFetchDiag struct {
	ID            int64     `json:"id"`
	RunID         int64     `json:"run_id"`
	Airport       string    `json:"airport"`
	AirportID     int64     `json:"airport_id"`
	HTTPStatus    int       `json:"http_status"`
	DurationMs    int64     `json:"duration_ms"`
	NodeCount     int       `json:"node_count"`
	ParseFailures int       `json:"parse_failures"`
	Error         string    `json:"error"`
	CreatedAt     time.Time `json:"created_at"`
}

// InsertRefreshFetchDiag 写入一条机场拉取诊断
func (s *Store) InsertRefreshFetchDiag(d *RefreshFetchDiag) error {
	res, err := s.db.Exec(
		`INSERT INTO refresh_fetch_diags
		 (run_id, airport, airport_id, http_status, duration_ms, node_count, parse_failures, error, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d.RunID, d.Airport, d.AirportID, d.HTTPStatus, d.DurationMs, d.NodeCount, d.ParseFailures, d.Error, time.Now())
	if err != nil {
		return fmt.Errorf("insert refresh fetch diag: %w", err)
	}
	d.ID, _ = res.LastInsertId()
	return nil
}

// ListRefreshFetchDiags 按机场列表顺序(写入序)列出某次刷新的全部拉取诊断
func (s *Store) ListRefreshFetchDiags(runID int64) ([]*RefreshFetchDiag, error) {
	rows, err := s.db.Query(
		`SELECT id, run_id, airport, airport_id, http_status, duration_ms, node_count, parse_failures, error, created_at
		 FROM refresh_fetch_diags WHERE run_id = ? ORDER BY id ASC`, runID)
	if err != nil {
		return nil, fmt.Errorf("query refresh fetch diags: %w", err)
	}
	defer rows.Close()

	var diags []*RefreshFetchDiag
	for rows.Next() {
		var d RefreshFetchDiag
		if err := rows.Scan(&d.ID, &d.RunID, &d.Airport, &d.AirportID,
			&d.HTTPStatus, &d.DurationMs, &d.NodeCount, &d.ParseFailures, &d.Error, &d.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan refresh fetch diag: %w", err)
		}
		diags = append(diags, &d)
	}
	return diags, rows.Err()
}

// cleanupRefreshRuns 在事务内删除超出保留上限的旧记录及其事件
func cleanupRefreshRuns(tx *sql.Tx) error {
	if _, err := tx.Exec(
		`DELETE FROM refresh_events WHERE run_id NOT IN
		 (SELECT id FROM refresh_runs ORDER BY id DESC LIMIT ?)`, MaxRefreshRuns); err != nil {
		return err
	}
	if _, err := tx.Exec(
		`DELETE FROM refresh_fetch_diags WHERE run_id NOT IN
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
		&run.Error, &run.StartedAt, &finishedAt, &run.JobID); err != nil {
		return nil, err
	}
	if finishedAt.Valid {
		run.FinishedAt = &finishedAt.Time
	}
	return &run, nil
}

// FailRunningRefreshRuns 把仍处于 running 的刷新记录标记为失败。
// 进程启动时调用:任何 running 行都是上一个已死进程的残留(本进程还没开始跑),
// 不清理会永久卡在 running 展示。
func (s *Store) FailRunningRefreshRuns(errMsg string) error {
	_, err := s.db.Exec(
		`UPDATE refresh_runs SET status = ?, error = ?, finished_at = ? WHERE status = ?`,
		RefreshStatusFailed, errMsg, time.Now(), RefreshStatusRunning)
	if err != nil {
		return fmt.Errorf("fail running refresh runs: %w", err)
	}
	return nil
}
