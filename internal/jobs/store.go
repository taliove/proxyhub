package jobs

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// Store 是 jobs 表的 CRUD + 游标更新。它复用 internal/store 拥有的同一 *sql.DB
// (单写者连接),表结构由 internal/store 的迁移链路建立(013_jobs.sql)。
type Store struct {
	db *sql.DB
}

// NewStore 基于共享的数据库连接构造 jobs 存储。
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// Record 一条 jobs 表记录。
type Record struct {
	ID        int64
	Kind      string
	Key       string
	Params    json.RawMessage
	Status    Status
	Cursor    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Insert 追加一条 running 任务记录,返回自增 id。params 为空时存 'null'。
func (s *Store) Insert(kind, key string, params json.RawMessage) (int64, error) {
	if len(params) == 0 {
		params = json.RawMessage("null")
	}
	now := time.Now()
	res, err := s.db.Exec(
		`INSERT INTO jobs (kind, key, params_json, status, cursor, created_at, updated_at)
		 VALUES (?, ?, ?, ?, '', ?, ?)`,
		kind, key, string(params), string(StatusRunning), now, now,
	)
	if err != nil {
		return 0, fmt.Errorf("insert job: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("insert job id: %w", err)
	}
	return id, nil
}

// UpdateCursor 更新任务续跑游标(每推进一项即调用)。
func (s *Store) UpdateCursor(id int64, cursor string) error {
	if _, err := s.db.Exec(
		`UPDATE jobs SET cursor = ?, updated_at = ? WHERE id = ?`,
		cursor, time.Now(), id,
	); err != nil {
		return fmt.Errorf("update job cursor: %w", err)
	}
	return nil
}

// Finish 落任务终态(done/failed/cancelled/interrupted)。
func (s *Store) Finish(id int64, status Status) error {
	if _, err := s.db.Exec(
		`UPDATE jobs SET status = ?, updated_at = ? WHERE id = ?`,
		string(status), time.Now(), id,
	); err != nil {
		return fmt.Errorf("finish job: %w", err)
	}
	return nil
}

// LoadRunning 加载所有仍处于 running 的任务(重启恢复用),按 id 升序。
func (s *Store) LoadRunning() ([]Record, error) {
	rows, err := s.db.Query(
		`SELECT id, kind, key, params_json, status, cursor, created_at, updated_at
		 FROM jobs WHERE status = ? ORDER BY id ASC`,
		string(StatusRunning),
	)
	if err != nil {
		return nil, fmt.Errorf("query running jobs: %w", err)
	}
	defer rows.Close()

	records := make([]Record, 0)
	for rows.Next() {
		var (
			rec        Record
			params     string
			status     string
			createdStr string
			updatedStr string
		)
		if err := rows.Scan(&rec.ID, &rec.Kind, &rec.Key, &params, &status, &rec.Cursor, &createdStr, &updatedStr); err != nil {
			return nil, fmt.Errorf("scan running job: %w", err)
		}
		rec.Params = json.RawMessage(params)
		rec.Status = Status(status)
		rec.CreatedAt = parseSQLTime(createdStr)
		rec.UpdatedAt = parseSQLTime(updatedStr)
		records = append(records, rec)
	}
	return records, rows.Err()
}

// Get 读取单条记录(测试/诊断用);无记录返回 (nil, nil)。
func (s *Store) Get(id int64) (*Record, error) {
	var (
		rec        Record
		params     string
		status     string
		createdStr string
		updatedStr string
	)
	err := s.db.QueryRow(
		`SELECT id, kind, key, params_json, status, cursor, created_at, updated_at
		 FROM jobs WHERE id = ?`, id,
	).Scan(&rec.ID, &rec.Kind, &rec.Key, &params, &status, &rec.Cursor, &createdStr, &updatedStr)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get job: %w", err)
	}
	rec.Params = json.RawMessage(params)
	rec.Status = Status(status)
	rec.CreatedAt = parseSQLTime(createdStr)
	rec.UpdatedAt = parseSQLTime(updatedStr)
	return &rec, nil
}

// parseSQLTime 解析 SQLite 存储的时间字符串,失败返回零值。
func parseSQLTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse("2006-01-02 15:04:05.999999999-07:00", s); err == nil {
		return t
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	return time.Time{}
}
