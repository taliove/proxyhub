package store

import "time"

// NodeOverride 机场节点编辑覆盖层
type NodeOverride struct {
	NodeKey     string    `json:"node_key"`
	DisplayName string    `json:"display_name"`
	Region      string    `json:"region"`
	UpdatedAt   time.Time `json:"updated_at"`
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

// ClearNodeOverride 删除覆盖层
func (s *Store) ClearNodeOverride(nodeKey string) error {
	return s.ClearNodeOverrideForUser(0, nodeKey)
}

// ClearNodeOverrideForUser 与 ClearNodeOverride 同语义,但只删该用户的覆盖行(多租户)。
func (s *Store) ClearNodeOverrideForUser(userID int64, nodeKey string) error {
	_, err := s.db.Exec(`DELETE FROM node_overrides WHERE user_id = ? AND node_key = ?`, userID, nodeKey)
	return err
}

// ListNodeOverrides 读取所有覆盖层。等价于 ListNodeOverridesForUser(0)(全量,旧语义)。
func (s *Store) ListNodeOverrides() (map[string]NodeOverride, error) {
	return s.ListNodeOverridesForUser(0)
}

// ListNodeOverridesForUser 读取指定用户的覆盖层(多租户);userID<=0 返回全量
// (超管跨用户视角或旧单用户语义)。
func (s *Store) ListNodeOverridesForUser(userID int64) (map[string]NodeOverride, error) {
	query := `SELECT node_key, display_name, region, updated_at FROM node_overrides`
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
		var updatedStr string
		if err := rows.Scan(&o.NodeKey, &o.DisplayName, &o.Region, &updatedStr); err != nil {
			return nil, err
		}
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
