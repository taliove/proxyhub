package store

import (
	"errors"
	"fmt"
)

// BlockNode 把机场节点(按 NodeKey server:port)加入屏蔽名单。幂等：重复屏蔽不报错。
// 等价于 BlockNodeForUser(0, nodeKey)(未归属桶,旧语义)。
func (s *Store) BlockNode(nodeKey string) error {
	return s.BlockNodeForUser(0, nodeKey)
}

// BlockNodeForUser 与 BlockNode 同语义,但按属主隔离(多租户):
// 屏蔽是每用户名单,(user_id, node_key) 联合主键,不同用户互不影响。
func (s *Store) BlockNodeForUser(userID int64, nodeKey string) error {
	if nodeKey == "" {
		return errors.New("node_key is required")
	}
	_, err := s.db.Exec(
		`INSERT INTO node_blocks (user_id, node_key) VALUES (?, ?) ON CONFLICT(user_id, node_key) DO NOTHING`,
		userID, nodeKey)
	if err != nil {
		return fmt.Errorf("block node: %w", err)
	}
	return nil
}

// BlockNodes 批量把多个 NodeKey 加入屏蔽名单，单事务写入。幂等：已存在的项跳过。
// 空列表直接返回。任一失败整体回滚，避免留下半套屏蔽状态。
func (s *Store) BlockNodes(nodeKeys []string) error {
	return s.BlockNodesForUser(0, nodeKeys)
}

// BlockNodesForUser 与 BlockNodes 同语义,但按属主隔离(多租户)。
func (s *Store) BlockNodesForUser(userID int64, nodeKeys []string) error {
	if len(nodeKeys) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin block nodes: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT INTO node_blocks (user_id, node_key) VALUES (?, ?) ON CONFLICT(user_id, node_key) DO NOTHING`)
	if err != nil {
		return fmt.Errorf("prepare block nodes: %w", err)
	}
	defer stmt.Close()

	for _, key := range nodeKeys {
		if key == "" {
			continue
		}
		if _, err := stmt.Exec(userID, key); err != nil {
			return fmt.Errorf("block node %q: %w", key, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit block nodes: %w", err)
	}
	return nil
}

// UnblockNodes 批量移除屏蔽。空列表或不存在的项都不报错。
func (s *Store) UnblockNodes(nodeKeys []string) error {
	return s.UnblockNodesForUser(0, nodeKeys)
}

// UnblockNodesForUser 与 UnblockNodes 同语义,但只移除该用户名单里的项(多租户)。
func (s *Store) UnblockNodesForUser(userID int64, nodeKeys []string) error {
	if len(nodeKeys) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin unblock nodes: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`DELETE FROM node_blocks WHERE user_id = ? AND node_key = ?`)
	if err != nil {
		return fmt.Errorf("prepare unblock nodes: %w", err)
	}
	defer stmt.Close()

	for _, key := range nodeKeys {
		if _, err := stmt.Exec(userID, key); err != nil {
			return fmt.Errorf("unblock node %q: %w", key, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit unblock nodes: %w", err)
	}
	return nil
}

// UnblockNode 从屏蔽名单移除节点。移除不存在的项不报错。
func (s *Store) UnblockNode(nodeKey string) error {
	return s.UnblockNodeForUser(0, nodeKey)
}

// UnblockNodeForUser 与 UnblockNode 同语义,但只移除该用户名单里的项(多租户)。
func (s *Store) UnblockNodeForUser(userID int64, nodeKey string) error {
	_, err := s.db.Exec(`DELETE FROM node_blocks WHERE user_id = ? AND node_key = ?`, userID, nodeKey)
	if err != nil {
		return fmt.Errorf("unblock node: %w", err)
	}
	return nil
}

// ListBlockedNodes 返回被屏蔽的 NodeKey 集合，便于订阅生成时 O(1) 查询。
// 等价于 ListBlockedNodesForUser(0)(未归属桶,旧语义)。
func (s *Store) ListBlockedNodes() (map[string]bool, error) {
	return s.ListBlockedNodesForUser(0)
}

// ListBlockedNodesForUser 返回指定用户屏蔽名单(多租户);userID<=0 返回全量
// (超管跨用户视角或旧单用户语义)。
func (s *Store) ListBlockedNodesForUser(userID int64) (map[string]bool, error) {
	query := `SELECT node_key FROM node_blocks`
	args := []any{}
	if userID > 0 {
		query += ` WHERE user_id = ?`
		args = append(args, userID)
	}
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query node blocks: %w", err)
	}
	defer rows.Close()

	blocked := make(map[string]bool)
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, fmt.Errorf("scan node block: %w", err)
		}
		blocked[key] = true
	}
	return blocked, rows.Err()
}
