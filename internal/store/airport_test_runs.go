package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// AirportTestRun represents a test run record.
type AirportTestRun struct {
	ID             int64     `json:"id"`
	AirportID      int64     `json:"airport_id"`
	CreatedAt      time.Time `json:"created_at"`
	SampleParams   string    `json:"sample_params"`
	IsFull         bool      `json:"is_full"`
	Status         string    `json:"status"`
	OverallScore   *float64  `json:"overall_score,omitempty"`
	DimensionsJSON string    `json:"dimensions_json"`
	ErrorMessage   string    `json:"error_message,omitempty"`
	// JobID 关联的 jobs 表任务 id(机场测试迁入 jobs 运行时后回填;
	// 0 = 任务化前的旧记录或未关联,对齐 refresh_runs.job_id 口径)
	JobID int64 `json:"job_id"`
}

// CreateAirportTestRun inserts a new test run.
func (s *Store) CreateAirportTestRun(ctx context.Context, run *AirportTestRun) (int64, error) {
	// Use explicit UTC format for created_at (see ADR 0010 re: modernc.org/sqlite datetime compatibility)
	createdStr := run.CreatedAt.UTC().Format("2006-01-02 15:04:05")

	result, err := s.db.ExecContext(ctx,
		`INSERT INTO airport_test_runs
		(airport_id, created_at, sample_params, is_full, status, overall_score, dimensions_json, error_message, job_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.AirportID, createdStr, run.SampleParams, boolToInt(run.IsFull),
		run.Status, run.OverallScore, run.DimensionsJSON, run.ErrorMessage, run.JobID)
	if err != nil {
		return 0, fmt.Errorf("insert test run: %w", err)
	}

	id, _ := result.LastInsertId()
	return id, nil
}

// GetAirportTestRun retrieves a test run by airport ID and run ID.
func (s *Store) GetAirportTestRun(ctx context.Context, airportID, runID int64) (*AirportTestRun, error) {
	var run AirportTestRun
	var isFull int
	err := s.db.QueryRowContext(ctx,
		`SELECT id, airport_id, created_at, sample_params, is_full, status, overall_score, dimensions_json, error_message, job_id
		FROM airport_test_runs
		WHERE airport_id = ? AND id = ?`,
		airportID, runID).Scan(
		&run.ID, &run.AirportID, &run.CreatedAt, &run.SampleParams, &isFull,
		&run.Status, &run.OverallScore, &run.DimensionsJSON, &run.ErrorMessage, &run.JobID)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query test run: %w", err)
	}
	run.IsFull = isFull == 1
	return &run, nil
}

// UpdateAirportTestRun updates an existing test run.
// sample_params 一并落库:检活进度镜像(checked/total)由编排层反复更新,
// 此前 UPDATE 不含该列导致进度镜像被静默丢弃(jobs cursor 是主进度源,本列为镜像)。
func (s *Store) UpdateAirportTestRun(ctx context.Context, run *AirportTestRun) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE airport_test_runs
		SET status = ?, overall_score = ?, dimensions_json = ?, error_message = ?, sample_params = ?
		WHERE id = ?`,
		run.Status, run.OverallScore, run.DimensionsJSON, run.ErrorMessage, run.SampleParams, run.ID)
	if err != nil {
		return fmt.Errorf("update test run: %w", err)
	}
	return nil
}

// GetAirportTestRunByJobID 按 jobs 任务 id 反查关联的测试记录(任务结果端点,
// 对齐 GetRefreshRunByJobID);无关联记录返回 (nil, nil)。
func (s *Store) GetAirportTestRunByJobID(jobID int64) (*AirportTestRun, error) {
	var run AirportTestRun
	var isFull int
	err := s.db.QueryRow(
		`SELECT id, airport_id, created_at, sample_params, is_full, status, overall_score, dimensions_json, error_message, job_id
		FROM airport_test_runs
		WHERE job_id = ? ORDER BY id DESC LIMIT 1`, jobID).Scan(
		&run.ID, &run.AirportID, &run.CreatedAt, &run.SampleParams, &isFull,
		&run.Status, &run.OverallScore, &run.DimensionsJSON, &run.ErrorMessage, &run.JobID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query test run by job: %w", err)
	}
	run.IsFull = isFull == 1
	return &run, nil
}

// ListAirportTestRuns retrieves recent test runs for an airport (descending order, limited).
func (s *Store) ListAirportTestRuns(ctx context.Context, airportID int64, limit int) ([]*AirportTestRun, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, airport_id, created_at, sample_params, is_full, status, overall_score, dimensions_json, error_message, job_id
		FROM airport_test_runs
		WHERE airport_id = ?
		ORDER BY id DESC
		LIMIT ?`,
		airportID, limit)
	if err != nil {
		return nil, fmt.Errorf("query test runs: %w", err)
	}
	defer rows.Close()

	var runs []*AirportTestRun
	for rows.Next() {
		var run AirportTestRun
		var isFull int
		if err := rows.Scan(
			&run.ID, &run.AirportID, &run.CreatedAt, &run.SampleParams, &isFull,
			&run.Status, &run.OverallScore, &run.DimensionsJSON, &run.ErrorMessage, &run.JobID); err != nil {
			return nil, fmt.Errorf("scan test run: %w", err)
		}
		run.IsFull = isFull == 1
		runs = append(runs, &run)
	}
	return runs, nil
}

// PruneAirportTestRuns deletes test runs older than specified time (90-day retention).
func (s *Store) PruneAirportTestRuns(olderThan time.Time) error {
	cutoff := olderThan.UTC().Format("2006-01-02 15:04:05")
	_, err := s.db.ExecContext(context.Background(),
		`DELETE FROM airport_test_runs WHERE datetime(created_at) < datetime(?)`, cutoff)
	if err != nil {
		return fmt.Errorf("prune airport test runs: %w", err)
	}
	return nil
}

// 机场测试 run 状态(store 层裸字符串,与 internal/airporttest RunStatus 对齐;
// 不 import airporttest 避免循环依赖:airporttest 已依赖 store)。
const (
	AirportTestStatusDiagnosing = "diagnosing"
	AirportTestStatusChecking   = "checking"
	AirportTestStatusScoring    = "scoring"
	AirportTestStatusFailed     = "failed"
	// AirportTestStatusCancelled 任务化(issue 0025)后被显式取消的 run 终态,
	// 对齐 jobs.StatusCancelled 与 refresh_runs 的 cancelled 口径。
	AirportTestStatusCancelled = "cancelled"
)

// FailRunningAirportTestRuns 把仍处于进行态(diagnosing/checking/scoring)的
// 测试记录标记为 failed。进程启动时调用:任何进行态行都是上一个已死进程的残留
// (本进程还没开始跑),不清理会永久卡在"进行中"展示。
// 状态值取 failed 而非 interrupted,对齐 FailRunningRefreshRuns 的既有口径。
func (s *Store) FailRunningAirportTestRuns(errMsg string) error {
	_, err := s.db.Exec(
		`UPDATE airport_test_runs SET status = ?, error_message = ? WHERE status IN (?, ?, ?)`,
		AirportTestStatusFailed, errMsg,
		AirportTestStatusDiagnosing, AirportTestStatusChecking, AirportTestStatusScoring)
	if err != nil {
		return fmt.Errorf("fail running airport test runs: %w", err)
	}
	return nil
}
