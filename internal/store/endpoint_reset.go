package store

import (
	"fmt"
	"time"
)

// graceTimeLayout 宽限到期时间的存储布局(UTC 文本,与 banned_ips 归一后的
// 格式一致,ADR 0010;空串 = 无宽限)。
const graceTimeLayout = "2006-01-02 15:04:05"

// graceDuration 默认宽限期(issue #117):重置后旧链接继续可用 3 天;
// 每次手动延长同样 +3 天。
const graceDuration = 72 * time.Hour

// ResetEndpointLinkForUser 原位轮换订阅链接(issue #117):重新生成 path+token,
// 旧对移入 prev 槽位并开启 3 天宽限;端点上的全部配置(筛选/精选/模板/地域)不变。
// 再次重置:prev 槽位被覆盖,只保留一代宽限,不叠加。
// 行属他人时 ErrNotFound;userID=0 跳过属主校验(管理端代重置路径)。
func (s *Store) ResetEndpointLinkForUser(userID, id int64) (*Endpoint, error) {
	path, err := randomHex(8)
	if err != nil {
		return nil, err
	}
	token, err := randomHex(16)
	if err != nil {
		return nil, err
	}
	grace := time.Now().UTC().Add(graceDuration).Format(graceTimeLayout)

	query := `UPDATE endpoints
		SET prev_path = path, prev_token = token, grace_expires_at = ?, path = ?, token = ?
		WHERE id = ?`
	args := []any{grace, path, token, id}
	if userID != 0 {
		query += ` AND user_id = ?`
		args = append(args, userID)
	}
	res, err := s.db.Exec(query, args...)
	if err != nil {
		return nil, fmt.Errorf("reset endpoint link: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return nil, err
	} else if n == 0 {
		return nil, ErrNotFound
	}
	return s.GetEndpointByID(id)
}

// ExtendEndpointGraceForUser 延长宽限 +3 天(issue #117):自当前到期时间累加。
// 宽限不存在(从未重置)或已过期(不可复活)时 ErrNotFound,对外与"端点不存在"
// 无差别。行属他人时 ErrNotFound;userID=0 跳过属主校验。
func (s *Store) ExtendEndpointGraceForUser(userID, id int64) (*Endpoint, error) {
	ep, err := s.GetEndpointByIDForUser(userID, id)
	if err != nil {
		return nil, err
	}
	expiry, alive := ep.graceExpiry()
	if !alive || !expiry.After(time.Now().UTC()) {
		return nil, ErrNotFound
	}
	next := expiry.Add(graceDuration).Format(graceTimeLayout)
	// UPDATE 同样带属主(userID≠0 时),消除读改之间的 TOCTOU 缝隙。
	query := `UPDATE endpoints SET grace_expires_at = ? WHERE id = ?`
	args := []any{next, id}
	if userID != 0 {
		query += ` AND user_id = ?`
		args = append(args, userID)
	}
	res, err := s.db.Exec(query, args...)
	if err != nil {
		return nil, fmt.Errorf("extend endpoint grace: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return nil, err
	} else if n == 0 {
		return nil, ErrNotFound
	}
	return s.GetEndpointByID(id)
}

// GetEndpointByPrevPath 按 prev_path 反查端点(宽限期内的旧链接,issue #117)。
// 查不到时 ErrNotFound,与 GetEndpointByPath 的未知路径不可区分。
func (s *Store) GetEndpointByPrevPath(prevPath string) (*Endpoint, error) {
	return s.scanEndpoint(s.db.QueryRow(
		`SELECT `+endpointColumns+` FROM endpoints WHERE prev_path = ? AND prev_path != ''`, prevPath))
}

// graceExpiry 解析宽限到期时间;无宽限返回零值+false。
func (e *Endpoint) graceExpiry() (time.Time, bool) {
	if e.GraceExpiresAt == "" {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation(graceTimeLayout, e.GraceExpiresAt, time.UTC)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// GraceAlive 宽限是否存活(供订阅校验链:prev 命中后判 old link 是否仍可下发)。
func (e *Endpoint) GraceAlive(now time.Time) bool {
	expiry, ok := e.graceExpiry()
	return ok && expiry.After(now)
}
