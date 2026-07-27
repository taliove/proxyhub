package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
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
	// UserID 属主(ticket 06/07);0 = 未归属(历史数据桶,迁移后由超管认领)。
	UserID int64 `json:"user_id,omitempty"`

	// 节点名称标准化的按端点覆盖(见 ADR 0012):
	// NameMode 空=跟随全局,"on"/"off"=强制;NameTemplate 空=用全局模板。
	NameMode     string `json:"name_mode"`
	NameTemplate string `json:"name_template"`

	// Conditions 节点范围筛选条件的原始 JSON(见 internal/subfilter.Conditions)。
	// 空串=不筛选=全量(现状行为)。store 只存原始串,谓词语义由 subfilter 解释。
	Conditions string `json:"conditions"`
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

// endpointColumns 查询列表共用的列清单(ticket 07 起带 user_id,读取侧三处保持一致)。
const endpointColumns = `id, alias, path, token, enabled, created_at, name_mode, name_template, conditions, user_id`

// CreateEndpoint 创建订阅地址（随机 Path + Token）。
// 未指定属主(旧调用/直接库调用)时归一到首个 super_admin(ticket 07 Invariant B);
// 没有超管(初始化前)才落未归属桶 0,与 CreateAirport/CreateSelfHostedNode 同策。
func (s *Store) CreateEndpoint(alias string) (*Endpoint, error) {
	return s.CreateEndpointForUser(s.defaultOwnerUserID(0), alias)
}

// CreateEndpointForUser 创建归属指定用户的订阅地址(ticket 07)。userID=0 保留旧行为(未归属)。
func (s *Store) CreateEndpointForUser(userID int64, alias string) (*Endpoint, error) {
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
		`INSERT INTO endpoints (alias, path, token, user_id) VALUES (?, ?, ?, ?)`,
		alias, path, token, userID,
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
		`SELECT `+endpointColumns+` FROM endpoints WHERE id = ?`, id,
	))
}

// GetEndpointByIDForUser 按 ID 查询且校验属主(ticket 07):行存在但归属他人时同样返回
// ErrNotFound(不暴露存在性)。userID=0 = 全局视角(测试逃生舱/超管未切换时使用),属主校验跳过。
func (s *Store) GetEndpointByIDForUser(userID, id int64) (*Endpoint, error) {
	if userID == 0 {
		return s.GetEndpointByID(id)
	}
	return s.scanEndpoint(s.db.QueryRow(
		`SELECT `+endpointColumns+` FROM endpoints WHERE id = ? AND user_id = ?`, id, userID,
	))
}

// GetEndpointByPath 按随机 Path 查询（订阅请求入口用）
func (s *Store) GetEndpointByPath(path string) (*Endpoint, error) {
	return s.scanEndpoint(s.db.QueryRow(
		`SELECT `+endpointColumns+` FROM endpoints WHERE path = ?`, path,
	))
}

// ListEndpoints 列出所有订阅地址(跨用户,全量视角;
// 按用户过滤走 ListEndpointsByUser)。
//
// 剩余调用者仅限:测试代码与级联删除内部路径;生产 handler 已改用 ByUser 版本。
func (s *Store) ListEndpoints() ([]*Endpoint, error) {
	rows, err := s.db.Query(
		`SELECT ` + endpointColumns + ` FROM endpoints ORDER BY id`,
	)
	if err != nil {
		return nil, fmt.Errorf("query endpoints: %w", err)
	}
	defer rows.Close()

	return scanEndpoints(rows)
}

// ListEndpointsByUser 列出指定用户名下的订阅地址(ticket 06 新增接口;
// 不影响 ListEndpoints 的全量语义)。userID 无匹配行时返回空切片。
func (s *Store) ListEndpointsByUser(userID int64) ([]*Endpoint, error) {
	rows, err := s.db.Query(
		`SELECT `+endpointColumns+` FROM endpoints WHERE user_id = ? ORDER BY id`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("query endpoints by user: %w", err)
	}
	defer rows.Close()

	return scanEndpoints(rows)
}

// scanEndpoints 是 ListEndpoints/ListEndpointsByUser 共用的行扫描器。
func scanEndpoints(rows *sql.Rows) ([]*Endpoint, error) {
	var endpoints []*Endpoint
	for rows.Next() {
		ep, err := scanEndpointFrom(rows)
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

// SetEndpointEnabledForUser 按属主启用/禁用订阅地址(ticket 07);行属他人时 ErrNotFound。
// userID=0 = 全局视角(测试逃生舱/超管未切换时使用),属主校验跳过。
func (s *Store) SetEndpointEnabledForUser(userID, id int64, enabled bool) error {
	if userID == 0 {
		return s.SetEndpointEnabled(id, enabled)
	}
	res, err := s.db.Exec(`UPDATE endpoints SET enabled = ? WHERE id = ? AND user_id = ?`, boolToInt(enabled), id, userID)
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

// UpdateEndpointNameConfigForUser 按属主更新名称配置(ticket 07);行属他人时 ErrNotFound。
func (s *Store) UpdateEndpointNameConfigForUser(userID, id int64, mode, template string) error {
	if userID == 0 {
		return s.UpdateEndpointNameConfig(id, mode, template)
	}
	switch mode {
	case NameModeInherit, NameModeOn, NameModeOff:
	default:
		return fmt.Errorf("invalid name_mode %q", mode)
	}
	res, err := s.db.Exec(
		`UPDATE endpoints SET name_mode = ?, name_template = ? WHERE id = ? AND user_id = ?`,
		mode, template, id, userID)
	if err != nil {
		return fmt.Errorf("update endpoint name config: %w", err)
	}
	return checkAffected(res)
}

// UpdateEndpointConditions 设置订阅地址的节点范围筛选条件(见 internal/subfilter)。
// conditions 为原始 JSON;空串表示清空(回到全量)。非空时校验为合法 JSON,
// 在边界处 fail fast,不让脏数据落库(谓词语义解释仍归 subfilter,store 不解释内容)。
func (s *Store) UpdateEndpointConditions(id int64, conditions string) error {
	if conditions != "" && !json.Valid([]byte(conditions)) {
		return fmt.Errorf("invalid conditions json")
	}
	res, err := s.db.Exec(`UPDATE endpoints SET conditions = ? WHERE id = ?`, conditions, id)
	if err != nil {
		return fmt.Errorf("update endpoint conditions: %w", err)
	}
	return checkAffected(res)
}

// UpdateEndpointConditionsForUser 按属主更新筛选条件(ticket 07);行属他人时 ErrNotFound。
func (s *Store) UpdateEndpointConditionsForUser(userID, id int64, conditions string) error {
	if userID == 0 {
		return s.UpdateEndpointConditions(id, conditions)
	}
	if conditions != "" && !json.Valid([]byte(conditions)) {
		return fmt.Errorf("invalid conditions json")
	}
	res, err := s.db.Exec(`UPDATE endpoints SET conditions = ? WHERE id = ? AND user_id = ?`, conditions, id, userID)
	if err != nil {
		return fmt.Errorf("update endpoint conditions: %w", err)
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

// DeleteEndpointForUser 按属主删除订阅地址(ticket 07);行属他人时 ErrNotFound。
func (s *Store) DeleteEndpointForUser(userID, id int64) error {
	if userID == 0 {
		return s.DeleteEndpoint(id)
	}
	res, err := s.db.Exec(`DELETE FROM endpoints WHERE id = ? AND user_id = ?`, id, userID)
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
		&ep.NameMode, &ep.NameTemplate, &ep.Conditions, &ep.UserID); err != nil {
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
