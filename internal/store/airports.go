package store

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/taliove/proxyhub/internal/subscription"
)

// 机场来源类型(见 CONTEXT.md「手动机场」与 ADR 0034)。
const (
	// AirportSourceURL 拉取型机场:从订阅 URL 拉取节点(默认,历史行迁移后落此值)。
	AirportSourceURL = "url"
	// AirportSourceManual 手动机场:无订阅 URL(url 列存空串),
	// 节点由用户粘贴订阅导出内容导入;定时/全量刷新跳过,清空豁免。
	AirportSourceManual = "manual"
)

// Airport 机场订阅
type Airport struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	Abbr      string    `json:"abbr"` // 机场简称,空表示自动生成(见 ADR 0012)
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	// UserID 属主(ticket 06/07);0 = 未归属(历史数据桶,迁移后由超管认领)。
	UserID int64 `json:"user_id,omitempty"`
	// SourceType 来源类型:AirportSourceURL(默认)/ AirportSourceManual。
	SourceType string `json:"source_type"`
	// 用量信息(CONTEXT.md「用量信息」;全部可选,零值 = 未知不展示):
	// 拉取型每次拉取从 subscription-userinfo / profile-web-page-url 响应头捕获覆盖;
	// 手动型由用户在粘贴导入/编辑时手填(剩余换算为 download 落库,上行未知计 0)。
	UsageUpload   int64  `json:"usage_upload"`
	UsageDownload int64  `json:"usage_download"`
	UsageTotal    int64  `json:"usage_total"`
	UsageExpire   int64  `json:"usage_expire"` // unix 秒;0 = 未知
	WebPageURL    string `json:"web_page_url"`
}

// SelfHostedNode 自建节点
type SelfHostedNode struct {
	ID              int64  `json:"id"`
	Name            string `json:"name"`
	Protocol        string `json:"protocol"`
	Server          string `json:"server"`
	Port            int    `json:"port"`
	UUID            string `json:"uuid"`
	Password        string `json:"password"`
	Cipher          string `json:"cipher"`
	AlterID         int    `json:"alter_id"`
	Network         string `json:"network"`
	TLS             bool   `json:"tls"`
	RegionCode      string `json:"region_code"`
	GrpcServiceName string `json:"grpc_service_name"`
	// GrpcAuthority gRPC authority(spec #72):vless/vmess over grpc 自建节点可填;
	// 空 = 无 authority(多数部署不需要)。
	GrpcAuthority string `json:"grpc_authority"`
	Enabled         bool   `json:"enabled"`
	// UserID 属主(ticket 06/07);0 = 未归属(历史数据桶,迁移后由超管认领)。
	UserID int64 `json:"user_id,omitempty"`
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
		GrpcAuthority:   n.GrpcAuthority,
		Region:          region,
		Source:          subscription.SourceSelfHosted,
		Available:       true,
	}
}

// CreateAirport 添加机场。
// 未指定属主(旧调用/直接库调用)时归一到首个 super_admin(ticket 07 Invariant B);
// 没有超管(初始化前)才落未归属桶 0。
func (s *Store) CreateAirport(name, url string) (*Airport, error) {
	return s.CreateAirportForUser(s.defaultOwnerUserID(0), name, url)
}

// defaultOwnerUserID 归一属主:rowUserID>0 直接用;=0 回退首个 super_admin;
// 无用户时保持 0(未归属桶)。
func (s *Store) defaultOwnerUserID(rowUserID int64) int64 {
	if rowUserID > 0 {
		return rowUserID
	}
	users, err := s.ListUsers()
	if err != nil {
		return 0
	}
	for _, u := range users {
		if u.Role == RoleSuperAdmin {
			return u.ID
		}
	}
	return 0
}

// CreateAirportForUser 创建归属指定用户的拉取型机场(ticket 07)。userID=0 保留旧行为(未归属)。
func (s *Store) CreateAirportForUser(userID int64, name, url string) (*Airport, error) {
	return s.createAirportForUser(userID, name, url, AirportSourceURL)
}

