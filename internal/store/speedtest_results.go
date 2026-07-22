package store

import (
	"database/sql"
	"fmt"
	"time"
)

// speedtestRetention 每 key(含直连桶)保留的最近实测记录条数,写入后超出即修剪。
const speedtestRetention = 50

// SpeedtestResult 一条本机实测历史记录。
// NodeKey 空串 = 直连/未标注(落库为 NULL);节点已不在池的孤儿历史照常读出。
type SpeedtestResult struct {
	ID            int64     `json:"id"`
	NodeKey       string    `json:"node_key"` // 空 = 直连/未标注
	DownMbps      float64   `json:"down_mbps"`
	UpMbps        float64   `json:"up_mbps"`
	IdleLatencyMs float64   `json:"idle_latency_ms"`
	JitterMs      float64   `json:"jitter_ms"`
	ClientInfo    string    `json:"client_info"`
	CreatedAt     time.Time `json:"created_at"`
}

// SaveSpeedtestResult 追加一条实测历史并修剪该 key 至最近 speedtestRetention 条。
// 直连(空 NodeKey)是独立的修剪桶,与节点 key 互不影响。返回新行 id。
func (s *Store) SaveSpeedtestResult(res SpeedtestResult) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin speedtest result tx: %w", err)
	}
	defer tx.Rollback()

	// 空 NodeKey 落 NULL,直连桶与具体 key 在 SQL 层就是两个桶
	var nodeKey any
	if res.NodeKey != "" {
		nodeKey = res.NodeKey
	}
	sqlRes, err := tx.Exec(`
		INSERT INTO speedtest_results (node_key, down_mbps, up_mbps, idle_latency_ms, jitter_ms, client_info, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		nodeKey, res.DownMbps, res.UpMbps, res.IdleLatencyMs, res.JitterMs, res.ClientInfo, time.Now(),
	)
	if err != nil {
		return 0, fmt.Errorf("insert speedtest result: %w", err)
	}
	id, err := sqlRes.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("last insert id: %w", err)
	}

	// 修剪:仅保留该桶最近 speedtestRetention 条(id 单调递增=写入顺序),其余删除。
	if res.NodeKey == "" {
		if _, err := tx.Exec(`
			DELETE FROM speedtest_results
			WHERE node_key IS NULL
			  AND id NOT IN (
				SELECT id FROM speedtest_results
				WHERE node_key IS NULL
				ORDER BY id DESC
				LIMIT ?
			)`, speedtestRetention); err != nil {
			return 0, fmt.Errorf("trim direct speedtest results: %w", err)
		}
	} else {
		if _, err := tx.Exec(`
			DELETE FROM speedtest_results
			WHERE node_key = ?
			  AND id NOT IN (
				SELECT id FROM speedtest_results
				WHERE node_key = ?
				ORDER BY id DESC
				LIMIT ?
			)`, res.NodeKey, res.NodeKey, speedtestRetention); err != nil {
			return 0, fmt.Errorf("trim speedtest results: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit speedtest result: %w", err)
	}
	return id, nil
}

// ListSpeedtestResults 返回实测历史(时间倒序 id DESC);无记录返回空切片。
// nodeKey 为 nil 时返回全部(含直连与孤儿历史);指向空串时只返回直连桶;
// 指向具体 key 时只返回该 key 的历史。
func (s *Store) ListSpeedtestResults(nodeKey *string) ([]SpeedtestResult, error) {
	var rows *sql.Rows
	var err error
	switch {
	case nodeKey == nil:
		rows, err = s.db.Query(`
			SELECT id, node_key, down_mbps, up_mbps, idle_latency_ms, jitter_ms, client_info, created_at
			FROM speedtest_results
			ORDER BY id DESC`)
	case *nodeKey == "":
		rows, err = s.db.Query(`
			SELECT id, node_key, down_mbps, up_mbps, idle_latency_ms, jitter_ms, client_info, created_at
			FROM speedtest_results
			WHERE node_key IS NULL
			ORDER BY id DESC`)
	default:
		rows, err = s.db.Query(`
			SELECT id, node_key, down_mbps, up_mbps, idle_latency_ms, jitter_ms, client_info, created_at
			FROM speedtest_results
			WHERE node_key = ?
			ORDER BY id DESC`, *nodeKey)
	}
	if err != nil {
		return nil, fmt.Errorf("query speedtest results: %w", err)
	}
	defer rows.Close()

	entries := make([]SpeedtestResult, 0)
	for rows.Next() {
		var (
			entry      SpeedtestResult
			nullKey    sql.NullString
			createdStr string
		)
		if err := rows.Scan(&entry.ID, &nullKey, &entry.DownMbps, &entry.UpMbps,
			&entry.IdleLatencyMs, &entry.JitterMs, &entry.ClientInfo, &createdStr); err != nil {
			return nil, fmt.Errorf("scan speedtest result: %w", err)
		}
		entry.NodeKey = nullKey.String
		entry.CreatedAt = parseTimeOrZero(&createdStr)
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

// DeleteSpeedtestResult 按 id 删除一条实测历史;不存在返回 ErrNotFound。
func (s *Store) DeleteSpeedtestResult(id int64) error {
	res, err := s.db.Exec(`DELETE FROM speedtest_results WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete speedtest result: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete speedtest result rows affected: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}
