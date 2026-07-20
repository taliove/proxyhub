package store

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/taliove/proxyhub/internal/subscription"
)

// Airport 机场订阅
type Airport struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	Abbr      string    `json:"abbr"` // 机场简称,空表示自动生成(见 ADR 0012)
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
}

// SelfHostedNode 自建节点
type SelfHostedNode struct {
	ID               int64  `json:"id"`
	Name             string `json:"name"`
	Protocol         string `json:"protocol"`
	Server           string `json:"server"`
	Port             int    `json:"port"`
	UUID             string `json:"uuid"`
	Password         string `json:"password"`
	Cipher           string `json:"cipher"`
	AlterID          int    `json:"alter_id"`
	Network          string `json:"network"`
	TLS              bool   `json:"tls"`
	RegionCode       string `json:"region_code"`
	GrpcServiceName  string `json:"grpc_service_name"`
	Enabled          bool   `json:"enabled"`
}

// ToNode 把自建节点转换为聚合/订阅使用的 subscription.Node。
//
// Region 固定 "SELF"、Source 标记 SourceSelfHosted(全过滤链豁免的常驻安全网)。
// Available 默认 true、Latency 0 仅作占位:走健康检查时会被真实检测结果覆盖
// (见 aggregator.checkHealth 的降级逻辑),serve-time 兜底合并时则保留占位直到下轮检查。
// 聚合注入与 serve-time 合并共用此方法,避免两处重复构造(DRY)。
func (n *SelfHostedNode) ToNode() *subscription.Node {
	region := n.RegionCode
	if region == "" {
		region = "SELF" // 历史行未解析时的兼容兜底
	}
	return &subscription.Node{
		Name:            n.Name,
		Type:            n.Protocol,
		Server:          n.Server,
		Port:            n.Port,
		UUID:            n.UUID,
		Password:        n.Password,
		AlterID:         n.AlterID,
		Cipher:          n.Cipher,
		Network:         n.Network,
		TLS:             n.TLS,
		GrpcServiceName: n.GrpcServiceName,
		Region:          region,
		Source:          subscription.SourceSelfHosted,
		Available:       true,
	}
}

// CreateAirport 添加机场
func (s *Store) CreateAirport(name, url string) (*Airport, error) {
	result, err := s.db.Exec(
		`INSERT INTO airports (name, url) VALUES (?, ?)`,
		name, url)
	if err != nil {
		return nil, fmt.Errorf("insert airport: %w", err)
	}

	id, _ := result.LastInsertId()
	return &Airport{
		ID:        id,
		Name:      name,
		URL:       url,
		Enabled:   true,
		CreatedAt: time.Now(),
	}, nil
}

// ListAirports 列出所有机场
func (s *Store) ListAirports() ([]*Airport, error) {
	rows, err := s.db.Query(
		`SELECT id, name, url, abbr, enabled, created_at FROM airports ORDER BY id DESC`)
	if err != nil {
		return nil, fmt.Errorf("query airports: %w", err)
	}
	defer rows.Close()

	var airports []*Airport
	for rows.Next() {
		var a Airport
		var enabled int
		if err := rows.Scan(&a.ID, &a.Name, &a.URL, &a.Abbr, &enabled, &a.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan airport: %w", err)
		}
		a.Enabled = enabled == 1
		airports = append(airports, &a)
	}
	return airports, nil
}

// GetAirportByID 获取机场
func (s *Store) GetAirportByID(id int64) (*Airport, error) {
	var a Airport
	var enabled int
	err := s.db.QueryRow(
		`SELECT id, name, url, abbr, enabled, created_at FROM airports WHERE id = ?`, id).
		Scan(&a.ID, &a.Name, &a.URL, &a.Abbr, &enabled, &a.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query airport: %w", err)
	}
	a.Enabled = enabled == 1
	return &a, nil
}

// SetAirportEnabled 启用/禁用机场
func (s *Store) SetAirportEnabled(id int64, enabled bool) error {
	_, err := s.db.Exec(
		`UPDATE airports SET enabled = ? WHERE id = ?`,
		boolToInt(enabled), id)
	return err
}

