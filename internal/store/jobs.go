package store

import (
	"fmt"

	"github.com/taliove/proxyhub/internal/jobs"
)

// Jobs 返回复用本 Store 数据库连接的 jobs 表存储(通用任务运行时用)。
// jobs 表由迁移链路(013_jobs.sql)建立;此处只在同一连接上做 CRUD。
func (s *Store) Jobs() *jobs.Store {
	return s.jobsStore
}

// GetLatestJobByKindKey retrieves the most recent job (by created_at) matching the given kind and key.
// Returns nil if no matching job exists.
func (s *Store) GetLatestJobByKindKey(kind, key string) (*jobs.Record, error) {
	var (
		rec        jobs.Record
		params     string
		status     string
		createdStr string
		updatedStr string
	)
	err := s.db.QueryRow(`
		SELECT id, kind, key, params_json, status, cursor, created_at, updated_at
		FROM jobs
		WHERE kind = ? AND key = ?
		ORDER BY created_at DESC
		LIMIT 1
	`, kind, key).Scan(&rec.ID, &rec.Kind, &rec.Key, &params, &status, &rec.Cursor, &createdStr, &updatedStr)

	if err != nil {
		if isNoRows(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("get latest job: %w", err)
	}

	rec.Params = []byte(params)
	rec.Status = jobs.Status(status)
	rec.CreatedAt = parseSQLiteTime(createdStr)
	rec.UpdatedAt = parseSQLiteTime(updatedStr)
	return &rec, nil
}
