package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// ErrNotFound 记录不存在
var ErrNotFound = errors.New("not found")

// NameMode 订阅地址对节点名称标准化的覆盖模式(见 ADR 0012)。
const (
	NameModeInherit = ""    // 跟随全局设置
	NameModeOn      = "on"  // 强制开启标准化
	NameModeOff     = "off" // 强制关闭标准化(用原名)
)

// Endpoint 订阅地址
type Endpoint struct {
	ID        int64     `json:"id"`
	Alias     string    `json:"alias"`
	Path      string    `json:"path"`
	Token     string    `json:"token"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`

	// 节点名称标准化的按端点覆盖(见 ADR 0012):
	// NameMode 空=跟随全局,"on"/"off"=强制;NameTemplate 空=用全局模板。
	NameMode     string `json:"name_mode"`
	NameTemplate string `json:"name_template"`
}

// URL 返回订阅地址的相对路径
func (e *Endpoint) URL() string {
	return fmt.Sprintf("/sub/%s?token=%s", e.Path, e.Token)
}

// randomHex 生成加密安全的随机十六进制字符串
func randomHex(bytes int) (string, error) {
	b := make([]byte, bytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate random: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// CreateEndpoint 创建订阅地址（随机 Path + Token）
func (s *Store) CreateEndpoint(alias string) (*Endpoint, error) {
	if alias == "" {
		return nil, errors.New("alias is required")
	}

	path, err := randomHex(8) // 16 字符
	if err != nil {
		return nil, err
	}
	token, err := randomHex(16) // 32 字符
	if err != nil {
		return nil, err
	}

	res, err := s.db.Exec(
		`INSERT INTO endpoints (alias, path, token) VALUES (?, ?, ?)`,
		alias, path, token,
	)
	if err != nil {
		return nil, fmt.Errorf("insert endpoint: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("get endpoint id: %w", err)
	}

	return s.GetEndpointByID(id)
}

// GetEndpointByID 按 ID 查询
func (s *Store) GetEndpointByID(id int64) (*Endpoint, error) {
	return s.scanEndpoint(s.db.QueryRow(
		`SELECT id, alias, path, token, enabled, created_at, name_mode, name_template FROM endpoints WHERE id = ?`, id,
	))
}

// GetEndpointByPath 按随机 Path 查询（订阅请求入口用）
func (s *Store) GetEndpointByPath(path string) (*Endpoint, error) {
	return s.scanEndpoint(s.db.QueryRow(
		`SELECT id, alias, path, token, enabled, created_at, name_mode, name_template FROM endpoints WHERE path = ?`, path,
	))
}

// ListEndpoints 列出所有订阅地址
func (s *Store) ListEndpoints() ([]*Endpoint, error) {
	rows, err := s.db.Query(
		`SELECT id, alias, path, token, enabled, created_at, name_mode, name_template FROM endpoints ORDER BY id`,
	)
	if err != nil {
		return nil, fmt.Errorf("query endpoints: %w", err)
	}
	defer rows.Close()

	var endpoints []*Endpoint
	for rows.Next() {
		ep, err := s.scanEndpointRow(rows)
		if err != nil {
			return nil, err
		}
		endpoints = append(endpoints, ep)
	}
	return endpoints, rows.Err()
}

// SetEndpointEnabled 启用/禁用订阅地址
func (s *Store) SetEndpointEnabled(id int64, enabled bool) error {
	res, err := s.db.Exec(`UPDATE endpoints SET enabled = ? WHERE id = ?`, boolToInt(enabled), id)
	if err != nil {
		return fmt.Errorf("update endpoint: %w", err)
	}
	return checkAffected(res)
}

// UpdateEndpointAlias 修改别名
func (s *Store) UpdateEndpointAlias(id int64, alias string) error {
	if alias == "" {
		return errors.New("alias is required")
	}
	res, err := s.db.Exec(`UPDATE endpoints SET alias = ? WHERE id = ?`, alias, id)
	if err != nil {
		return fmt.Errorf("update endpoint alias: %w", err)
	}
	return checkAffected(res)
}

// UpdateEndpointNameConfig 设置订阅地址的节点名称标准化覆盖(见 ADR 0012)。
// mode 必须是 NameModeInherit/On/Off 之一;template 空表示用全局模板。
func (s *Store) UpdateEndpointNameConfig(id int64, mode, template string) error {
	switch mode {
	case NameModeInherit, NameModeOn, NameModeOff:
	default:
		return fmt.Errorf("invalid name_mode %q", mode)
	}
	res, err := s.db.Exec(
		`UPDATE endpoints SET name_mode = ?, name_template = ? WHERE id = ?`,
		mode, template, id)
	if err != nil {
		return fmt.Errorf("update endpoint name config: %w", err)
	}
	return checkAffected(res)
}

// DeleteEndpoint 删除订阅地址
func (s *Store) DeleteEndpoint(id int64) error {
	res, err := s.db.Exec(`DELETE FROM endpoints WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete endpoint: %w", err)
	}
	return checkAffected(res)
}

type rowScanner interface {
	Scan(dest ...any) error
}

func (s *Store) scanEndpoint(row *sql.Row) (*Endpoint, error) {
	ep, err := scanEndpointFrom(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return ep, err
}

func (s *Store) scanEndpointRow(rows *sql.Rows) (*Endpoint, error) {
	return scanEndpointFrom(rows)
}

func scanEndpointFrom(r rowScanner) (*Endpoint, error) {
	var ep Endpoint
	var enabled int
	if err := r.Scan(&ep.ID, &ep.Alias, &ep.Path, &ep.Token, &enabled, &ep.CreatedAt,
		&ep.NameMode, &ep.NameTemplate); err != nil {
		return nil, err
	}
	ep.Enabled = enabled != 0
	return &ep, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func checkAffected(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