// DeleteAirport 删除机场
func (s *Store) DeleteAirport(id int64) error {
	result, err := s.db.Exec(`DELETE FROM airports WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete airport: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

// CreateSelfHostedNode 添加自建节点
func (s *Store) CreateSelfHostedNode(node *SelfHostedNode) error {
	_, err := s.db.Exec(
		`INSERT INTO self_hosted_nodes
		(name, protocol, server, port, uuid, password, cipher, alter_id, network, tls, region_code, grpc_service_name, enabled)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		node.Name, node.Protocol, node.Server, node.Port,
		node.UUID, node.Password, node.Cipher, node.AlterID,
		node.Network, boolToInt(node.TLS), node.RegionCode, node.GrpcServiceName, boolToInt(node.Enabled))
	return err
}

// ListSelfHostedNodes 列出所有自建节点
func (s *Store) ListSelfHostedNodes() ([]*SelfHostedNode, error) {
	rows, err := s.db.Query(
		`SELECT id, name, protocol, server, port, uuid, password, cipher,
		alter_id, network, tls, region_code, grpc_service_name, enabled FROM self_hosted_nodes WHERE enabled = 1`)
	if err != nil {
		return nil, fmt.Errorf("query self hosted nodes: %w", err)
	}
	defer rows.Close()

	var nodes []*SelfHostedNode
	for rows.Next() {
		var n SelfHostedNode
		var tls, enabled int
		if err := rows.Scan(&n.ID, &n.Name, &n.Protocol, &n.Server, &n.Port,
			&n.UUID, &n.Password, &n.Cipher, &n.AlterID, &n.Network, &tls, &n.RegionCode,
			&n.GrpcServiceName, &enabled); err != nil {
			return nil, fmt.Errorf("scan node: %w", err)
		}
		n.TLS = tls == 1
		n.Enabled = enabled == 1
		nodes = append(nodes, &n)
	}
	return nodes, nil
}

// DeleteSelfHostedNode 删除自建节点
func (s *Store) DeleteSelfHostedNode(id int64) error {
	_, err := s.db.Exec(`DELETE FROM self_hosted_nodes WHERE id = ?`, id)
	return err
}

// GetSystemSettings 获取系统设置（JSON 格式）
func (s *Store) GetSystemSettings() (map[string]string, error) {
	rows, err := s.db.Query(`SELECT key, value FROM settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	settings := make(map[string]string)
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		settings[key] = value
	}
	return settings, nil
}

// SaveSystemSettings 批量保存系统设置
func (s *Store) SaveSystemSettings(settings map[string]string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO settings (key, value) VALUES (?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for k, v := range settings {
		if _, err := stmt.Exec(k, v); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// UpdateAirport 更新机场信息（含简称）
func (s *Store) UpdateAirport(id int64, name, url, abbr string) error {
	query := `UPDATE airports SET name = ?, url = ?, abbr = ? WHERE id = ?`
	_, err := s.db.Exec(query, name, url, abbr, id)
	if err != nil {
		return fmt.Errorf("update airport: %w", err)
	}
	return nil
}

// AirportAbbreviations 返回 机场名 → 简称 的映射,供节点名称标准化使用(见 ADR 0012)。
//
// 手动设置的简称(abbr 字段非空)优先占用;其余机场自动生成并去重,
// 避免与手动简称冲突。按 id 升序处理,保证分配稳定。
func (s *Store) AirportAbbreviations() (map[string]string, error) {
	rows, err := s.db.Query(`SELECT name, abbr FROM airports ORDER BY id ASC`)
	if err != nil {
		return nil, fmt.Errorf("query airport abbrs: %w", err)
	}
	defer rows.Close()

	type row struct{ name, abbr string }
	var all []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.name, &r.abbr); err != nil {
			return nil, err
		}
		all = append(all, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	result := make(map[string]string, len(all))
	used := make(map[string]bool)

	// 第一遍:手动简称占位
	for _, r := range all {
		if r.abbr != "" {
			result[r.name] = r.abbr
			used[r.abbr] = true
		}
	}
	// 第二遍:自动生成,避开已占用(与内存去重共享 NextFreeAbbr,避免两套逻辑漂移)
	for _, r := range all {
		if r.abbr != "" {
			continue
		}
		abbr := subscription.NextFreeAbbr(subscription.GenerateAbbreviation(r.name), used)
		used[abbr] = true
		result[r.name] = abbr
	}
	return result, nil
}

// IsSystemInitialized 检查系统是否已初始化
func (s *Store) IsSystemInitialized() (bool, error) {
	value, err := s.GetSetting("initialized")
	if err == ErrNotFound {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return value == "true", nil
}

// MarkSystemInitialized 标记系统已初始化
func (s *Store) MarkSystemInitialized() error {
	return s.SetSetting("initialized", "true")
}
