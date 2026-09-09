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
// userID>0 时只操作该用户分片(stale 标记/删除均限定 user_id,跨用户互不影响,ticket 07);
// userID=0 为旧行为(全表,兼容未分片的既有调用与测试)。
func (s *Store) SaveNodePool(nodes []*subscription.Node) error {
	return s.SaveNodePoolForUser(0, nodes)
}

// SaveNodePoolForUser 按属主分片保存节点池(ticket 07)。见 SaveNodePool。
func (s *Store) SaveNodePoolForUser(userID int64, nodes []*subscription.Node) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// 第一步：将本用户分片现有行标记为 stale=1，并设置大 position（确保 stale 节点排在后面）
	markStale := `UPDATE nodes SET stale = 1, position = 999999`
	args := []any{}
	if userID > 0 {
		markStale += ` WHERE user_id = ?`
		args = append(args, userID)
	}
	if _, err := tx.Exec(markStale, args...); err != nil {
		return fmt.Errorf("mark all stale: %w", err)
	}

	if len(nodes) == 0 {
		// 空池：全标记 stale，清理所有死节点标签后提交
		if err := pruneStaleNodeTags(tx, userID); err != nil {
			return err
		}
		// 空池不豁免超期清理：历史 stale 节点到期照样删除
		if err := purgeExpiredStaleNodes(tx, time.Now(), userID); err != nil {
			return err
		}
		return tx.Commit()
	}

	// 第二步：upsert 本轮池的每个节点
	if err := upsertPoolNodes(tx, nodes); err != nil {
		return err
	}

	// 清理死节点标签:本轮消失(仍 stale=1)的节点标签失去意义,随刷新删除。
	if err := pruneStaleNodeTags(tx, userID); err != nil {
		return err
	}

	// 清理超期 stale 节点:下架超过 StaleRetentionDays 的节点物理删除,防无限累积。
	if err := purgeExpiredStaleNodes(tx, time.Now(), userID); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit node pool: %w", err)
	}
	return nil
}

// UpsertNodePoolShard 只重写一个来源(机场)×属主的分片(issue #143):本分片
// 现有行先整体标记 stale,再逐行 upsert 传入节点,最后清理本分片的死节点标签
// 与超期 stale 节点。其他行全程不动——任一节点数据异常只回滚本分片,不再连坐
// 全池。prune/purge 口径与 SaveNodePool 相同,仅作用域收窄到 (source, user_id)。
// nodes 必须是该来源分片合并后的完整新状态(MergePool 产出:在架 + 消失标 stale),
// 不在 nodes 里的本分片行会保持 stale。
//
// userID 为机场属主(issue #143 跨用户隔离):机场名是用户自选字符串,两个用户
// 可有同名机场;stale 标记/prune/purge 全部带 AND user_id = ?,否则用户 A 的
// 单机场刷新/手动导入会把用户 B 同名机场的节点标 stale、打乱 position、删自动
// 标签。userID=0 只作用未归属桶(与 staleScope 的 "0=全表" 语义刻意不同——
// 分片属主隔离是硬边界,见 nodes.go UpdateNodeDetectionResult 的同款约定)。
//
// 并发警示:本方法是"标 stale -> upsert"的读-改-写,跨两条语句(即便同事务,
// 两个并发事务对同一 source 会交错产生错误的 stale 终态)。调用方必须串行——
// 现由 poolops 包级 upsertMu 保证;禁止绕过 poolops 直接并发调用本方法。
func (s *Store) UpsertNodePoolShard(userID int64, source string, nodes []*subscription.Node) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// 第一步:本来源本属主分片现有行标记 stale(在架与否以本轮 upsert 为准)
	if _, err := tx.Exec(`UPDATE nodes SET stale = 1, position = 999999 WHERE source = ? AND user_id = ?`, source, userID); err != nil {
		return fmt.Errorf("mark shard stale: %w", err)
	}

	// 第二步:upsert 本来源本轮节点(含 MergePool 追加的分片内 stale 节点)
	if err := upsertPoolNodes(tx, nodes); err != nil {
		return err
	}

	// 第三步:分片内清理,口径同 SaveNodePool,作用域收窄到 (source, user_id)
	if err := pruneStaleNodeTagsForSource(tx, userID, source); err != nil {
		return err
	}
	if err := purgeExpiredStaleNodesForSource(tx, time.Now(), userID, source); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit node pool shard: %w", err)
	}
	return nil
}

