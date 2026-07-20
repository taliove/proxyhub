package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// DistributionNode represents a distribution node that routes traffic to upstream nodes
type DistributionNode struct {
	ID               int64     `json:"id"`
	Name             string    `json:"name"`
	Region           string    `json:"region"`
	DistributionPath string    `json:"distribution_path"`
	UpstreamNodeKeys []string  `json:"upstream_node_keys"`
	LBStrategy       string    `json:"lb_strategy"`
	Enabled          bool      `json:"enabled"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// ListDistributionNodes returns all enabled distribution nodes for node pool injection
func (s *Store) ListDistributionNodes() ([]*DistributionNode, error) {
	rows, err := s.db.Query(`
		SELECT id, name, region, distribution_path, upstream_node_keys, lb_strategy, enabled, created_at, updated_at
		FROM distribution_nodes
		WHERE enabled = 1
		ORDER BY id
	`)
	if err != nil {
		return nil, fmt.Errorf("query enabled distribution nodes: %w", err)
	}
	defer rows.Close()

	return s.scanDistributionNodes(rows)
}

// ListAllDistributionNodes returns all distribution nodes (including disabled) for management UI
func (s *Store) ListAllDistributionNodes() ([]*DistributionNode, error) {
	rows, err := s.db.Query(`
		SELECT id, name, region, distribution_path, upstream_node_keys, lb_strategy, enabled, created_at, updated_at
		FROM distribution_nodes
		ORDER BY id
	`)
	if err != nil {
		return nil, fmt.Errorf("query all distribution nodes: %w", err)
	}
	defer rows.Close()

	return s.scanDistributionNodes(rows)
}

// GetDistributionNode returns a single distribution node by ID
func (s *Store) GetDistributionNode(id int64) (*DistributionNode, error) {
	row := s.db.QueryRow(`
		SELECT id, name, region, distribution_path, upstream_node_keys, lb_strategy, enabled, created_at, updated_at
		FROM distribution_nodes
		WHERE id = ?
	`, id)

	var node DistributionNode
	var enabled int
	var nodeKeysJSON string

	err := row.Scan(&node.ID, &node.Name, &node.Region, &node.DistributionPath,
		&nodeKeysJSON, &node.LBStrategy, &enabled, &node.CreatedAt, &node.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get distribution node: %w", err)
	}

	node.Enabled = enabled == 1

	if err := json.Unmarshal([]byte(nodeKeysJSON), &node.UpstreamNodeKeys); err != nil {
		return nil, fmt.Errorf("unmarshal upstream_node_keys: %w", err)
	}

	return &node, nil
}

// CreateDistributionNode creates a new distribution node
func (s *Store) CreateDistributionNode(node *DistributionNode) error {
	if node.Name == "" {
		return fmt.Errorf("name is required")
	}
	if node.DistributionPath == "" {
		return fmt.Errorf("distribution_path is required")
	}

	// Ensure UpstreamNodeKeys is not nil (marshal as empty array)
	if node.UpstreamNodeKeys == nil {
		node.UpstreamNodeKeys = []string{}
	}

	nodeKeysJSON, err := json.Marshal(node.UpstreamNodeKeys)
	if err != nil {
		return fmt.Errorf("marshal upstream_node_keys: %w", err)
	}

	res, err := s.db.Exec(`
		INSERT INTO distribution_nodes
		(name, region, distribution_path, upstream_node_keys, lb_strategy, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, node.Name, node.Region, node.DistributionPath, string(nodeKeysJSON),
		node.LBStrategy, boolToInt(node.Enabled))
	if err != nil {
		return fmt.Errorf("create distribution node: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("get last insert id: %w", err)
	}
	node.ID = id

	return nil
}

// UpdateDistributionNode updates an existing distribution node
func (s *Store) UpdateDistributionNode(node *DistributionNode) error {
	if node.Name == "" {
		return fmt.Errorf("name is required")
	}
	if node.DistributionPath == "" {
		return fmt.Errorf("distribution_path is required")
	}

	// Ensure UpstreamNodeKeys is not nil (marshal as empty array)
	if node.UpstreamNodeKeys == nil {
		node.UpstreamNodeKeys = []string{}
	}

	nodeKeysJSON, err := json.Marshal(node.UpstreamNodeKeys)
	if err != nil {
		return fmt.Errorf("marshal upstream_node_keys: %w", err)
	}

	res, err := s.db.Exec(`
		UPDATE distribution_nodes
		SET name = ?, region = ?, distribution_path = ?, upstream_node_keys = ?,
		    lb_strategy = ?, enabled = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, node.Name, node.Region, node.DistributionPath, string(nodeKeysJSON),
		node.LBStrategy, boolToInt(node.Enabled), node.ID)
	if err != nil {
		return fmt.Errorf("update distribution node: %w", err)
	}

	return checkAffected(res)
}

// DeleteDistributionNode deletes a distribution node by ID
func (s *Store) DeleteDistributionNode(id int64) error {
	res, err := s.db.Exec(`DELETE FROM distribution_nodes WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete distribution node: %w", err)
	}
	return checkAffected(res)
}

// SetDistributionNodeEnabled enables or disables a distribution node
func (s *Store) SetDistributionNodeEnabled(id int64, enabled bool) error {
	res, err := s.db.Exec(`
		UPDATE distribution_nodes
		SET enabled = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, boolToInt(enabled), id)
	if err != nil {
		return fmt.Errorf("toggle distribution node: %w", err)
	}
	return checkAffected(res)
}

// scanDistributionNodes is a helper to scan rows into DistributionNode slice
func (s *Store) scanDistributionNodes(rows interface {
	Next() bool
	Scan(dest ...interface{}) error
	Err() error
}) ([]*DistributionNode, error) {
	var nodes []*DistributionNode
	for rows.Next() {
		var node DistributionNode
		var enabled int
		var nodeKeysJSON string

		if err := rows.Scan(&node.ID, &node.Name, &node.Region, &node.DistributionPath,
			&nodeKeysJSON, &node.LBStrategy, &enabled, &node.CreatedAt, &node.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan distribution node: %w", err)
		}

		node.Enabled = enabled == 1

		if err := json.Unmarshal([]byte(nodeKeysJSON), &node.UpstreamNodeKeys); err != nil {
			return nil, fmt.Errorf("unmarshal upstream_node_keys: %w", err)
		}

		nodes = append(nodes, &node)
	}

	return nodes, rows.Err()
}
