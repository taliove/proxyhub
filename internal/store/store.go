package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/taliove/proxyhub/internal/jobs"
	_ "modernc.org/sqlite"
)

// Store SQLite 存储
type Store struct {
	db        *sql.DB
	jobsStore *jobs.Store
}

// Open 打开（或创建）数据库文件并执行迁移
func Open(path string) (*Store, error) {
	// Ensure the parent directory exists: local runtime state lives under
	// var/, which is gitignored and may not exist on a fresh checkout.
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// SQLite 单写者模型，限制连接数避免锁冲突
	db.SetMaxOpenConns(1)

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate database: %w", err)
	}
	s.jobsStore = jobs.NewStore(db)

	return s, nil
}

// Close 关闭数据库
func (s *Store) Close() error {
	return s.db.Close()
}

// migrate 建表
func (s *Store) migrate() error {
	schema := `
CREATE TABLE IF NOT EXISTS endpoints (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	alias       TEXT NOT NULL,
	path        TEXT NOT NULL UNIQUE,
	token       TEXT NOT NULL,
	enabled     INTEGER NOT NULL DEFAULT 1,
	created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS pull_logs (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	endpoint_id INTEGER NOT NULL REFERENCES endpoints(id),
	ip          TEXT NOT NULL,
	user_agent  TEXT NOT NULL DEFAULT '',
	pulled_at   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_pull_logs_endpoint ON pull_logs(endpoint_id);
CREATE INDEX IF NOT EXISTS idx_pull_logs_ip ON pull_logs(ip);

CREATE TABLE IF NOT EXISTS ip_geo (
	ip          TEXT PRIMARY KEY,
	country     TEXT NOT NULL DEFAULT '',
	region      TEXT NOT NULL DEFAULT '',
	city        TEXT NOT NULL DEFAULT '',
	isp         TEXT NOT NULL DEFAULT '',
	resolved_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS banned_ips (
	ip           TEXT PRIMARY KEY,
	fail_count   INTEGER NOT NULL DEFAULT 0,
	banned_until TIMESTAMP,
	updated_at   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS settings (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);

-- 机场订阅表
CREATE TABLE IF NOT EXISTS airports (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	name       TEXT NOT NULL,
	url        TEXT NOT NULL,
	enabled    INTEGER NOT NULL DEFAULT 1,
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- 自建节点表
CREATE TABLE IF NOT EXISTS self_hosted_nodes (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	name       TEXT NOT NULL,
	protocol   TEXT NOT NULL,
	server     TEXT NOT NULL,
	port       INTEGER NOT NULL,
	uuid       TEXT NOT NULL DEFAULT '',
	password   TEXT NOT NULL DEFAULT '',
	cipher     TEXT NOT NULL DEFAULT '',
	alter_id   INTEGER NOT NULL DEFAULT 0,
	network    TEXT NOT NULL DEFAULT 'tcp',
	tls        INTEGER NOT NULL DEFAULT 0,
	enabled    INTEGER NOT NULL DEFAULT 1,
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- 刷新记录表（聚合刷新历史）
CREATE TABLE IF NOT EXISTS refresh_runs (
	id              INTEGER PRIMARY KEY AUTOINCREMENT,
	trigger_type    TEXT NOT NULL,
	status          TEXT NOT NULL DEFAULT 'running',
	total_nodes     INTEGER NOT NULL DEFAULT 0,
	available_nodes INTEGER NOT NULL DEFAULT 0,
	final_nodes     INTEGER NOT NULL DEFAULT 0,
	error           TEXT NOT NULL DEFAULT '',
	started_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	finished_at     TIMESTAMP
);

-- 刷新事件明细表
CREATE TABLE IF NOT EXISTS refresh_events (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	run_id     INTEGER NOT NULL REFERENCES refresh_runs(id),
	level      TEXT NOT NULL DEFAULT 'info',
	stage      TEXT NOT NULL DEFAULT '',
	message    TEXT NOT NULL,
	data       TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_refresh_events_run ON refresh_events(run_id);

CREATE TABLE IF NOT EXISTS node_health (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	node_key    TEXT NOT NULL,
	name        TEXT NOT NULL,
	source      TEXT NOT NULL,
	available   INTEGER NOT NULL,
	latency_ms  INTEGER NOT NULL,
	checked_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	target_name TEXT NOT NULL DEFAULT 'connectivity',
	error       TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_node_health_checked ON node_health(checked_at DESC);
CREATE INDEX IF NOT EXISTS idx_node_health_key ON node_health(node_key);
CREATE INDEX IF NOT EXISTS idx_node_health_node_target ON node_health(node_key, target_name, checked_at DESC);

-- 节点池快照：最近一次成功聚合的完整节点池（含连接参数），
-- 供进程重启后回填内存池，避免重启即失忆（见 ADR 0008）。
-- 每轮成功刷新全量替换，与内存池保持一致。
CREATE TABLE IF NOT EXISTS nodes (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	name       TEXT NOT NULL,
	type       TEXT NOT NULL,
	server     TEXT NOT NULL,
	port       INTEGER NOT NULL,
	uuid       TEXT NOT NULL DEFAULT '',
	password   TEXT NOT NULL DEFAULT '',
	alter_id   INTEGER NOT NULL DEFAULT 0,
	cipher     TEXT NOT NULL DEFAULT '',
	network    TEXT NOT NULL DEFAULT '',
	tls        INTEGER NOT NULL DEFAULT 0,
	region     TEXT NOT NULL DEFAULT '',
	source     TEXT NOT NULL DEFAULT '',
	available  INTEGER NOT NULL DEFAULT 0,
	latency_ms INTEGER NOT NULL DEFAULT 0,
	position   INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_nodes_position ON nodes(position);

-- 机场节点屏蔽名单：按 NodeKey(server:port) 精确拉黑单个机场节点，
-- 跨刷新持久，订阅生成时剔除；自建节点豁免（见 ADR 0009）。
CREATE TABLE IF NOT EXISTS node_blocks (
	node_key   TEXT PRIMARY KEY,
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- 机场节点编辑覆盖层：按 NodeKey 覆盖展示字段（display_name/region），
-- 跨刷新持久，独立于 nodes 快照生命周期（见 spec-node-testing-upsert.md）。
CREATE TABLE IF NOT EXISTS node_overrides (
	node_key     TEXT PRIMARY KEY,
	display_name TEXT NOT NULL DEFAULT '',
	region       TEXT NOT NULL DEFAULT '',
	updated_at   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- 安全审计事件流水：登录成功/失败/蜜罐命中/达阈值封禁。保留 90 天。
CREATE TABLE IF NOT EXISTS audit_logs (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	event_type TEXT NOT NULL,
	ip         TEXT NOT NULL,
	username   TEXT NOT NULL,
	detail     TEXT,
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at ON audit_logs(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_logs_event_type ON audit_logs(event_type);
CREATE INDEX IF NOT EXISTS idx_audit_logs_ip ON audit_logs(ip);
`
	_, err := s.db.Exec(schema)
	if err != nil {
		return err
	}

	// 增量列迁移(CREATE TABLE IF NOT EXISTS 不会给旧表补列)
	if err := s.addColumnIfMissing("airports", "abbr", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	// 订阅地址的按端点名称标准化覆盖(见 ADR 0012)
	if err := s.addColumnIfMissing("endpoints", "name_mode", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing("endpoints", "name_template", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}

	// 自建节点缓存的真实地区码(存时解析,见 2026-07-16 设计)
	if err := s.addColumnIfMissing("self_hosted_nodes", "region_code", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}

	// gRPC 传输选项(vless/vmess over grpc 需要 service-name)
	if err := s.addColumnIfMissing("self_hosted_nodes", "grpc_service_name", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}

	// NodeKey upsert + 带宽测试 + stale 标记（见 docs/spec-node-testing-upsert.md）
	if err := s.migrateNodesToUpsert(); err != nil {
		return err
	}

	// node_health 带宽列
	if err := s.addColumnIfMissing("node_health", "down_mbps", "REAL NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing("node_health", "up_mbps", "REAL NOT NULL DEFAULT 0"); err != nil {
		return err
	}

	// node_health 解锁级别/命中区域列（011_node_health_unlock.sql）。
	// 用 addColumnIfMissing（按列存在性幂等）而非 applyMigrationFile：
	// 后者以表存在性作已应用标记，node_health 恒存在会被误判为已应用 -> 死迁移。
	if err := s.addColumnIfMissing("node_health", "level", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing("node_health", "region", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}

	// 流量分发表（008_distribution.sql）
	if err := s.applyMigrationFile("008_distribution.sql"); err != nil {
		return err
	}

	// 流量分发节点表（009_distribution_nodes.sql）
	if err := s.applyMigrationFile("009_distribution_nodes.sql"); err != nil {
		return err
	}

	// 深度体检历史表（010_exam_history.sql）
	if err := s.applyMigrationFile("010_exam_history.sql"); err != nil {
		return err
	}

	// 节点自动标签表（012_node_tags.sql）
	if err := s.applyMigrationFile("012_node_tags.sql"); err != nil {
		return err
	}

	// 通用异步任务运行时表（013_jobs.sql）
	if err := s.applyMigrationFile("013_jobs.sql"); err != nil {
		return err
	}

	// 初始化地区识别规则表
	return s.InitRegionRules()
}

// addColumnIfMissing 幂等地为已存在的表补充列。用于给旧库加新字段
// (SQLite 的 CREATE TABLE IF NOT EXISTS 不会修改既有表结构)。
func (s *Store) addColumnIfMissing(table, column, decl string) error {
	rows, err := s.db.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return fmt.Errorf("inspect %s columns: %w", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		if name == column {
			return nil // 已存在
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	stmt := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, decl)
	if _, err := s.db.Exec(stmt); err != nil {
		return fmt.Errorf("add column %s.%s: %w", table, column, err)
	}
	return nil
}

// applyMigrationFile 读取并执行迁移文件（幂等，支持 CREATE TABLE IF NOT EXISTS）
func (s *Store) applyMigrationFile(filename string) error {
	// 在生产环境中，迁移文件应通过 go:embed 嵌入或从固定路径读取
	// 这里直接执行 SQL，假设表已通过 IF NOT EXISTS 保护
	// 由于迁移使用 IF NOT EXISTS，重复执行是安全的

	var migrationSQL string
	var checkTable string

	switch filename {
	case "008_distribution.sql":
		checkTable = "distribution_config"
		migrationSQL = `
CREATE TABLE IF NOT EXISTS distribution_config (
    id             INTEGER PRIMARY KEY CHECK (id = 1),
    enabled        INTEGER NOT NULL DEFAULT 0,
    listen_port    INTEGER NOT NULL DEFAULT 10808,
    domain         TEXT NOT NULL DEFAULT '',
    protocol       TEXT NOT NULL DEFAULT 'vless',
    network        TEXT NOT NULL DEFAULT 'tcp',
    uuid           TEXT NOT NULL DEFAULT '',
    tls            INTEGER NOT NULL DEFAULT 0,
    cert_path      TEXT NOT NULL DEFAULT '',
    key_path       TEXT NOT NULL DEFAULT '',
    updated_at     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT OR IGNORE INTO distribution_config (id, enabled) VALUES (1, 0);

CREATE TABLE IF NOT EXISTS distribution_paths (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    name               TEXT NOT NULL,
    path               TEXT NOT NULL UNIQUE,
    upstream_node_keys TEXT NOT NULL DEFAULT '[]',
    lb_strategy        TEXT NOT NULL DEFAULT 'random',
    total_upload       INTEGER NOT NULL DEFAULT 0,
    total_download     INTEGER NOT NULL DEFAULT 0,
    total_connections  INTEGER NOT NULL DEFAULT 0,
    last_access        TIMESTAMP,
    enabled            INTEGER NOT NULL DEFAULT 1,
    created_at         TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_distribution_paths_path ON distribution_paths(path);
CREATE INDEX IF NOT EXISTS idx_distribution_paths_enabled ON distribution_paths(enabled);

CREATE TABLE IF NOT EXISTS distribution_stats (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    path_id     INTEGER NOT NULL REFERENCES distribution_paths(id) ON DELETE CASCADE,
    timestamp   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    upload      INTEGER NOT NULL DEFAULT 0,
    download    INTEGER NOT NULL DEFAULT 0,
    connections INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_distribution_stats_path ON distribution_stats(path_id, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_distribution_stats_timestamp ON distribution_stats(timestamp DESC);
`
	case "009_distribution_nodes.sql":
		checkTable = "distribution_nodes"
		migrationSQL = `
CREATE TABLE IF NOT EXISTS distribution_nodes (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    name               TEXT NOT NULL,
    region             TEXT NOT NULL DEFAULT '',
    distribution_path  TEXT NOT NULL UNIQUE,
    upstream_node_keys TEXT NOT NULL DEFAULT '[]',
    lb_strategy        TEXT NOT NULL DEFAULT 'random',
    enabled            INTEGER NOT NULL DEFAULT 1,
    created_at         TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at         TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_distribution_nodes_path ON distribution_nodes(distribution_path);
CREATE INDEX IF NOT EXISTS idx_distribution_nodes_enabled ON distribution_nodes(enabled);
`
	case "010_exam_history.sql":
		checkTable = "exam_history"
		migrationSQL = `
CREATE TABLE IF NOT EXISTS exam_history (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    node_key    TEXT NOT NULL,
    report_json TEXT NOT NULL,
    created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_exam_history_node ON exam_history(node_key, id DESC);
`
	case "012_node_tags.sql":
		checkTable = "node_tags"
		migrationSQL = `
CREATE TABLE IF NOT EXISTS node_tags (
    node_key TEXT NOT NULL,
    tag      TEXT NOT NULL,
    PRIMARY KEY (node_key, tag)
);

CREATE INDEX IF NOT EXISTS idx_node_tags_tag ON node_tags(tag);
`
	case "013_jobs.sql":
		checkTable = "jobs"
		migrationSQL = `
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

CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs(status);
CREATE INDEX IF NOT EXISTS idx_jobs_kind_key ON jobs(kind, key, id DESC);
`
	default:
		return fmt.Errorf("unknown migration file: %s", filename)
	}

	// 检查表是否存在（作为迁移完成标记）
	var name string
	err := s.db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, checkTable).Scan(&name)
	if err == nil {
		// 表已存在，迁移已应用
		return nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("check migration %s: %w", filename, err)
	}

	_, err = s.db.Exec(migrationSQL)
	if err != nil {
		return fmt.Errorf("apply migration %s: %w", filename, err)
	}
	return nil
}

// timeOrNull 将 time.Time 转为 SQL NULL-able 类型（零值→nil）
func timeOrNull(t time.Time) interface{} {
	if t.IsZero() {
		return nil
	}
	return t
}

// parseTimeOrZero 解析 SQL 返回的时间字符串，失败返回零值
func parseTimeOrZero(s *string) time.Time {
	if s == nil || *s == "" {
		return time.Time{}
	}
	// 尝试两种格式（SQLite 存储格式）
	if t, err := time.Parse("2006-01-02 15:04:05.999999999-07:00", *s); err == nil {
		return t
	}
	if t, err := time.Parse(time.RFC3339, *s); err == nil {
		return t
	}
	return time.Time{}
}
