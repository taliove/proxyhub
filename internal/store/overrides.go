package store

import "time"

// NodeOverride 机场节点编辑覆盖层
type NodeOverride struct {
	NodeKey     string `json:"node_key"`
	DisplayName string `json:"display_name"`
	Region      string `json:"region"`
	// Favorite 节点收藏(issue #83):展示层星标,与名称/地区覆盖同表异列,互不影响。
	Favorite  bool      `json:"favorite"`
	UpdatedAt time.Time `json:"updated_at"`
}

// SetNodeOverride upsert 覆盖层（display_name/region 任一非空）。
// 等价于 SetNodeOverrideForUser(0, ...)(未归属桶,旧语义)。
func (s *Store) SetNodeOverride(nodeKey, displayName, region string) error {
	return s.SetNodeOverrideForUser(0, nodeKey, displayName, region)
}

// SetNodeOverrideForUser 与 SetNodeOverride 同语义,但按属主隔离(多租户):
// (user_id, node_key) 联合主键,同一节点可被不同用户独立覆盖。
func (s *Store) SetNodeOverrideForUser(userID int64, nodeKey, displayName, region string) error {
	_, err := s.db.Exec(`
		INSERT INTO node_overrides (user_id, node_key, display_name, region, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(user_id, node_key) DO UPDATE SET
			display_name = excluded.display_name,
			region = excluded.region,
			updated_at = excluded.updated_at
	`, userID, nodeKey, displayName, region, time.Now())
	return err
}

// SetNodeFavoriteForUser 设置/取消节点收藏(issue #83)。与 SetNodeOverrideForUser
// 同表异列:upsert 只触碰 favorite(及 updated_at),不清 display_name/region,
// 反之亦然——两条写路径互不覆盖对方字段。
func (s *Store) SetNodeFavoriteForUser(userID int64, nodeKey string, favorite bool) error {
	_, err := s.db.Exec(`
		INSERT INTO node_overrides (user_id, node_key, display_name, region, favorite, updated_at)
		VALUES (?, ?, '', '', ?, ?)
		ON CONFLICT(user_id, node_key) DO UPDATE SET
			favorite = excluded.favorite,
			updated_at = excluded.updated_at
	`, userID, nodeKey, boolToInt(favorite), time.Now())
	return err
}

// ListFavoriteNodeKeysForUser 读取指定用户已收藏节点的 NodeKey 集合(issue #83)。
// userID<=0 返回全量(超管跨用户视角)。只含 favorite=1 的行。
func (s *Store) ListFavoriteNodeKeysForUser(userID int64) (map[string]bool, error) {
	query := `SELECT node_key FROM node_overrides WHERE favorite != 0`
	args := []any{}
	if userID > 0 {
		query += ` AND user_id = ?`
		args = append(args, userID)
	}
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]bool)
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		result[key] = true
	}
	return result, rows.Err()
}

// ClearNodeOverride 删除覆盖层
func (s *Store) ClearNodeOverride(nodeKey string) error {
	return s.ClearNodeOverrideForUser(0, nodeKey)
}

// ClearNodeOverrideForUser 与 ClearNodeOverride 同语义,但只清该用户的覆盖行(多租户)。
// 收藏(issue #83)与名称/地区覆盖同表异列:已收藏的行只清空 display_name/region
// (保留收藏标记),未收藏的行才整行删除(旧语义)——否则"恢复默认名"会静默丢收藏。
func (s *Store) ClearNodeOverrideForUser(userID int64, nodeKey string) error {
	res, err := s.db.Exec(
		`DELETE FROM node_overrides WHERE user_id = ? AND node_key = ? AND favorite = 0`, userID, nodeKey)
	if err != nil {
		return err
	}
	if n, rerr := res.RowsAffected(); rerr == nil && n > 0 {
		return nil
	}
	_, err = s.db.Exec(
		`UPDATE node_overrides SET display_name = '', region = '', updated_at = ? WHERE user_id = ? AND node_key = ?`,
		time.Now(), userID, nodeKey)
	return err
}

// ListNodeOverrides 读取所有覆盖层。等价于 ListNodeOverridesForUser(0)(全量,旧语义)。
func (s *Store) ListNodeOverrides() (map[string]NodeOverride, error) {
	return s.ListNodeOverridesForUser(0)
}

// ListNodeOverridesForUser 读取指定用户的覆盖层(多租户);userID<=0 返回全量
// (超管跨用户视角或旧单用户语义)。
func (s *Store) ListNodeOverridesForUser(userID int64) (map[string]NodeOverride, error) {
	query := `SELECT node_key, display_name, region, favorite, updated_at FROM node_overrides`
	args := []any{}
	if userID > 0 {
		query += ` WHERE user_id = ?`
		args = append(args, userID)
	}
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]NodeOverride)
	for rows.Next() {
		var o NodeOverride
		var fav int
		var updatedStr string
		if err := rows.Scan(&o.NodeKey, &o.DisplayName, &o.Region, &fav, &updatedStr); err != nil {
			return nil, err
		}
		o.Favorite = fav != 0
		// 解析时间
		if t, perr := time.Parse("2006-01-02 15:04:05.999999999-07:00", updatedStr); perr == nil {
			o.UpdatedAt = t
		} else if t, perr := time.Parse(time.RFC3339, updatedStr); perr == nil {
			o.UpdatedAt = t
		}
		result[o.NodeKey] = o
	}
	return result, rows.Err()
}
