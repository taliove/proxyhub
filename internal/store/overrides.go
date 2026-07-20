package store

import "time"

// NodeOverride 机场节点编辑覆盖层
type NodeOverride struct {
	NodeKey     string    `json:"node_key"`
	DisplayName string    `json:"display_name"`
	Region      string    `json:"region"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// SetNodeOverride upsert 覆盖层（display_name/region 任一非空）
func (s *Store) SetNodeOverride(nodeKey, displayName, region string) error {
	_, err := s.db.Exec(`
		INSERT INTO node_overrides (node_key, display_name, region, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(node_key) DO UPDATE SET
			display_name = excluded.display_name,
			region = excluded.region,
			updated_at = excluded.updated_at
	`, nodeKey, displayName, region, time.Now())
	return err
}

// ClearNodeOverride 删除覆盖层
func (s *Store) ClearNodeOverride(nodeKey string) error {
	_, err := s.db.Exec(`DELETE FROM node_overrides WHERE node_key = ?`, nodeKey)
	return err
}

// ListNodeOverrides 读取所有覆盖层
func (s *Store) ListNodeOverrides() (map[string]NodeOverride, error) {
	rows, err := s.db.Query(`SELECT node_key, display_name, region, updated_at FROM node_overrides`)
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