// upsertPoolNodes 在事务内逐行 upsert 节点(position 取切片序)。空切片直接返回。
func upsertPoolNodes(tx *sql.Tx, nodes []*subscription.Node) error {
	if len(nodes) == 0 {
		return nil
	}
	stmt, err := tx.Prepare(`
		INSERT INTO nodes (
			node_key, name, type, server, port, uuid, password, alter_id, cipher, network, tls,
			sni, grpc_service_name, region, source, available, latency_ms, position, stale, last_seen,
			detection_last_check, bandwidth_down, bandwidth_up, bandwidth_check, plugin, plugin_opts,
			detection_kind, detection_fail_reason, detection_fail_detail, user_id,
			flow, reality_public_key, reality_short_id, client_fingerprint, grpc_authority
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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
			plugin_opts = excluded.plugin_opts,
			detection_kind = excluded.detection_kind,
			detection_fail_reason = excluded.detection_fail_reason,
			detection_fail_detail = excluded.detection_fail_detail,
			user_id = excluded.user_id,
			flow = excluded.flow,
			reality_public_key = excluded.reality_public_key,
			reality_short_id = excluded.reality_short_id,
			client_fingerprint = excluded.client_fingerprint,
			grpc_authority = excluded.grpc_authority
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
			n.Plugin, n.PluginOpts, n.DetectionKind, n.DetectionFailReason, n.DetectionFailDetail,
			n.UserID,
			n.Flow, n.RealityPublicKey, n.RealityShortID, n.ClientFingerprint, n.GrpcAuthority,
		); err != nil {
			return fmt.Errorf("upsert node %s: %w", key, err)
		}
	}
	return nil
}

// purgeExpiredStaleNodes 删除下架超过 StaleRetentionDays 的节点(在 SaveNodePool 事务内调用)。
func purgeExpiredStaleNodes(tx *sql.Tx, now time.Time, userID int64) error {
	scope, args := staleScope(userID, "")
	return purgeExpiredStaleNodesScoped(tx, now, scope, args)
}

// purgeExpiredStaleNodesForSource 删除指定 (属主, 来源) 分片内超期的 stale 节点
// (在 UpsertNodePoolShard 事务内调用)。user_id 谓词恒在(0 = 未归属桶):
// 跨用户同名机场互不清理(issue #143)。
func purgeExpiredStaleNodesForSource(tx *sql.Tx, now time.Time, userID int64, source string) error {
	scope, args := shardStaleScope(userID, source)
	return purgeExpiredStaleNodesScoped(tx, now, scope, args)
}

// purgeExpiredStaleNodesScoped 是超期 stale 清理的 scoped 实现:scope/scopeArgs
// 由调用方按边界组装(SaveNodePool 用 staleScope,分片 upsert 用 shardStaleScope)。
//
// last_seen 在库中是 Go time 格式串(如 "2026-07-20 17:57:05.013304 +0800 CST"),
// SQLite datetime() 无法解析、裸串比较依赖时区巧合,故必须在 Go 侧解析后比较。
// 有意不级联删 node_overrides/node_blocks/exam_history:保留期内节点复活时这些仍应生效;
// 超期删除后若同 key 节点再次复活,旧 override/block 会重新生效(接受此语义)。
func purgeExpiredStaleNodesScoped(tx *sql.Tx, now time.Time, scope string, scopeArgs []any) error {
	cutoff := now.AddDate(0, 0, -StaleRetentionDays)

	rows, err := tx.Query(`SELECT node_key, last_seen FROM nodes WHERE stale = 1`+scope, scopeArgs...)
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
	delArgs := make([]any, len(expired))
	for i, k := range expired {
		delArgs[i] = k
	}
	delQuery := `DELETE FROM nodes WHERE node_key IN (` + placeholders + `)` + scope
	delArgs = append(delArgs, scopeArgs...)
	if _, err := tx.Exec(delQuery, delArgs...); err != nil {
		return fmt.Errorf("delete expired stale nodes: %w", err)
	}
	return nil
}

// UpdateNodeDetectionResult 单节点检测结果即时落库(issue #33):手动检测/批量
// 检测/订阅实测写回内存池的同时持久化检测字段,与「全量刷新成功才 SaveNodePool」
// 解耦——机场 URL 拉不通时,手动检出的可用状态不再只活在当前进程里。
// mode 语义与内存池写回一致:"bandwidth" 只更新带宽三列;其余(quick/real)更新
// 可用性/延迟/检测时间/判定来源/失败原因。只动检测列,身份列一概不碰(节点身份
// 可能已被刷新改写,此处不越权)。行不存在(节点从未入过库)静默跳过——内存池
// 仍是事实源,下轮刷新落库。
// userID 谓词(issue #131):nodes.user_id 是 last-writer-wins 归属列,单行可能
// 归其他租户所有;userID>0 时带 `AND user_id = ?`,行不属本人与「行不存在」同语义
// 静默跳过(不报错、不写),内存池写回不受影响。userID=0 是内部跨分片回退路径
// (机场测试探测等,单管理员等价旧行为),不带谓词。
func (s *Store) UpdateNodeDetectionResult(userID int64, n *subscription.Node, mode string) error {
	if mode == "bandwidth" {
		query := `UPDATE nodes SET bandwidth_down = ?, bandwidth_up = ?, bandwidth_check = ? WHERE node_key = ?`
		args := []any{n.BandwidthDownMbps, n.BandwidthUpMbps, timeOrNull(n.BandwidthCheck), n.NodeKey()}
		if userID > 0 {
			query += ` AND user_id = ?`
			args = append(args, userID)
		}
		if _, err := s.db.Exec(query, args...); err != nil {
			return fmt.Errorf("persist bandwidth result %s: %w", n.NodeKey(), err)
		}
		return nil
	}
	query := `UPDATE nodes SET available = ?, latency_ms = ?, detection_last_check = ?,
		detection_kind = ?, detection_fail_reason = ?, detection_fail_detail = ?
		WHERE node_key = ?`
	args := []any{boolToInt(n.Available), n.Latency, timeOrNull(n.DetectionLastCheck),
		n.DetectionKind, n.DetectionFailReason, n.DetectionFailDetail, n.NodeKey()}
	if userID > 0 {
		query += ` AND user_id = ?`
		args = append(args, userID)
	}
	if _, err := s.db.Exec(query, args...); err != nil {
		return fmt.Errorf("persist detection result %s: %w", n.NodeKey(), err)
	}
	return nil
}

// staleScope 组装 stale 行筛选子句:userID>0 限定用户分片,source 非空限定
// 来源(机场)分片;两者皆空时返回空串(全表)。返回片段以 " AND " 开头,
// 供拼接在已有 WHERE 条件之后。
func staleScope(userID int64, source string) (string, []any) {
	var conds []string
	var args []any
	if userID > 0 {
		conds = append(conds, "user_id = ?")
		args = append(args, userID)
	}
	if source != "" {
		conds = append(conds, "source = ?")
		args = append(args, source)
	}
	if len(conds) == 0 {
		return "", nil
	}
	return " AND " + strings.Join(conds, " AND "), args
}

// shardStaleScope 分片 upsert 的 (属主, 来源) 筛选:与 staleScope 同形,但
// user_id 谓词恒在——0 只命中未归属桶,不是"全表"(issue #143 跨用户隔离:
// 同名机场的 stale 标记/标签修剪/超期清理不得越过属主边界)。
func shardStaleScope(userID int64, source string) (string, []any) {
	return " AND user_id = ? AND source = ?", []any{userID, source}
}

// pruneStaleNodeTags 删除当前所有 stale 节点的自动标签(在 SaveNodePool 事务内调用)。
func pruneStaleNodeTags(tx *sql.Tx, userID int64) error {
	scope, args := staleScope(userID, "")
	return pruneStaleNodeTagsScoped(tx, scope, args)
}

// pruneStaleNodeTagsForSource 删除指定 (属主, 来源) 分片内 stale 节点的自动标签
// (在 UpsertNodePoolShard 事务内调用)。user_id 谓词恒在(0 = 未归属桶)。
func pruneStaleNodeTagsForSource(tx *sql.Tx, userID int64, source string) error {
	scope, args := shardStaleScope(userID, source)
	return pruneStaleNodeTagsScoped(tx, scope, args)
}

func pruneStaleNodeTagsScoped(tx *sql.Tx, scope string, args []any) error {
	query := `DELETE FROM node_tags WHERE node_key IN (SELECT node_key FROM nodes WHERE stale = 1` + scope + `)`
	if _, err := tx.Exec(query, args...); err != nil {
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
	return s.loadNodePoolQuery(`SELECT node_key, name, type, server, port, uuid, password, alter_id, cipher, network, tls,
	       sni, grpc_service_name, region, source, available, latency_ms, stale, last_seen,
	       detection_last_check, bandwidth_down, bandwidth_up, bandwidth_check, plugin, plugin_opts,
	       detection_kind, detection_fail_reason, detection_fail_detail, user_id,
	       flow, reality_public_key, reality_short_id, client_fingerprint, grpc_authority
		FROM nodes
		ORDER BY position`)
}

// LoadNodePoolByUser 读取指定用户分片的节点池快照(ticket 07)。
// 注:不含 user_id=0 未归属桶——未归属节点在 Invariant B 下应已回填超管;
// 仍在桶里的行按"不属于任何用户"处理,不与该用户数据混淆。
func (s *Store) LoadNodePoolByUser(userID int64) ([]*subscription.Node, error) {
	return s.loadNodePoolQuery(`SELECT node_key, name, type, server, port, uuid, password, alter_id, cipher, network, tls,
	       sni, grpc_service_name, region, source, available, latency_ms, stale, last_seen,
	       detection_last_check, bandwidth_down, bandwidth_up, bandwidth_check, plugin, plugin_opts,
	       detection_kind, detection_fail_reason, detection_fail_detail, user_id,
	       flow, reality_public_key, reality_short_id, client_fingerprint, grpc_authority
		FROM nodes
		WHERE user_id = ?
		ORDER BY position`, userID)
}

func (s *Store) loadNodePoolQuery(query string, args ...any) ([]*subscription.Node, error) {
	rows, err := s.db.Query(query, args...)
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
			&n.Plugin, &n.PluginOpts, &n.DetectionKind, &n.DetectionFailReason, &n.DetectionFailDetail,
			&n.UserID,
			&n.Flow, &n.RealityPublicKey, &n.RealityShortID, &n.ClientFingerprint, &n.GrpcAuthority,
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
