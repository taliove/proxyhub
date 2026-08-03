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
	-- path 必须全局唯一(不改 (user_id, path) 联合唯一):
	-- /sub/{path} 是公开无用户上下文的入口,反查属主只能靠 path 唯一索引;
	-- 且 path 是 16 字符随机 hex,碰撞概率可忽略。ticket 10 决策偏离原 ticket 文案。
	path        TEXT NOT NULL UNIQUE,
	token       TEXT NOT NULL,
	enabled     INTEGER NOT NULL DEFAULT 1,
	created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	-- 地域白名单(pull-guard ticket 07):geo_mode off/observe/enforce,
	-- geo_countries/geo_provinces 逗号分隔,空=该维度不判。语义见 endpoint_geo.go。
	-- 新库由本 schema 建出;既有库靠 migrateEndpointGeo 幂等补列(同 pull_logs.status 双路径)。
	geo_mode      TEXT NOT NULL DEFAULT 'off',
	geo_countries TEXT NOT NULL DEFAULT '',
	geo_provinces TEXT NOT NULL DEFAULT '',
	-- 订阅 profile 公开名称(issue #38):空串=未设,/sub 回退裸品牌名。
	-- 新库由本 schema 建出;既有库靠 migrateEndpointPublicName 幂等补列。
	public_name   TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS pull_logs (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	endpoint_id INTEGER NOT NULL REFERENCES endpoints(id),
	ip          TEXT NOT NULL,
	user_agent  TEXT NOT NULL DEFAULT '',
	pulled_at   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	-- status 拉取结果(pull-guard ticket 01):被拦请求也留痕,取值见 pull_status.go。
	-- 新库由本 schema 建出;既有库靠 migratePullLogStatus 幂等补列(同 multi_tenant 的
	-- "schema + 增量迁移" 双路径),endpoint_id=0 是 path 未知时的全局桶。
	status      TEXT NOT NULL DEFAULT 'ok'
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

-- 节点 Server 的 GeoIP 缓存(issue #37 三层识别 L3):host(域名)主键,全局维度
-- (server→country 与用户无关,不做 per-user);country_code 存 ISO 3166-1 alpha-2,
-- 空串 = 负缓存(DNS 失败/无记录,短 TTL 防每轮刷新重试)。不复用 ip_geo:
-- 主键维度不同(IP vs 域名)且其 country 列存中文名,语义是 pull_logs 来源地理。
-- 新库由本 schema 建出;既有库靠 migrateNodeServerGeo 幂等建表(同 pull_logs.status 双路径)。
CREATE TABLE IF NOT EXISTS node_server_geo (
	host         TEXT PRIMARY KEY,
	country_code TEXT NOT NULL DEFAULT '',
	resolved_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
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
-- (user_id, node_key) 联合主键:同一节点可被不同用户独立屏蔽(多租户,021)。
CREATE TABLE IF NOT EXISTS node_blocks (
	user_id    INTEGER NOT NULL DEFAULT 0,
	node_key   TEXT NOT NULL,
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	PRIMARY KEY (user_id, node_key)
);

-- 机场节点编辑覆盖层：按 NodeKey 覆盖展示字段（display_name/region），
-- 跨刷新持久，独立于 nodes 快照生命周期（见 spec-node-testing-upsert.md）。
-- (user_id, node_key) 联合主键:同一节点可被不同用户独立覆盖(多租户,021)。
CREATE TABLE IF NOT EXISTS node_overrides (
	user_id      INTEGER NOT NULL DEFAULT 0,
	node_key     TEXT NOT NULL,
	display_name TEXT NOT NULL DEFAULT '',
	region       TEXT NOT NULL DEFAULT '',
	updated_at   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	PRIMARY KEY (user_id, node_key)
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
	// 手动机场来源类型与用量信息(spec-manual-airport-import,见 ADR 0034):
	// 历史行默认 'url' 拉取型;用量列零值 = 未知不展示。
	if err := s.addColumnIfMissing("airports", "source_type", "TEXT NOT NULL DEFAULT 'url'"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing("airports", "usage_upload", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing("airports", "usage_download", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing("airports", "usage_total", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing("airports", "usage_expire", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing("airports", "web_page_url", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	// 订阅地址的按端点名称标准化覆盖(见 ADR 0012)
	if err := s.addColumnIfMissing("endpoints", "name_mode", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing("endpoints", "name_template", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	// 订阅地址的节点范围筛选条件(013_endpoint_conditions.sql)。
	// 用 addColumnIfMissing(按列存在性幂等)而非 applyMigrationFile:后者以表存在性
	// 作已应用标记,endpoints 恒存在会被误判为已应用 -> 死迁移(同 011 先例)。
	if err := s.addColumnIfMissing("endpoints", "conditions", "TEXT NOT NULL DEFAULT ''"); err != nil {
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

	// 机场测试运行记录表（014_airport_test_runs.sql）
	if err := s.applyMigrationFile("014_airport_test_runs.sql"); err != nil {
		return err
	}

	// 常规刷新每机场拉取诊断表（015_refresh_fetch_diags.sql,ticket 0018）
	if err := s.applyMigrationFile("015_refresh_fetch_diags.sql"); err != nil {
		return err
	}

	// 本机实测历史表（016_speedtest_results.sql,ticket 0032）
	if err := s.applyMigrationFile("016_speedtest_results.sql"); err != nil {
		return err
	}

	// 用户账户表（017_users.sql,ticket 01 用户模型）
	if err := s.applyMigrationFile("017_users.sql"); err != nil {
		return err
	}

	// 用户资源配额表（018_user_quotas.sql,ticket 01 用户模型）
	if err := s.applyMigrationFile("018_user_quotas.sql"); err != nil {
		return err
	}

	// 每用户 Xray 实例表（020_user_xray_instances.sql,ticket 08）
	if err := s.applyMigrationFile("020_user_xray_instances.sql"); err != nil {
		return err
	}

	// 数据模型多租户化(ticket 06):user_id 列 + settings 拆分。
	// 必须在 MigrateAdminToSuperUser 之前:migrateMultiTenant 创建 system_settings,
	// GetSetting 读它;放在所有既有迁移之后,保证目标表都已存在;幂等。
	if err := s.migrateMultiTenant(); err != nil {
		return err
	}

	// 超管自动迁移:users 表为空且 settings.admin_user 存在时,迁移为 super_admin
	if err := s.MigrateAdminToSuperUser(); err != nil {
		return err
	}

	// 超管落位后回填:user_id=0 的历史行统一归属超管(ticket 06 expand)。
	// 无超管(全新安装)时幂等为空操作,首位超管创建后由下次启动补齐。
	if err := s.BackfillUserID(); err != nil {
		return err
	}

	// 屏蔽名单/覆盖层主键重建为 (user_id, node_key)(多租户写隔离,021)。
	// 必须在 migrateMultiTenant(user_id 列)与 BackfillUserID(归属回填)之后。
	if err := s.migrateNodeOwnershipScope(); err != nil {
		return err
	}

	// 刷新任务化:refresh_runs 关联 jobs 任务 id(ticket 03,刷新迁入 jobs 运行时)
	if err := s.addColumnIfMissing("refresh_runs", "job_id", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}

	// 任务结果关联:exam_history 记录产出它的 jobs 任务 id(ticket 0022;
	// 0 = 任务结果关联前的旧数据或未关联,查询侧走任务时间窗回退)
	if err := s.addColumnIfMissing("exam_history", "job_id", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}

	// 机场测试任务化前置:airport_test_runs 关联 jobs 任务 id(ticket 0024,
	// 对齐 refresh_runs 的 job_id 模式;0 = 任务化前的旧记录或未关联)
	if err := s.addColumnIfMissing("airport_test_runs", "job_id", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}

	// 模板库升级(ticket endpoint-template-01):template 表重建为带 is_default
	// 和 UNIQUE(user_id, name) 的库表;endpoints 加 template_name 准备列(02 消费);
	// user_quotas 加 max_templates 配额列(默认 10)。
	if err := s.migrateTemplateLibrary(); err != nil {
		return err
	}
	if err := s.addColumnIfMissing("endpoints", "template_name", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	// 模板版本表(ticket template-editor-upgrade-01):template_versions 表
	// 用于自动保存版本历史,保留最近 20 个版本。
	if err := s.migrateTemplateVersions(); err != nil {
		return err
	}
	if err := s.addColumnIfMissing("user_quotas", "max_templates", "INTEGER NOT NULL DEFAULT 10"); err != nil {
		return err
	}

	// MFA 与受信 IP(登录加固 ticket 02):users 加 3 列 + user_trusted_ips 表 +
	// audit_logs (username, ip, created_at) 复合索引。必须在 017_users.sql 之后。
	if err := s.migrateMFA(); err != nil {
		return err
	}

	// 审计增强:audit_logs 加 user_agent 列(登录加固批次审计增强)
	if err := s.addColumnIfMissing("audit_logs", "user_agent", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}

	// pull_logs 异常留痕(pull-guard ticket 01):加 status 列 + (endpoint_id, status)
	// 索引。既有库补列默认 'ok'——本 ticket 之前只记成功下发,存量行语义就是 ok。
	if err := s.migratePullLogStatus(); err != nil {
		return err
	}

	// 自动升级链计数索引(拉取防护 ticket 05):(ip, status, pulled_at) 让
	// "某 IP 最近 1 小时 rate_limited 行数" 走索引搜索而非全表扫描 —— 该查询
	// 跑在 /sub 热路径上被拒的那一侧,正是攻击者能控制的请求。
	if err := s.migratePullLogEscalationIndex(); err != nil {
		return err
	}

	// 统一 IP 规则表(拉取防护 ticket 02):ip_access_rules 承载整站拒止(scope=global)
	// 与拉取黑名单(scope=sub),expires_at 为空表示永久。
	if err := s.EnsureIPAccessRulesSchema(); err != nil {
		return err
	}

	// 订阅地址地域白名单(拉取防护 ticket 07):endpoints 补 geo_mode/geo_countries/
	// geo_provinces 三列,默认 off + 双空列表 = 存量订阅行为不变。
	if err := s.migrateEndpointGeo(); err != nil {
		return err
	}

	// 订阅 profile 公开名称(issue #38):endpoints 补 public_name 列,
	// 默认空串 = 未设,/sub 头回退为裸品牌名。
	if err := s.migrateEndpointPublicName(); err != nil {
		return err
	}

	// banned_ips 时间格式归一(pull-guard ticket 00):把存量 Go String 格式的
	// banned_until/updated_at 重写成 UTC "2006-01-02 15:04:05",让裸 SQL
	// datetime() 能读(ADR 0010)。读路径已双格式兼容,故此处尽力而为:
	// 失败只 warn 不返回错误,永不阻断启动。
	s.migrateBannedIPTimeFormat()

	// 节点 Server GeoIP 缓存表(issue #37):新库已由上方 schema 建出,
	// 既有库在此幂等补建(双路径,同 pull_logs.status 注释模式)。
	if err := s.migrateNodeServerGeo(); err != nil {
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
	case "014_airport_test_runs.sql":
		checkTable = "airport_test_runs"
		migrationSQL = `
CREATE TABLE IF NOT EXISTS airport_test_runs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    airport_id INTEGER NOT NULL,
    created_at TIMESTAMP NOT NULL,
    sample_params TEXT NOT NULL DEFAULT '{}',
    is_full INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL,
    overall_score REAL,
    dimensions_json TEXT NOT NULL DEFAULT '{}',
    error_message TEXT,
    FOREIGN KEY(airport_id) REFERENCES airports(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_airport_test_runs_airport ON airport_test_runs(airport_id, id DESC);
CREATE INDEX IF NOT EXISTS idx_airport_test_runs_created ON airport_test_runs(created_at);
`
	case "015_refresh_fetch_diags.sql":
		checkTable = "refresh_fetch_diags"
		migrationSQL = `
CREATE TABLE IF NOT EXISTS refresh_fetch_diags (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id         INTEGER NOT NULL REFERENCES refresh_runs(id),
    airport        TEXT NOT NULL,
    airport_id     INTEGER NOT NULL DEFAULT 0,
    http_status    INTEGER NOT NULL DEFAULT 0,
    duration_ms    INTEGER NOT NULL DEFAULT 0,
    node_count     INTEGER NOT NULL DEFAULT 0,
    parse_failures INTEGER NOT NULL DEFAULT 0,
    error          TEXT NOT NULL DEFAULT '',
    created_at     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_refresh_fetch_diags_run ON refresh_fetch_diags(run_id);
`
	case "016_speedtest_results.sql":
		checkTable = "speedtest_results"
		migrationSQL = `
CREATE TABLE IF NOT EXISTS speedtest_results (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    node_key        TEXT,
    down_mbps       REAL NOT NULL DEFAULT 0,
    up_mbps         REAL NOT NULL DEFAULT 0,
    idle_latency_ms REAL NOT NULL DEFAULT 0,
    jitter_ms       REAL NOT NULL DEFAULT 0,
    client_info     TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_speedtest_results_node ON speedtest_results(node_key, id DESC);
`
	case "020_user_xray_instances.sql":
		checkTable = "user_xray_instances"
		migrationSQL = `
CREATE TABLE IF NOT EXISTS user_xray_instances (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id         INTEGER NOT NULL UNIQUE,
    port            INTEGER NOT NULL,
    config_path     TEXT NOT NULL DEFAULT '',
    pid             INTEGER NOT NULL DEFAULT 0,
    status          TEXT NOT NULL DEFAULT 'stopped',
    last_started_at TIMESTAMP,
    created_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_user_xray_instances_status ON user_xray_instances(status);
CREATE INDEX IF NOT EXISTS idx_user_xray_instances_port ON user_xray_instances(port);
`
	case "017_users.sql":
		checkTable = "users"
		migrationSQL = `
CREATE TABLE IF NOT EXISTS users (
    id                   INTEGER PRIMARY KEY AUTOINCREMENT,
    username             TEXT NOT NULL UNIQUE,
    pass_hash            TEXT NOT NULL,
    role                 TEXT NOT NULL,
    must_change_password INTEGER NOT NULL DEFAULT 0,
    disabled_at          TIMESTAMP,
    created_at           TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_login_at        TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_users_role ON users(role);
`
	case "018_user_quotas.sql":
		checkTable = "user_quotas"
		migrationSQL = `
CREATE TABLE IF NOT EXISTS user_quotas (
    user_id          INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    max_airports     INTEGER NOT NULL DEFAULT 0,
    max_endpoints    INTEGER NOT NULL DEFAULT 0,
    xray_port_start  INTEGER NOT NULL DEFAULT 0,
    xray_port_end    INTEGER NOT NULL DEFAULT 0
);
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
