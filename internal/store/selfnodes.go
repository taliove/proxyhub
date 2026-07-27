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

// UpdateSelfHostedNodeForUser 按属主更新自建节点(ticket 07);行属他人时 ErrNotFound。
// userID=0 = 全局视角(测试逃生舱/超管未切换时使用),属主校验跳过。
func (s *Store) UpdateSelfHostedNodeForUser(userID int64, node *SelfHostedNode) error {
	if userID == 0 {
		return s.UpdateSelfHostedNode(node)
	}
	res, err := s.db.Exec(
		`UPDATE self_hosted_nodes SET
		 name = ?, protocol = ?, server = ?, port = ?, uuid = ?, password = ?,
		 cipher = ?, alter_id = ?, network = ?, tls = ?, region_code = ?, grpc_service_name = ?
		 WHERE id = ? AND user_id = ?`,
		node.Name, node.Protocol, node.Server, node.Port, node.UUID, node.Password,
		node.Cipher, node.AlterID, node.Network, boolToInt(node.TLS), node.RegionCode,
		node.GrpcServiceName, node.ID, userID)
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

// SetSelfHostedNodeEnabledForUser 按属主启停自建节点(ticket 07);行属他人时 ErrNotFound。
// userID=0 = 全局视角(测试逃生舱/超管未切换时使用),属主校验跳过。
func (s *Store) SetSelfHostedNodeEnabledForUser(userID, id int64, enabled bool) error {
	if userID == 0 {
		return s.SetSelfHostedNodeEnabled(id, enabled)
	}
	res, err := s.db.Exec(
		`UPDATE self_hosted_nodes SET enabled = ? WHERE id = ? AND user_id = ?`, boolToInt(enabled), id, userID)
	if err != nil {
		return fmt.Errorf("toggle self hosted node: %w", err)
	}
	return checkAffected(res)
}

// ListAllSelfHostedNodes 列出全部自建节点（含已禁用），供后台管理页展示。
// 聚合注入用的是只返回启用节点的 ListSelfHostedNodes。
func (s *Store) ListAllSelfHostedNodes() ([]*SelfHostedNode, error) {
	rows, err := s.db.Query(
		`SELECT ` + selfHostedColumns + ` FROM self_hosted_nodes ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("query all self hosted nodes: %w", err)
	}
	defer rows.Close()

	return scanSelfHostedNodes(rows)
}

// ListAllSelfHostedNodesByUser 列出指定用户名下全部自建节点(含已禁用,ticket 07)。
// userID<=0 回退为全量(内部后台路径:体检回写/任务回调等无请求属主的场景)。
func (s *Store) ListAllSelfHostedNodesByUser(userID int64) ([]*SelfHostedNode, error) {
	if userID <= 0 {
		return s.ListAllSelfHostedNodes()
	}
	rows, err := s.db.Query(
		`SELECT `+selfHostedColumns+` FROM self_hosted_nodes WHERE user_id = ? ORDER BY id`, userID)
	if err != nil {
		return nil, fmt.Errorf("query all self hosted nodes by user: %w", err)
	}
	defer rows.Close()

	return scanSelfHostedNodes(rows)
}
