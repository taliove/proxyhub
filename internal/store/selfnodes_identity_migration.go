package store

import (
	"errors"
	"fmt"
)

// ErrDuplicateIdentity 自建节点身份(user_id+server+port+protocol)冲突(issue #67)。
// 应用层查重(409)是友好快路径,本哨兵兜住 check-then-insert 并发竞态。
var ErrDuplicateIdentity = errors.New("duplicate self-hosted node identity")

// mapIdentityViolation 把唯一约束违例映射为 ErrDuplicateIdentity,其余错误原样返回。
// isUniqueViolation 复用 users.go 既有实现(字符串判定)。
func mapIdentityViolation(err error) error {
	if err != nil && isUniqueViolation(err) {
		return ErrDuplicateIdentity
	}
	return err
}

// migrateSelfHostedIdentityUnique 清理存量重复行并建身份唯一索引(023, issue #67)。
// 幂等:索引已存在则整体跳过。先清重再建索引——顺序反了会在脏库上建索引失败。
func (s *Store) migrateSelfHostedIdentityUnique() error {
	var exists int
	if err := s.db.QueryRow(
		`SELECT COUNT(1) FROM sqlite_master WHERE type = 'index' AND name = 'idx_self_hosted_nodes_identity'`,
	).Scan(&exists); err != nil {
		return fmt.Errorf("inspect identity index: %w", err)
	}
	if exists > 0 {
		return nil
	}

	// 清理存量重复:按身份保留最早一行(MIN(id))
	if _, err := s.db.Exec(`DELETE FROM self_hosted_nodes WHERE id NOT IN (
		SELECT MIN(id) FROM self_hosted_nodes GROUP BY user_id, server, port, protocol)`); err != nil {
		return fmt.Errorf("dedupe self hosted nodes: %w", err)
	}

	if _, err := s.db.Exec(`CREATE UNIQUE INDEX idx_self_hosted_nodes_identity
		ON self_hosted_nodes(user_id, server, port, protocol)`); err != nil {
		return fmt.Errorf("create identity unique index: %w", err)
	}
	return nil
}
