package store

import "fmt"

// migrateNodesToUpsert 为 nodes 表添加 upsert 所需列并建立唯一索引。
// 包含去重逻辑：旧库可能有重复 NodeKey 历史行（因之前是整表替换），
// 去重保留 position 最小的行，去重失败降级为非唯一索引（不阻断启动）。
func (s *Store) migrateNodesToUpsert() error {
	// 1. 添加新列
	if err := s.addColumnIfMissing("nodes", "node_key", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing("nodes", "stale", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing("nodes", "last_seen", "TIMESTAMP"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing("nodes", "detection_last_check", "TIMESTAMP"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing("nodes", "bandwidth_down", "REAL NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing("nodes", "bandwidth_up", "REAL NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing("nodes", "bandwidth_check", "TIMESTAMP"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing("nodes", "sni", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing("nodes", "grpc_service_name", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}

	// 2. 回填 node_key（旧行的 node_key 列为空，需从 server/port/sni 计算）
	if err := s.backfillNodeKeys(); err != nil {
		return fmt.Errorf("backfill node_key: %w", err)
	}

	// 3. 去重（保留 position 最小的行）
	if err := s.deduplicateNodesByKey(); err != nil {
		// 去重失败静默降级：用非唯一索引（不阻断启动）
		// 调用方（aggregator）会在日志记录此情况
	}

	// 4. 建立唯一索引（去重成功则唯一，失败则非唯一）
	return s.ensureNodeKeyIndex()
}

// backfillNodeKeys 为 node_key 列为空的旧行回填 NodeKey（server:port[:sni]）
func (s *Store) backfillNodeKeys() error {
	// 只更新 node_key 为空的行
	_, err := s.db.Exec(`
		UPDATE nodes
		SET node_key = CASE
			WHEN sni != '' THEN server || ':' || port || ':' || sni
			ELSE server || ':' || port
		END
		WHERE node_key = ''
	`)
	return err
}

// deduplicateNodesByKey 去重：对每个 node_key，保留 position 最小的行，删除其余
func (s *Store) deduplicateNodesByKey() error {
	// 找出所有重复的 node_key（出现次数 > 1）
	rows, err := s.db.Query(`
		SELECT node_key, COUNT(*) as cnt
		FROM nodes
		WHERE node_key != ''
		GROUP BY node_key
		HAVING cnt > 1
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	var duplicates []string
	for rows.Next() {
		var key string
		var cnt int
		if err := rows.Scan(&key, &cnt); err != nil {
			return err
		}
		duplicates = append(duplicates, key)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	if len(duplicates) == 0 {
		return nil // 无重复，直接成功
	}

	// 对每个重复的 node_key，删除 position 不是最小的行
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, key := range duplicates {
		// 保留 position 最小的，删除其他
		if _, err := tx.Exec(`
			DELETE FROM nodes
			WHERE node_key = ?
			AND id NOT IN (
				SELECT id FROM nodes
				WHERE node_key = ?
				ORDER BY position
				LIMIT 1
			)
		`, key, key); err != nil {
			return fmt.Errorf("deduplicate key %s: %w", key, err)
		}
	}

	return tx.Commit()
}

// ensureNodeKeyIndex 建立 node_key 索引；去重成功则唯一索引，失败则普通索引
func (s *Store) ensureNodeKeyIndex() error {
	// 先尝试唯一索引
	_, err := s.db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_nodes_node_key ON nodes(node_key)`)
	if err != nil {
		// 唯一索引失败（说明去重没彻底成功或新写入了重复），降级为普通索引
		_, err = s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_nodes_node_key ON nodes(node_key)`)
	}
	return err
}