// CreateManualAirportForUser 创建手动机场(url 列存空串 + source_type=manual,见 ADR 0034)。
// 节点不入库——创建后由粘贴导入端点显式 upsert(凭证红线:粘贴内容不走 jobs params)。
func (s *Store) CreateManualAirportForUser(userID int64, name string) (*Airport, error) {
	return s.createAirportForUser(userID, name, "", AirportSourceManual)
}

func (s *Store) createAirportForUser(userID int64, name, url, sourceType string) (*Airport, error) {
	result, err := s.db.Exec(
		`INSERT INTO airports (name, url, user_id, source_type) VALUES (?, ?, ?, ?)`,
		name, url, userID, sourceType)
	if err != nil {
		return nil, fmt.Errorf("insert airport: %w", err)
	}

	id, _ := result.LastInsertId()
	return &Airport{
		ID:         id,
		Name:       name,
		URL:        url,
		Enabled:    true,
		CreatedAt:  time.Now(),
		UserID:     userID,
		SourceType: sourceType,
	}, nil
}

// airportColumns 机场查询共用的列清单(ticket 07 起带 user_id;
// spec-manual-airport-import 起带 source_type 与用量信息列)。
const airportColumns = `id, name, url, abbr, enabled, created_at, user_id,
	source_type, usage_upload, usage_download, usage_total, usage_expire, web_page_url`

// scanAirport 扫描一行机场(airportColumns 列序的唯一事实源)。
func scanAirport(row interface{ Scan(...any) error }, a *Airport) error {
	var enabled int
	if err := row.Scan(&a.ID, &a.Name, &a.URL, &a.Abbr, &enabled, &a.CreatedAt, &a.UserID,
		&a.SourceType, &a.UsageUpload, &a.UsageDownload, &a.UsageTotal, &a.UsageExpire, &a.WebPageURL); err != nil {
		return err
	}
	a.Enabled = enabled == 1
	return nil
}

// ListAirports 列出所有机场(跨用户,全量视角;
// 按用户过滤走 ListAirportsByUser)。
//
// 剩余调用者仅限:(a) 聚合器全量刷新分支(airportOwnerID=0 时);
// (b) 测试代码 / 级联删除内部路径。生产 handler 已改用 ByUser 版本。
func (s *Store) ListAirports() ([]*Airport, error) {
	rows, err := s.db.Query(
		`SELECT ` + airportColumns + ` FROM airports ORDER BY id DESC`)
	if err != nil {
		return nil, fmt.Errorf("query airports: %w", err)
	}
	defer rows.Close()

	return scanAirports(rows)
}

// ListAirportsByUser 列出指定用户名下的机场(ticket 06 新增接口;
// 不影响 ListAirports 的全量语义)。userID 无匹配行时返回空切片。
func (s *Store) ListAirportsByUser(userID int64) ([]*Airport, error) {
	rows, err := s.db.Query(
		`SELECT `+airportColumns+` FROM airports WHERE user_id = ? ORDER BY id DESC`,
		userID)
	if err != nil {
		return nil, fmt.Errorf("query airports by user: %w", err)
	}
	defer rows.Close()

	return scanAirports(rows)
}

// scanAirports 是 ListAirports/ListAirportsByUser 共用的行扫描器。
func scanAirports(rows *sql.Rows) ([]*Airport, error) {
	var airports []*Airport
	for rows.Next() {
		var a Airport
		if err := scanAirport(rows, &a); err != nil {
			return nil, fmt.Errorf("scan airport: %w", err)
		}
		airports = append(airports, &a)
	}
	return airports, rows.Err()
}

// GetAirportByID 获取机场
func (s *Store) GetAirportByID(id int64) (*Airport, error) {
	var a Airport
	err := scanAirport(s.db.QueryRow(
		`SELECT `+airportColumns+` FROM airports WHERE id = ?`, id), &a)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query airport: %w", err)
	}
	return &a, nil
}

