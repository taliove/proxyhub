package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// DistributionConfig 流量分发全局配置（单行）
type DistributionConfig struct {
	ID         int64     `json:"id"`
	Enabled    bool      `json:"enabled"`
	ListenPort int       `json:"listen_port"`
	Domain     string    `json:"domain"`
	Protocol   string    `json:"protocol"`
	Network    string    `json:"network"`
	UUID       string    `json:"uuid"`
	TLS        bool      `json:"tls"`
	CertPath   string    `json:"cert_path"`
	KeyPath    string    `json:"key_path"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// DistributionPath 分发路径（流量路由规则）
type DistributionPath struct {
	ID               int64     `json:"id"`
	Name             string    `json:"name"`
	Path             string    `json:"path"`
	UpstreamNodeKeys []string  `json:"upstream_node_keys"`
	LBStrategy       string    `json:"lb_strategy"`
	TotalUpload      int64     `json:"total_upload"`
	TotalDownload    int64     `json:"total_download"`
	TotalConnections int64     `json:"total_connections"`
	LastAccess       time.Time `json:"last_access"`
	Enabled          bool      `json:"enabled"`
	CreatedAt        time.Time `json:"created_at"`
}

// DistributionStat 分发统计（时序流量数据）
type DistributionStat struct {
	ID          int64     `json:"id"`
	PathID      int64     `json:"path_id"`
	Timestamp   time.Time `json:"timestamp"`
	Upload      int64     `json:"upload"`
	Download    int64     `json:"download"`
	Connections int64     `json:"connections"`
}

// GetDistributionConfig 获取分发配置（单行，不存在返回默认值）
func (s *Store) GetDistributionConfig() (*DistributionConfig, error) {
	row := s.db.QueryRow(`
		SELECT id, enabled, listen_port, domain, protocol, network, uuid, tls, cert_path, key_path, updated_at
		FROM distribution_config WHERE id = 1
	`)

	var cfg DistributionConfig
	var enabled, tls int
	err := row.Scan(&cfg.ID, &enabled, &cfg.ListenPort, &cfg.Domain, &cfg.Protocol,
		&cfg.Network, &cfg.UUID, &tls, &cfg.CertPath, &cfg.KeyPath, &cfg.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// 返回默认配置
			return &DistributionConfig{
				ID:         1,
				Enabled:    false,
				ListenPort: 10808,
				Protocol:   "vless",
				Network:    "tcp",
			}, nil
		}
		return nil, fmt.Errorf("query distribution config: %w", err)
	}

	cfg.Enabled = enabled != 0
	cfg.TLS = tls != 0
	return &cfg, nil
}

// SaveDistributionConfig 保存分发配置（upsert 单行）
func (s *Store) SaveDistributionConfig(cfg *DistributionConfig) error {
	if cfg.ListenPort < 1 || cfg.ListenPort > 65535 {
		return errors.New("listen_port must be between 1 and 65535")
	}
	if cfg.Protocol == "" {
		return errors.New("protocol is required")
	}

	_, err := s.db.Exec(`
		INSERT INTO distribution_config
		(id, enabled, listen_port, domain, protocol, network, uuid, tls, cert_path, key_path, updated_at)
		VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(id) DO UPDATE SET
		enabled = excluded.enabled,
		listen_port = excluded.listen_port,
		domain = excluded.domain,
		protocol = excluded.protocol,
		network = excluded.network,
		uuid = excluded.uuid,
		tls = excluded.tls,
		cert_path = excluded.cert_path,
		key_path = excluded.key_path,
		updated_at = CURRENT_TIMESTAMP
	`, boolToInt(cfg.Enabled), cfg.ListenPort, cfg.Domain, cfg.Protocol, cfg.Network,
		cfg.UUID, boolToInt(cfg.TLS), cfg.CertPath, cfg.KeyPath)

	if err != nil {
		return fmt.Errorf("save distribution config: %w", err)
	}
	return nil
}

// ListDistributionPaths 列出所有分发路径
func (s *Store) ListDistributionPaths() ([]*DistributionPath, error) {
	rows, err := s.db.Query(`
		SELECT id, name, path, upstream_node_keys, lb_strategy,
		       total_upload, total_download, total_connections, last_access, enabled, created_at
		FROM distribution_paths ORDER BY id
	`)
	if err != nil {
		return nil, fmt.Errorf("query distribution paths: %w", err)
	}
	defer rows.Close()

	var paths []*DistributionPath
	for rows.Next() {
		path, err := s.scanDistributionPathRow(rows)
		if err != nil {
			return nil, err
		}
		paths = append(paths, path)
	}
	return paths, rows.Err()
}

// GetDistributionPath 按 ID 查询分发路径
func (s *Store) GetDistributionPath(id int64) (*DistributionPath, error) {
	return s.scanDistributionPath(s.db.QueryRow(`
		SELECT id, name, path, upstream_node_keys, lb_strategy,
		       total_upload, total_download, total_connections, last_access, enabled, created_at
		FROM distribution_paths WHERE id = ?
	`, id))
}

// GetDistributionPathByPath 按路径字符串查询（用于代理请求路由）
func (s *Store) GetDistributionPathByPath(path string) (*DistributionPath, error) {
	return s.scanDistributionPath(s.db.QueryRow(`
		SELECT id, name, path, upstream_node_keys, lb_strategy,
		       total_upload, total_download, total_connections, last_access, enabled, created_at
		FROM distribution_paths WHERE path = ?
	`, path))
}

// CreateDistributionPath 创建分发路径
func (s *Store) CreateDistributionPath(path *DistributionPath) (*DistributionPath, error) {
	if path.Name == "" {
		return nil, errors.New("name is required")
	}
	if path.Path == "" {
		return nil, errors.New("path is required")
	}
	if path.LBStrategy == "" {
		path.LBStrategy = "random"
	}

	nodeKeysJSON, err := json.Marshal(path.UpstreamNodeKeys)
	if err != nil {
		return nil, fmt.Errorf("marshal upstream_node_keys: %w", err)
	}

	res, err := s.db.Exec(`
		INSERT INTO distribution_paths (name, path, upstream_node_keys, lb_strategy, enabled)
		VALUES (?, ?, ?, ?, ?)
	`, path.Name, path.Path, string(nodeKeysJSON), path.LBStrategy, boolToInt(path.Enabled))
	if err != nil {
		return nil, fmt.Errorf("insert distribution path: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("get distribution path id: %w", err)
	}

	return s.GetDistributionPath(id)
}

// UpdateDistributionPath 更新分发路径
func (s *Store) UpdateDistributionPath(path *DistributionPath) error {
	if path.Name == "" {
		return errors.New("name is required")
	}
	if path.Path == "" {
		return errors.New("path is required")
	}

	nodeKeysJSON, err := json.Marshal(path.UpstreamNodeKeys)
	if err != nil {
		return fmt.Errorf("marshal upstream_node_keys: %w", err)
	}

	res, err := s.db.Exec(`
		UPDATE distribution_paths
		SET name = ?, path = ?, upstream_node_keys = ?, lb_strategy = ?, enabled = ?
		WHERE id = ?
	`, path.Name, path.Path, string(nodeKeysJSON), path.LBStrategy, boolToInt(path.Enabled), path.ID)
	if err != nil {
		return fmt.Errorf("update distribution path: %w", err)
	}
	return checkAffected(res)
}

// DeleteDistributionPath 删除分发路径（手动级联删除统计数据）
func (s *Store) DeleteDistributionPath(id int64) error {
	// 先删除统计数据
	if _, err := s.db.Exec(`DELETE FROM distribution_stats WHERE path_id = ?`, id); err != nil {
		return fmt.Errorf("delete distribution stats: %w", err)
	}

	// 再删除路径
	res, err := s.db.Exec(`DELETE FROM distribution_paths WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete distribution path: %w", err)
	}
	return checkAffected(res)
}

