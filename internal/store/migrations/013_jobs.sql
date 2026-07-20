-- 013: 通用异步任务运行时的持久化表(internal/jobs)。
-- 每次任务启动追加一行;游标随进度更新;终态落 status。
-- 断点续跑:进程重启时 status='running' 的行,可续跑 kind 从 cursor 继续,
-- 否则(单发任务/kind 未注册)标记 interrupted。
--
-- 接入点:internal/store/store.go 的 migrate() 用 applyMigrationFile 施加本迁移
-- (表存在性作已应用标记,jobs 为新表,与 010/012 同法)。
-- CRUD 在 internal/jobs/store.go,复用 store 拥有的同一 *sql.DB。
CREATE TABLE IF NOT EXISTS jobs (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    kind        TEXT NOT NULL,
    key         TEXT NOT NULL,
    params_json TEXT NOT NULL DEFAULT 'null',
    status      TEXT NOT NULL DEFAULT 'running',
    cursor      TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- 重启恢复:按 status 拉取仍在 running 的任务。
CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs(status);
-- 按 kind+key 反查该任务的历史运行。
CREATE INDEX IF NOT EXISTS idx_jobs_kind_key ON jobs(kind, key, id DESC);
