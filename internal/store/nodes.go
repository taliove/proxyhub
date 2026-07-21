package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/taliove/proxyhub/internal/subscription"
)

// StaleRetentionDays 下架(stale)节点保留天数。机场订阅中消失的节点先标记 stale
// 保留一段时间(期间复活可 carry-forward 检测状态),超期才物理删除,防止无限累积。
const StaleRetentionDays = 7

// SaveNodePool 以 NodeKey 为唯一 ID 进行 upsert，保留检测状态并标记消失节点为 stale。
// 相比旧版（DELETE + 全量 INSERT），upsert 修复了刷新抹掉真实检测结果的 bug。
// 传入空池则将所有机场节点标记为 stale（自建节点不受影响，由聚合注入保证在场）。
func (s *Store) SaveNodePool(nodes []*subscription.Node) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// 第一步：将所有现有行标记为 stale=1，并设置大 position（确保 stale 节点排在后面）
	if _, err := tx.Exec(`UPDATE nodes SET stale = 1, position = 999999`); err != nil {
		return fmt.Errorf("mark all stale: %w", err)
	}

	if len(nodes) == 0 {
		// 空池：全标记 stale，清理所有死节点标签后提交
		if err := pruneStaleNodeTags(tx); err != nil {
			return err
		}
		// 空池不豁免超期清理：历史 stale 节点到期照样删除
		if err := purgeExpiredStaleNodes(tx, time.Now()); err != nil {
			return err
		}
		return tx.Commit()
	}

	// 第二步：upsert 本轮池的每个节点
	stmt, err := tx.Prepare(`
		INSERT INTO nodes (
			node_key, name, type, server, port, uuid, password, alter_id, cipher, network, tls,
			sni, grpc_service_name, region, source, available, latency_ms, position, stale, last_seen,
			detection_last_check, bandwidth_down, bandwidth_up, bandwidth_check, plugin, plugin_opts
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(node_key) DO UPDATE SET
			name = excluded.name,
			type = excluded.type,
			server = excluded.server,
			port = excluded.port,
			uuid = excluded.uuid,
			password = excluded.password,
			alter_id = excluded.alter_id,
			cipher = excluded.cipher,
			network = excluded.network,
			tls = excluded.tls,
			sni = excluded.sni,
			grpc_service_name = excluded.grpc_service_name,
			region = excluded.region,
			source = excluded.source,
			available = excluded.available,
			latency_ms = excluded.latency_ms,
			position = excluded.position,
			stale = excluded.stale,
			last_seen = excluded.last_seen,
			detection_last_check = excluded.detection_last_check,
			bandwidth_down = excluded.bandwidth_down,
			bandwidth_up = excluded.bandwidth_up,
			bandwidth_check = excluded.bandwidth_check,
			plugin = excluded.plugin,
			plugin_opts = excluded.plugin_opts
	`)
	if err != nil {
		return fmt.Errorf("prepare upsert: %w", err)
	}
	defer stmt.Close()

	for i, n := range nodes {
		key := n.NodeKey()
		if _, err := stmt.Exec(
			key, n.Name, n.Type, n.Server, n.Port, n.UUID, n.Password, n.AlterID,
			n.Cipher, n.Network, boolToInt(n.TLS), n.SNI, n.GrpcServiceName,
			n.Region, n.Source, boolToInt(n.Available), n.Latency, i,
			boolToInt(n.Stale), timeOrNull(n.LastSeen),
			timeOrNull(n.DetectionLastCheck),
			n.BandwidthDownMbps, n.BandwidthUpMbps, timeOrNull(n.BandwidthCheck),
			n.Plugin, n.PluginOpts,
		); err != nil {
			return fmt.Errorf("upsert node %s: %w", key, err)
		}
	}

	// 清理死节点标签:本轮消失(仍 stale=1)的节点标签失去意义,随刷新删除。
	if err := pruneStaleNodeTags(tx); err != nil {
		return err
	}

	// 清理超期 stale 节点:下架超过 StaleRetentionDays 的节点物理删除,防无限累积。
	if err := purgeExpiredStaleNodes(tx, time.Now()); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit node pool: %w", err)
	}
	return nil
}

