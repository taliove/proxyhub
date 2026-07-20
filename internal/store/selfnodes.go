package store

import "fmt"

// UpdateSelfHostedNode 更新自建节点的全部可编辑字段（不含 enabled，启停走 SetSelfHostedNodeEnabled）。
func (s *Store) UpdateSelfHostedNode(node *SelfHostedNode) error {
	res, err := s.db.Exec(
		`UPDATE self_hosted_nodes SET
		 name = ?, protocol = ?, server = ?, port = ?, uuid = ?, password = ?,
		 cipher = ?, alter_id = ?, network = ?, tls = ?, region_code = ?, grpc_service_name = ?
		 WHERE id = ?`,
		node.Name, node.Protocol, node.Server, node.Port, node.UUID, node.Password,
		node.Cipher, node.AlterID, node.Network, boolToInt(node.TLS), node.RegionCode,
		node.GrpcServiceName, node.ID)
	if err != nil {
		return fmt.Errorf("update self hosted node: %w", err)
	}
	return checkAffected(res)
}

// SetSelfHostedNodeEnabled 启用/禁用自建节点。禁用后不再注入节点池。
func (s *Store) SetSelfHostedNodeEnabled(id int64, enabled bool) error {
	res, err := s.db.Exec(
		`UPDATE self_hosted_nodes SET enabled = ? WHERE id = ?`, boolToInt(enabled), id)
	if err != nil {
		return fmt.Errorf("toggle self hosted node: %w", err)
	}
	return checkAffected(res)
}

// ListAllSelfHostedNodes 列出全部自建节点（含已禁用），供后台管理页展示。
// 聚合注入用的是只返回启用节点的 ListSelfHostedNodes。
func (s *Store) ListAllSelfHostedNodes() ([]*SelfHostedNode, error) {
	rows, err := s.db.Query(
		`SELECT id, name, protocol, server, port, uuid, password, cipher,
		 alter_id, network, tls, region_code, grpc_service_name, enabled FROM self_hosted_nodes ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("query all self hosted nodes: %w", err)
	}
	defer rows.Close()

	var nodes []*SelfHostedNode
	for rows.Next() {
		var n SelfHostedNode
		var tls, enabled int
		if err := rows.Scan(&n.ID, &n.Name, &n.Protocol, &n.Server, &n.Port,
			&n.UUID, &n.Password, &n.Cipher, &n.AlterID, &n.Network, &tls, &n.RegionCode,
			&n.GrpcServiceName, &enabled); err != nil {
			return nil, fmt.Errorf("scan node: %w", err)
		}
		n.TLS = tls == 1
		n.Enabled = enabled == 1
		nodes = append(nodes, &n)
	}
	return nodes, rows.Err()
}