// GetAirportByIDForUser 按 ID 查询且校验属主(ticket 07):行属他人时同样 ErrNotFound。
// userID=0 = 全局视角(测试逃生舱/超管未切换时使用),属主校验跳过。
func (s *Store) GetAirportByIDForUser(userID, id int64) (*Airport, error) {
	if userID == 0 {
		return s.GetAirportByID(id)
	}
	var a Airport
	err := scanAirport(s.db.QueryRow(
		`SELECT `+airportColumns+` FROM airports WHERE id = ? AND user_id = ?`, id, userID), &a)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query airport: %w", err)
	}
	return &a, nil
}

// UpdateAirportUsage 覆盖更新机场用量信息(拉取捕获路径,spec-manual-airport-import)。
// WebPageURL 为空时保留既有值——响应头缺 profile-web-page-url 不应抹掉用户手填的官网。
// 手动填写路径用 SetAirportUsageForUser(空官网 = 显式清空,不保留)。
// 官网非 http/https 时归一为空串(scheme 白名单,XSS 防线,见 subscription.SanitizeWebPageURL)。
func (s *Store) UpdateAirportUsage(id int64, u *subscription.UsageInfo) error {
	if u == nil {
		return nil
	}
	webPageURL := subscription.SanitizeWebPageURL(u.WebPageURL)
	if webPageURL == "" {
		_, err := s.db.Exec(
			`UPDATE airports SET usage_upload = ?, usage_download = ?, usage_total = ?, usage_expire = ? WHERE id = ?`,
			u.Upload, u.Download, u.Total, u.Expire, id)
		return err
	}
	_, err := s.db.Exec(
		`UPDATE airports SET usage_upload = ?, usage_download = ?, usage_total = ?, usage_expire = ?, web_page_url = ? WHERE id = ?`,
		u.Upload, u.Download, u.Total, u.Expire, webPageURL, id)
	return err
}

// SetAirportUsageForUser 按属主全量覆写用量信息(手动填写路径:空值 = 显式清空,
// 不做拉取路径的官网保留)。行属他人时 ErrNotFound;userID=0 跳过属主校验。
// 官网非 http/https 时归一为空串(scheme 白名单,XSS 防线)。
func (s *Store) SetAirportUsageForUser(userID, id int64, u *subscription.UsageInfo) error {
	if u == nil {
		return nil
	}
	if userID > 0 {
		if _, err := s.GetAirportByIDForUser(userID, id); err != nil {
			return err
		}
	}
	_, err := s.db.Exec(
		`UPDATE airports SET usage_upload = ?, usage_download = ?, usage_total = ?, usage_expire = ?, web_page_url = ? WHERE id = ?`,
		u.Upload, u.Download, u.Total, u.Expire, subscription.SanitizeWebPageURL(u.WebPageURL), id)
	return err
}

// SetAirportWebPageURLForUser 按属主只覆写官网地址(拉取型机场手填路径:
// 用量列由订阅响应头自动捕获,绝不能随官网一并覆写,故独立成方法)。
// 空串 = 显式清空;非 http/https 归一为空串(scheme 白名单,XSS 防线)。
// 行属他人时 ErrNotFound;userID=0 跳过属主校验。
func (s *Store) SetAirportWebPageURLForUser(userID, id int64, url string) error {
	if userID > 0 {
		if _, err := s.GetAirportByIDForUser(userID, id); err != nil {
			return err
		}
	}
	_, err := s.db.Exec(
		`UPDATE airports SET web_page_url = ? WHERE id = ?`,
		subscription.SanitizeWebPageURL(url), id)
	return err
}

// ManualAirportNames 返回手动机场名集合(来源匹配键)。userID>0 限定该用户名下,
// =0 跨用户全量(机场节点清空豁免手动机场节点用,无 URL 可拉,清空后永不回来)。
func (s *Store) ManualAirportNames(userID int64) (map[string]bool, error) {
	query := `SELECT name FROM airports WHERE source_type = ?`
	args := []any{AirportSourceManual}
	if userID > 0 {
		query += ` AND user_id = ?`
		args = append(args, userID)
	}
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query manual airport names: %w", err)
	}
	defer rows.Close()
	names := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names[name] = true
	}
	return names, rows.Err()
}