// purgeExpiredStaleNodes 删除下架超过 StaleRetentionDays 的节点(在 SaveNodePool 事务内调用)。
//
// last_seen 在库中是 Go time 格式串(如 "2026-07-20 17:57:05.013304 +0800 CST"),
// SQLite datetime() 无法解析、裸串比较依赖时区巧合,故必须在 Go 侧解析后比较。
// 有意不级联删 node_overrides/node_blocks/exam_history:保留期内节点复活时这些仍应生效;
// 超期删除后若同 key 节点再次复活,旧 override/block 会重新生效(接受此语义)。
func purgeExpiredStaleNodes(tx *sql.Tx, now time.Time) error {
	cutoff := now.AddDate(0, 0, -StaleRetentionDays)

	rows, err := tx.Query(`SELECT node_key, last_seen FROM nodes WHERE stale = 1`)
	if err != nil {
		return fmt.Errorf("query stale nodes: %w", err)
	}
	defer rows.Close()

	var expired []string
	for rows.Next() {
		var key string
		var lastSeen *string
		if err := rows.Scan(&key, &lastSeen); err != nil {
			return fmt.Errorf("scan stale node: %w", err)
		}
		ts := parseTimeOrZero(lastSeen)
		// 解析失败/无 last_seen 的保守保留(不误删)
		if !ts.IsZero() && ts.Before(cutoff) {
			expired = append(expired, key)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate stale nodes: %w", err)
	}
	if len(expired) == 0 {
		return nil
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(expired)), ",")
	args := make([]any, len(expired))
	for i, k := range expired {
		args[i] = k
	}
	if _, err := tx.Exec(`DELETE FROM nodes WHERE node_key IN (`+placeholders+`)`, args...); err != nil {
		return fmt.Errorf("delete expired stale nodes: %w", err)
	}
	return nil
}

// pruneStaleNodeTags 删除当前所有 stale 节点的自动标签(在 SaveNodePool 事务内调用)。
func pruneStaleNodeTags(tx *sql.Tx) error {
	if _, err := tx.Exec(
		`DELETE FROM node_tags WHERE node_key IN (SELECT node_key FROM nodes WHERE stale = 1)`,
	); err != nil {
		return fmt.Errorf("prune stale node tags: %w", err)
	}
	return nil
}

// AllNodeKeys returns all node_key values in the pool (including stale nodes).
// Used by batch operations that need to iterate over all nodes.
func (s *Store) AllNodeKeys() ([]string, error) {
	rows, err := s.db.Query(`SELECT node_key FROM nodes ORDER BY position`)
	if err != nil {
		return nil, fmt.Errorf("query node keys: %w", err)
	}
	defer rows.Close()

	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, fmt.Errorf("scan node key: %w", err)
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

// LoadNodePool 读取节点池快照，按 position 排序（stale 节点也返回，由调用方决定过滤）。
func (s *Store) LoadNodePool() ([]*subscription.Node, error) {
	rows, err := s.db.Query(`
		SELECT node_key, name, type, server, port, uuid, password, alter_id, cipher, network, tls,
		       sni, grpc_service_name, region, source, available, latency_ms, stale, last_seen,
		       detection_last_check, bandwidth_down, bandwidth_up, bandwidth_check, plugin, plugin_opts
		FROM nodes
		ORDER BY position
	`)
	if err != nil {
		return nil, fmt.Errorf("query nodes: %w", err)
	}
	defer rows.Close()

	var nodes []*subscription.Node
	for rows.Next() {
		var n subscription.Node
		var nodeKey string
		var tls, available, stale int
		var lastSeen, detectionLastCheck, bandwidthCheck *string
		if err := rows.Scan(
			&nodeKey, &n.Name, &n.Type, &n.Server, &n.Port, &n.UUID, &n.Password, &n.AlterID,
			&n.Cipher, &n.Network, &tls, &n.SNI, &n.GrpcServiceName,
			&n.Region, &n.Source, &available, &n.Latency, &stale, &lastSeen,
			&detectionLastCheck, &n.BandwidthDownMbps, &n.BandwidthUpMbps, &bandwidthCheck,
			&n.Plugin, &n.PluginOpts,
		); err != nil {
			return nil, fmt.Errorf("scan node: %w", err)
		}
		n.TLS = tls == 1
		n.Available = available == 1
		n.Stale = stale == 1
		n.LastSeen = parseTimeOrZero(lastSeen)
		n.DetectionLastCheck = parseTimeOrZero(detectionLastCheck)
		n.BandwidthCheck = parseTimeOrZero(bandwidthCheck)
		nodes = append(nodes, &n)
	}
	return nodes, rows.Err()
}
