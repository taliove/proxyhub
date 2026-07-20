package store

import (
	"errors"
	"fmt"
)

// BlockNode 把机场节点(按 NodeKey server:port)加入屏蔽名单。幂等：重复屏蔽不报错。
func (s *Store) BlockNode(nodeKey string) error {
	if nodeKey == "" {
		return errors.New("node_key is required")
	}
	_, err := s.db.Exec(
		`INSERT INTO node_blocks (node_key) VALUES (?) ON CONFLICT(node_key) DO NOTHING`,
		nodeKey)
	if err != nil {
		return fmt.Errorf("block node: %w", err)
	}
	return nil
}

// BlockNodes 批量把多个 NodeKey 加入屏蔽名单，单事务写入。幂等：已存在的项跳过。
// 空列表直接返回。任一失败整体回滚，避免留下半套屏蔽状态。
func (s *Store) BlockNodes(nodeKeys []string) error {
	if len(nodeKeys) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin block nodes: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT INTO node_blocks (node_key) VALUES (?) ON CONFLICT(node_key) DO NOTHING`)
	if err != nil {
		return fmt.Errorf("prepare block nodes: %w", err)
	}
	defer stmt.Close()

	for _, key := range nodeKeys {
		if key == "" {
			continue
		}
		if _, err := stmt.Exec(key); err != nil {
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
	if len(nodeKeys) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin unblock nodes: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`DELETE FROM node_blocks WHERE node_key = ?`)
	if err != nil {
		return fmt.Errorf("prepare unblock nodes: %w", err)
	}
	defer stmt.Close()

	for _, key := range nodeKeys {
		if _, err := stmt.Exec(key); err != nil {
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
	_, err := s.db.Exec(`DELETE FROM node_blocks WHERE node_key = ?`, nodeKey)
	if err != nil {
		return fmt.Errorf("unblock node: %w", err)
	}
	return nil
}

// ListBlockedNodes 返回被屏蔽的 NodeKey 集合，便于订阅生成时 O(1) 查询。
func (s *Store) ListBlockedNodes() (map[string]bool, error) {
	rows, err := s.db.Query(`SELECT node_key FROM node_blocks`)
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