// SetAirportEnabled 启用/禁用机场
func (s *Store) SetAirportEnabled(id int64, enabled bool) error {
	_, err := s.db.Exec(
		`UPDATE airports SET enabled = ? WHERE id = ?`,
		boolToInt(enabled), id)
	return err
}

// SetAirportEnabledForUser 按属主启用/禁用机场(ticket 07);行属他人时 ErrNotFound。
// userID=0 = 全局视角(测试逃生舱/超管未切换时使用),属主校验跳过。
func (s *Store) SetAirportEnabledForUser(userID, id int64, enabled bool) error {
	if userID == 0 {
		return s.SetAirportEnabled(id, enabled)
	}
	res, err := s.db.Exec(
		`UPDATE airports SET enabled = ? WHERE id = ? AND user_id = ?`,
		boolToInt(enabled), id, userID)
	if err != nil {
		return err
	}
	return checkAffected(res)
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

// DeleteAirportForUser 按属主删除机场(ticket 07);行属他人时 ErrNotFound。
// userID=0 = 全局视角(测试逃生舱/超管未切换时使用),属主校验跳过。
func (s *Store) DeleteAirportForUser(userID, id int64) error {
	if userID == 0 {
		return s.DeleteAirport(id)
	}
	result, err := s.db.Exec(`DELETE FROM airports WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return fmt.Errorf("delete airport: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateAirportForUser 按属主更新机场(ticket 07);行属他人时 ErrNotFound。
// userID=0 = 全局视角(测试逃生舱/超管未切换时使用),属主校验跳过。
func (s *Store) UpdateAirportForUser(userID, id int64, name, url, abbr string) error {
	if userID == 0 {
		return s.UpdateAirport(id, name, url, abbr)
	}
	res, err := s.db.Exec(
		`UPDATE airports SET name = ?, url = ?, abbr = ? WHERE id = ? AND user_id = ?`,
		name, url, abbr, id, userID)
	if err != nil {
		return fmt.Errorf("update airport: %w", err)
	}
	return checkAffected(res)
}

// CreateSelfHostedNode 添加自建节点
// CreateSelfHostedNode 添加自建节点。
// 未指定属主(旧调用/直接库调用)时归一到首个 super_admin(ticket 07 Invariant B):
	// 没有超管(初始化前)才落未归属桶 0,避免后台属主校验把孤儿行过滤掉。
func (s *Store) CreateSelfHostedNode(node *SelfHostedNode) error {
	return s.CreateSelfHostedNodeForUser(s.defaultOwnerUserID(node.UserID), node)
}

// CreateSelfHostedNodeForUser 创建归属指定用户的自建节点(ticket 07)。
// userID=0 保留旧行为(未归属)。撞身份唯一约束(023)返回 ErrDuplicateIdentity。
func (s *Store) CreateSelfHostedNodeForUser(userID int64, node *SelfHostedNode) error {
	_, err := s.db.Exec(
		`INSERT INTO self_hosted_nodes
		(name, protocol, server, port, uuid, password, cipher, alter_id, network, tls, region_code, grpc_service_name, grpc_authority, enabled, user_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		node.Name, node.Protocol, node.Server, node.Port,
		node.UUID, node.Password, node.Cipher, node.AlterID,
		node.Network, boolToInt(node.TLS), node.RegionCode, node.GrpcServiceName, node.GrpcAuthority, boolToInt(node.Enabled), userID)
	return mapIdentityViolation(err)
}

// selfHostedColumns 自建节点查询共用列清单(ticket 07 起带 user_id;
// spec #72 起带 grpc_authority)。
const selfHostedColumns = `id, name, protocol, server, port, uuid, password, cipher,
		alter_id, network, tls, region_code, grpc_service_name, grpc_authority, enabled, user_id`

// ListSelfHostedNodes 列出所有自建节点(跨用户,全量视角;
// 按用户过滤走 ListSelfHostedNodesByUser)。
//
// 剩余调用者仅限:(a) 聚合器全量刷新分支(airportOwnerID=0 时);
// (b) 测试代码 / 级联删除内部路径。生产 handler 已改用 ByUser 版本。
func (s *Store) ListSelfHostedNodes() ([]*SelfHostedNode, error) {
	rows, err := s.db.Query(
		`SELECT ` + selfHostedColumns + ` FROM self_hosted_nodes WHERE enabled = 1`)
	if err != nil {
		return nil, fmt.Errorf("query self hosted nodes: %w", err)
	}
	defer rows.Close()

	return scanSelfHostedNodes(rows)
}

// ListSelfHostedNodesByUser 列出指定用户名下的启用自建节点(ticket 06 新增接口;
// 与 ListSelfHostedNodes 同样只返回 enabled=1 的行)。
func (s *Store) ListSelfHostedNodesByUser(userID int64) ([]*SelfHostedNode, error) {
	rows, err := s.db.Query(
		`SELECT `+selfHostedColumns+` FROM self_hosted_nodes WHERE enabled = 1 AND user_id = ?`,
		userID)
	if err != nil {
		return nil, fmt.Errorf("query self hosted nodes by user: %w", err)
	}
	defer rows.Close()

	return scanSelfHostedNodes(rows)
}

// scanSelfHostedNodes 是 ListSelfHostedNodes/ListSelfHostedNodesByUser 共用的行扫描器。
func scanSelfHostedNodes(rows *sql.Rows) ([]*SelfHostedNode, error) {
	var nodes []*SelfHostedNode
	for rows.Next() {
		var n SelfHostedNode
		var tls, enabled int
		if err := rows.Scan(&n.ID, &n.Name, &n.Protocol, &n.Server, &n.Port,
			&n.UUID, &n.Password, &n.Cipher, &n.AlterID, &n.Network, &tls, &n.RegionCode,
			&n.GrpcServiceName, &n.GrpcAuthority, &enabled, &n.UserID); err != nil {
			return nil, fmt.Errorf("scan node: %w", err)
		}
		n.TLS = tls == 1
		n.Enabled = enabled == 1
		nodes = append(nodes, &n)
	}
	return nodes, rows.Err()
}

// DeleteSelfHostedNode 删除自建节点
func (s *Store) DeleteSelfHostedNode(id int64) error {
	_, err := s.db.Exec(`DELETE FROM self_hosted_nodes WHERE id = ?`, id)
	return err
}

// DeleteSelfHostedNodeForUser 按属主删除自建节点(ticket 07);行属他人时 ErrNotFound。
func (s *Store) DeleteSelfHostedNodeForUser(userID, id int64) error {
	if userID == 0 {
		return s.DeleteSelfHostedNode(id)
	}
	res, err := s.db.Exec(`DELETE FROM self_hosted_nodes WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	return checkAffected(res)
}

// GetSystemSettings 获取系统设置（JSON 格式）。读 system_settings(ticket 06
// settings 拆分后的全局作用域;遗留 settings 表仅作回滚备份)。
func (s *Store) GetSystemSettings() (map[string]string, error) {
	rows, err := s.db.Query(`SELECT key, value FROM system_settings`)
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

// SaveSystemSettings 批量保存系统设置(写 system_settings,见 GetSystemSettings)。
func (s *Store) SaveSystemSettings(settings map[string]string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO system_settings (key, value) VALUES (?, ?)`)
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

// GetUsedAbbrs returns all abbreviations currently in use by airports,
// excluding the given ID (for update case). Returns a set map.
func (s *Store) GetUsedAbbrs(excludeID int64) (map[string]bool, error) {
	rows, err := s.db.Query(`SELECT abbr FROM airports WHERE id != ? AND abbr != ''`, excludeID)
	if err != nil {
		return nil, fmt.Errorf("query used abbrs: %w", err)
	}
	defer rows.Close()

	used := make(map[string]bool)
	for rows.Next() {
		var abbr string
		if err := rows.Scan(&abbr); err != nil {
			return nil, err
		}
		used[abbr] = true
	}
	return used, rows.Err()
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
