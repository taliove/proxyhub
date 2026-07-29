package store

import (
	"fmt"

	"github.com/taliove/proxyhub/internal/subscription"
)

// DeleteAirportNodes 一次性删除 nodes 表中全部机场节点(source != self-hosted),自建节点豁免。
// 返回删除的节点数。语义定义见 CONTEXT.md「机场节点清空」。
//
// 关联表语义:
//   - node_blocks / node_overrides 保留:屏蔽与名称/地区覆盖是跨刷新、跨清空的持久语义,
//     同 key 节点重新入池后继续生效(与 purgeExpiredStaleNodes 的保留惯例一致)。
//   - node_tags 随节点删除:标签由检测结果派生,节点不在池即失去意义
//     (同 pruneStaleNodeTags 惯例),节点重新入池并检测后重算。
//
// 单事务执行,失败整体回滚。
func (s *Store) DeleteAirportNodes() (int64, error) {
	return s.DeleteAirportNodesForUser(0)
}

// DeleteAirportNodesForUser 删除指定用户分片的机场节点(ticket 07);
// userID=0 为旧行为(跨用户全清)。关联表语义同 DeleteAirportNodes。
// 手动机场节点豁免(2026-07-29 决策):无 URL 可拉,清空后永不回来——
// 豁免经 SQL 子查询同事务判定,不在"列名-删除"两步间留竞态窗口。
func (s *Store) DeleteAirportNodesForUser(userID int64) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// 手动机场豁免子查询;userID>0 时按同一属主收窄(同名机场跨用户不串)。
	manualFilter := ` AND source NOT IN (SELECT name FROM airports WHERE source_type = '` + AirportSourceManual + `'`
	args := []any{subscription.SourceSelfHosted}
	if userID > 0 {
		manualFilter += ` AND user_id = ?`
		args = append(args, userID)
	}
	manualFilter += `)`

	tagQuery := `DELETE FROM node_tags WHERE node_key IN (SELECT node_key FROM nodes WHERE source != ?` + manualFilter
	nodeQuery := `DELETE FROM nodes WHERE source != ?` + manualFilter
	if userID > 0 {
		tagQuery += ` AND user_id = ?`
		nodeQuery += ` AND user_id = ?`
		args = append(args, userID)
	}
	tagQuery += `)`

	// 先删机场节点的自动标签(需引用 nodes 表筛选,必须在删节点之前)
	if _, err := tx.Exec(tagQuery, args...); err != nil {
		return 0, fmt.Errorf("delete airport node tags: %w", err)
	}

	res, err := tx.Exec(nodeQuery, args...)
	if err != nil {
		return 0, fmt.Errorf("delete airport nodes: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("rows affected: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit purge airport nodes: %w", err)
	}
	return affected, nil
}