// RecordDistributionStat 记录分发统计快照
func (s *Store) RecordDistributionStat(stat *DistributionStat) error {
	_, err := s.db.Exec(`
		INSERT INTO distribution_stats (path_id, timestamp, upload, download, connections)
		VALUES (?, ?, ?, ?, ?)
	`, stat.PathID, timeOrNull(stat.Timestamp), stat.Upload, stat.Download, stat.Connections)
	if err != nil {
		return fmt.Errorf("insert distribution stat: %w", err)
	}
	return nil
}

// GetDistributionStats 查询分发路径的统计数据（时间倒序）
func (s *Store) GetDistributionStats(pathID int64, limit int) ([]*DistributionStat, error) {
	if limit <= 0 {
		limit = 100
	}

	rows, err := s.db.Query(`
		SELECT id, path_id, timestamp, upload, download, connections
		FROM distribution_stats
		WHERE path_id = ?
		ORDER BY timestamp DESC
		LIMIT ?
	`, pathID, limit)
	if err != nil {
		return nil, fmt.Errorf("query distribution stats: %w", err)
	}
	defer rows.Close()

	var stats []*DistributionStat
	for rows.Next() {
		var stat DistributionStat
		var timestamp string
		if err := rows.Scan(&stat.ID, &stat.PathID, &timestamp, &stat.Upload, &stat.Download, &stat.Connections); err != nil {
			return nil, fmt.Errorf("scan stat: %w", err)
		}
		stat.Timestamp = parseTimeOrZero(&timestamp)
		stats = append(stats, &stat)
	}
	return stats, rows.Err()
}

