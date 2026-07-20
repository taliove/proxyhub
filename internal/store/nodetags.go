package store

import (
	"fmt"

	"github.com/taliove/proxyhub/internal/detection"
	"github.com/taliove/proxyhub/internal/nodetag"
)

// ReplaceNodeTags 事务内全量替换某节点的标签集(幂等)。
// 先删该节点所有标签,再插入新集合;空集合等价于清空。
// INSERT OR IGNORE 对 (node_key, tag) 主键去重,防御重复输入。
func (s *Store) ReplaceNodeTags(nodeKey string, tags []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin node tags tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM node_tags WHERE node_key = ?`, nodeKey); err != nil {
		return fmt.Errorf("delete node tags: %w", err)
	}

	if len(tags) > 0 {
		stmt, err := tx.Prepare(`INSERT OR IGNORE INTO node_tags (node_key, tag) VALUES (?, ?)`)
		if err != nil {
			return fmt.Errorf("prepare insert node tag: %w", err)
		}
		defer stmt.Close()
		for _, tag := range tags {
			if _, err := stmt.Exec(nodeKey, tag); err != nil {
				return fmt.Errorf("insert node tag %q: %w", tag, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit node tags: %w", err)
	}
	return nil
}

// ListNodeTags 批量查询多个节点的标签,返回 map[nodeKey][]tag(每节点标签按字典序)。
// 无标签的节点不出现在结果 map 中(调用方按缺省=空切片处理)。
func (s *Store) ListNodeTags(nodeKeys []string) (map[string][]string, error) {
	result := make(map[string][]string)
	if len(nodeKeys) == 0 {
		return result, nil
	}

	placeholders := make([]string, len(nodeKeys))
	args := make([]any, len(nodeKeys))
	for i, k := range nodeKeys {
		placeholders[i] = "?"
		args[i] = k
	}

	query := fmt.Sprintf(`
		SELECT node_key, tag FROM node_tags
		WHERE node_key IN (%s)
		ORDER BY node_key, tag
	`, joinPlaceholders(placeholders))

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query node tags: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var nodeKey, tag string
		if err := rows.Scan(&nodeKey, &tag); err != nil {
			return nil, fmt.Errorf("scan node tag: %w", err)
		}
		result[nodeKey] = append(result[nodeKey], tag)
	}
	return result, rows.Err()
}

// RecomputeNodeTags 重算单节点标签并全量替换(幂等)。
// 编排:检测目标配置(name->kind) + node_health 最新解锁判定 + 最近体检报告
// -> nodetag.Derive -> ReplaceNodeTags。无任何数据时派生空集,清掉历史残留标签。
func (s *Store) RecomputeNodeTags(nodeKey string) error {
	// 1. 检测目标配置:把 node_health 里的展示名(target_name)解析回专用 kind。
	targets, err := s.GetDetectionTargets()
	if err != nil {
		return fmt.Errorf("get detection targets: %w", err)
	}
	kindByName := make(map[string]detection.Kind, len(targets))
	for _, t := range targets {
		kindByName[t.Name] = t.Kind
	}

	// 2. 最新解锁判定 -> 派生输入(跳过 generic/未配置目标)。
	latest, err := s.GetLatestDetectionResults([]string{nodeKey})
	if err != nil {
		return fmt.Errorf("get latest detection results: %w", err)
	}
	var unlocks []nodetag.UnlockResult
	for _, v := range latest[nodeKey] {
		kind, ok := kindByName[v.TargetName]
		if !ok || kind == "" || kind == detection.KindGeneric {
			continue
		}
		unlocks = append(unlocks, nodetag.UnlockResult{Kind: kind, Level: v.Level})
	}

	// 3. 最近一次体检报告(无则 nil)。
	var report *detection.ExamReport
	entry, err := s.LatestExamHistory(nodeKey)
	if err != nil {
		return fmt.Errorf("latest exam history: %w", err)
	}
	if entry != nil {
		report = &entry.Report
	}

	// 4. 派生并全量替换。
	return s.ReplaceNodeTags(nodeKey, nodetag.Derive(unlocks, report))
}
