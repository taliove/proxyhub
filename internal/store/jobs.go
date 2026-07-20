package store

import "github.com/taliove/proxyhub/internal/jobs"

// Jobs 返回复用本 Store 数据库连接的 jobs 表存储(通用任务运行时用)。
// jobs 表由迁移链路(013_jobs.sql)建立;此处只在同一连接上做 CRUD。
func (s *Store) Jobs() *jobs.Store {
	return s.jobsStore
}