// UpdatePathTotalStats 更新分发路径的累计统计（原子增量）
func (s *Store) UpdatePathTotalStats(pathID int64, uploadDelta, downloadDelta, connectionsDelta int64) error {
	res, err := s.db.Exec(`
		UPDATE distribution_paths
		SET total_upload = total_upload + ?,
		    total_download = total_download + ?,
		    total_connections = total_connections + ?,
		    last_access = CURRENT_TIMESTAMP
		WHERE id = ?
	`, uploadDelta, downloadDelta, connectionsDelta, pathID)
	if err != nil {
		return fmt.Errorf("update path total stats: %w", err)
	}
	return checkAffected(res)
}

// scanDistributionPath 扫描单行到 DistributionPath（QueryRow 用）
func (s *Store) scanDistributionPath(row *sql.Row) (*DistributionPath, error) {
	path, err := scanDistributionPathFrom(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return path, err
}

// scanDistributionPathRow 扫描多行到 DistributionPath（Query 用）
func (s *Store) scanDistributionPathRow(rows *sql.Rows) (*DistributionPath, error) {
	return scanDistributionPathFrom(rows)
}

// scanDistributionPathFrom 通用扫描器
func scanDistributionPathFrom(r rowScanner) (*DistributionPath, error) {
	var path DistributionPath
	var nodeKeysJSON string
	var enabled int
	var lastAccess *string

	err := r.Scan(&path.ID, &path.Name, &path.Path, &nodeKeysJSON, &path.LBStrategy,
		&path.TotalUpload, &path.TotalDownload, &path.TotalConnections, &lastAccess,
		&enabled, &path.CreatedAt)
	if err != nil {
		return nil, err
	}

	path.Enabled = enabled != 0
	path.LastAccess = parseTimeOrZero(lastAccess)

	// 解析 JSON 数组
	if err := json.Unmarshal([]byte(nodeKeysJSON), &path.UpstreamNodeKeys); err != nil {
		return nil, fmt.Errorf("unmarshal upstream_node_keys: %w", err)
	}

	return &path, nil
}
