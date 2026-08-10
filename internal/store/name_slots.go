package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// 节点名称槽位(ADR 0047 / issue #95):命名所有权从节点翻转为用户资产。
// (user_id, name) 主键,名字指向当前占用它的 node_key;空 node_key = 预建空槽。
// 双向唯一由 DB 约束保证(主键 + idx_name_slots_node 部分唯一索引)。

// NameSlot 名称槽位
type NameSlot struct {
	UserID    int64     `json:"user_id"`
	Name      string    `json:"name"`
	NodeKey   string    `json:"node_key"` // 空串 = 空槽(未指派节点)
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// SlotConflictKind 槽位冲突类型(409 载荷用,前端据此弹确认)
type SlotConflictKind string

const (
	// SlotConflictName 名字已被占用(新建/改名目标名已存在)
	SlotConflictName SlotConflictKind = "name_taken"
	// SlotConflictNode 节点已占别的槽位(一个节点只能挂一个名字)
	SlotConflictNode SlotConflictKind = "node_occupied"
	// SlotConflictReassign 名字当前挂在别的节点上(转移需确认)
	SlotConflictReassign SlotConflictKind = "reassign"
)

// SlotConflictError 槽位双向唯一冲突。HolderName/HolderNodeKey 带出当前占用方,
// 供前端确认文案("该节点当前叫 X"/"该名字当前挂在节点 Y")。
type SlotConflictError struct {
	Kind          SlotConflictKind
	Name          string // 请求的名字
	NodeKey       string // 请求的节点
	HolderName    string // 占用该节点的槽位名(Kind=node_occupied 时非空)
	HolderNodeKey string // 该名字当前挂载的节点(Kind=reassign 时非空)
}

func (e *SlotConflictError) Error() string {
	switch e.Kind {
	case SlotConflictName:
		return fmt.Sprintf("slot name %q already exists", e.Name)
	case SlotConflictNode:
		return fmt.Sprintf("node %q already occupies slot %q", e.NodeKey, e.HolderName)
	default:
		return fmt.Sprintf("slot %q is bound to another node %q", e.Name, e.HolderNodeKey)
	}
}

// ErrSlotNotFound 槽位不存在
var ErrSlotNotFound = errors.New("name slot not found")

// ErrSlotNameEmpty 名字为空
var ErrSlotNameEmpty = errors.New("slot name required")

// maxSlotNameRunes 槽位名 rune 上限:展示卫生,与精选项别名同规格(50)。
const maxSlotNameRunes = 50

// SanitizeSlotName 槽位名边界归一(trim/去控制字符/rune 截断,剔除 '/')。
// 与精选项别名/公开名称同一 sanitizer,不让脏数据落库;'/' 会破坏
// /api/slots/{name} 单段路由寻址,必须剔除。空串由调用方判 400。
func SanitizeSlotName(name string) string {
	return strings.ReplaceAll(sanitizeDisplayText(name, maxSlotNameRunes), "/", "")
}

// parseSlotTime 解析时间列。与 overrides.go 同坑:历史写入格式不止一种
// (time.Time 直写 / RFC3339),两种都认,解析失败留零值。
func parseSlotTime(s string) time.Time {
	if t, err := time.Parse("2006-01-02 15:04:05.999999999-07:00", s); err == nil {
		return t
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	return time.Time{}
}

// ListNameSlotsForUser 读取指定用户的全部槽位;userID<=0 返回全量(超管视角)。
// 按名字排序,输出稳定。
func (s *Store) ListNameSlotsForUser(userID int64) ([]NameSlot, error) {
	query := `SELECT user_id, name, node_key, created_at, updated_at FROM name_slots`
	args := []any{}
	if userID > 0 {
		query += ` WHERE user_id = ?`
		args = append(args, userID)
	}
	query += ` ORDER BY name`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := []NameSlot{}
	for rows.Next() {
		var sl NameSlot
		var createdStr, updatedStr string
		if err := rows.Scan(&sl.UserID, &sl.Name, &sl.NodeKey, &createdStr, &updatedStr); err != nil {
			return nil, err
		}
		sl.CreatedAt = parseSlotTime(createdStr)
		sl.UpdatedAt = parseSlotTime(updatedStr)
		result = append(result, sl)
	}
	return result, rows.Err()
}

// GetNameSlotForUser 读单个槽位;不存在返回 ErrSlotNotFound。
func (s *Store) GetNameSlotForUser(userID int64, name string) (NameSlot, error) {
	var sl NameSlot
	var createdStr, updatedStr string
	err := s.db.QueryRow(
		`SELECT user_id, name, node_key, created_at, updated_at FROM name_slots WHERE user_id = ? AND name = ?`,
		userID, name).
		Scan(&sl.UserID, &sl.Name, &sl.NodeKey, &createdStr, &updatedStr)
	if errors.Is(err, sql.ErrNoRows) {
		return NameSlot{}, ErrSlotNotFound
	}
	if err != nil {
		return NameSlot{}, err
	}
	sl.CreatedAt = parseSlotTime(createdStr)
	sl.UpdatedAt = parseSlotTime(updatedStr)
	return sl, nil
}

// slotNameByNodeKey 反查节点当前占用的槽位名;未占用返回 ("", nil)。
func (s *Store) slotNameByNodeKey(userID int64, nodeKey string) (string, error) {
	var name string
	err := s.db.QueryRow(
		`SELECT name FROM name_slots WHERE user_id = ? AND node_key = ?`,
		userID, nodeKey).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil // 无行 = 未占用
	}
	if err != nil {
		return "", err
	}
	return name, nil
}

// checkNodeOccupied 指派前校验节点是否已占别的槽位;占用且非自身且未 force 时报冲突。
func (s *Store) checkNodeOccupied(userID int64, nodeKey, selfName string, force bool) error {
	if nodeKey == "" {
		return nil
	}
	holder, err := s.slotNameByNodeKey(userID, nodeKey)
	if err != nil {
		return err
	}
	if holder != "" && holder != selfName && !force {
		return &SlotConflictError{
			Kind: SlotConflictNode, Name: selfName, NodeKey: nodeKey, HolderName: holder,
		}
	}
	return nil
}

// evictNodeFromOtherSlots force 语义配套:把该节点从其它槽位上摘下(旧槽位变空槽)。
func (s *Store) evictNodeFromOtherSlots(userID int64, nodeKey, selfName string) error {
	_, err := s.db.Exec(
		`UPDATE name_slots SET node_key = '', updated_at = ? WHERE user_id = ? AND node_key = ? AND name != ?`,
		time.Now(), userID, nodeKey, selfName)
	return err
}

// CreateNameSlotForUser 新建槽位。nodeKey 空 = 预建空槽。
// 冲突(名字已存在/节点被占)返回 *SlotConflictError;force=true 时把节点从
// 旧槽位摘下(旧槽位变空槽),名字已存在不可 force。
//
// TODO(issue #97):并发下 check-then-act 竞态由 DB 唯一约束兜底,但冒出的将是
// 裸 sqlite UNIQUE 错误——API 层需把 "UNIQUE constraint failed: name_slots.*"
// 翻译成 *SlotConflictError,否则 409 载荷偶发丢失。
func (s *Store) CreateNameSlotForUser(userID int64, name, nodeKey string, force bool) error {
	if name == "" {
		return ErrSlotNameEmpty
	}
	if _, err := s.GetNameSlotForUser(userID, name); err == nil {
		return &SlotConflictError{Kind: SlotConflictName, Name: name, NodeKey: nodeKey}
	}
	if err := s.checkNodeOccupied(userID, nodeKey, name, force); err != nil {
		return err
	}
	if force {
		if err := s.evictNodeFromOtherSlots(userID, nodeKey, name); err != nil {
			return err
		}
	}
	_, err := s.db.Exec(
		`INSERT INTO name_slots (user_id, name, node_key, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		userID, name, nodeKey, time.Now(), time.Now())
	return err
}

// UpdateNameSlotForUser 改名/指派/转移。newName 非空 = 改名;nodeKey 非空 = 指派
// (重新指向)。两者至少其一,否则空操作。冲突语义:
// - 改名目标已存在:SlotConflictName(不可 force——不静默毁掉别人的槽位);
// - 节点被别的槽位占用:SlotConflictNode,force 时摘下旧槽位;
// - 名字当前挂在别的节点上:SlotConflictReassign,force 时执行转移。
func (s *Store) UpdateNameSlotForUser(userID int64, name, newName, nodeKey string, force bool) error {
	if name == "" {
		return ErrSlotNameEmpty
	}
	cur, err := s.GetNameSlotForUser(userID, name)
	if err != nil {
		return err
	}
	if newName == "" {
		newName = name
	}
	if newName != name {
		if _, err := s.GetNameSlotForUser(userID, newName); err == nil {
			return &SlotConflictError{Kind: SlotConflictName, Name: newName, NodeKey: nodeKey}
		}
	}
	if nodeKey == "" {
		nodeKey = cur.NodeKey
	}
	if nodeKey != cur.NodeKey && cur.NodeKey != "" && !force {
		return &SlotConflictError{
			Kind: SlotConflictReassign, Name: name, NodeKey: nodeKey, HolderNodeKey: cur.NodeKey,
		}
	}
	if err := s.checkNodeOccupied(userID, nodeKey, name, force); err != nil {
		return err
	}
	if force {
		if err := s.evictNodeFromOtherSlots(userID, nodeKey, name); err != nil {
			return err
		}
	}
	res, err := s.db.Exec(
		`UPDATE name_slots SET name = ?, node_key = ?, updated_at = ? WHERE user_id = ? AND name = ?`,
		newName, nodeKey, time.Now(), userID, name)
	if err != nil {
		return err
	}
	if n, rerr := res.RowsAffected(); rerr == nil && n == 0 {
		return ErrSlotNotFound
	}
	return nil
}

// UnassignNameSlotForUser 摘下节点,槽位变空槽(保留名字)。
func (s *Store) UnassignNameSlotForUser(userID int64, name string) error {
	res, err := s.db.Exec(
		`UPDATE name_slots SET node_key = '', updated_at = ? WHERE user_id = ? AND name = ?`,
		time.Now(), userID, name)
	if err != nil {
		return err
	}
	if n, rerr := res.RowsAffected(); rerr == nil && n == 0 {
		return ErrSlotNotFound
	}
	return nil
}

// DeleteNameSlotForUser 删除槽位;节点回退模板/原名(命名链职责,不在本层)。
func (s *Store) DeleteNameSlotForUser(userID int64, name string) error {
	res, err := s.db.Exec(`DELETE FROM name_slots WHERE user_id = ? AND name = ?`, userID, name)
	if err != nil {
		return err
	}
	if n, rerr := res.RowsAffected(); rerr == nil && n == 0 {
		return ErrSlotNotFound
	}
	return nil
}

// SlotNameByNodeKeyForUser 读指定用户的 node_key → 槽位名映射(命名链求值用,
// issue #96)。只含已指派的槽位(空槽无节点可查)。
func (s *Store) SlotNameByNodeKeyForUser(userID int64) (map[string]string, error) {
	rows, err := s.db.Query(
		`SELECT node_key, name FROM name_slots WHERE user_id = ? AND node_key != ''`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]string)
	for rows.Next() {
		var key, name string
		if err := rows.Scan(&key, &name); err != nil {
			return nil, err
		}
		result[key] = name
	}
	return result, rows.Err()
}

// migrateOverridesToNameSlots 存量回填(issue #95):node_overrides 中
// display_name != '' 的行升级为槽位。幂等,随 migrate() 每次启动执行。
//
// 同名冲突(同一用户给多个节点起过同名):updated_at 最新者占住槽位,其余行
// 保留 display_name 不动,由 ListSlotMigrationConflictsForUser 露出走人工处理——
// 不替用户猜哪个是对的。
//
// 清理(issue #96):命名链已切换为槽位层实时叠加,旧 applyOverrides 不再消费
// display_name——已迁移赢家(存在同名同节点槽位)的 display_name 在此清空,
// 避免残留旧值造成"两套命名来源"的误读;落选冲突行没有对应槽位,不受影响。
func (s *Store) migrateOverridesToNameSlots() error {
	rows, err := s.db.Query(
		`SELECT user_id, node_key, display_name, updated_at FROM node_overrides WHERE display_name != ''`)
	if err != nil {
		return err
	}
	defer rows.Close()

	type overrideRow struct {
		userID   int64
		nodeKey  string
		name     string
		updated  time.Time
	}
	// 按 (user_id, display_name) 分组,组内取 updated_at 最新者为迁移赢家。
	// 平局(updated_at 完全相等)取先扫到者——不确定但影响极小,落选对家照样
	// 进待处理冲突区,人工兜底语义不变。
	winners := make(map[string]overrideRow)
	for rows.Next() {
		var r overrideRow
		var updatedStr string
		if err := rows.Scan(&r.userID, &r.nodeKey, &r.name, &updatedStr); err != nil {
			return err
		}
		r.updated = parseSlotTime(updatedStr)
		key := fmt.Sprintf("%d\x00%s", r.userID, r.name)
		if cur, ok := winners[key]; !ok || r.updated.After(cur.updated) {
			winners[key] = r
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, w := range winners {
		// 名字已存在(上轮已迁移/用户已建)或节点已被占:跳过,行保留为待处理
		if _, err := s.GetNameSlotForUser(w.userID, w.name); err == nil {
			continue
		}
		if holder, err := s.slotNameByNodeKey(w.userID, w.nodeKey); err != nil {
			return err
		} else if holder != "" {
			continue
		}
		if _, err := s.db.Exec(
			`INSERT INTO name_slots (user_id, name, node_key, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
			w.userID, w.name, w.nodeKey, time.Now(), time.Now()); err != nil {
			return err
		}
	}

	// 清理已迁移赢家的 display_name(issue #96):命名链已切到槽位层,旧值不再被消费。
	// 只清"存在同名同节点槽位"的行;落选冲突行无对应槽位,保留待人工处理。
	_, err = s.db.Exec(`
		UPDATE node_overrides SET display_name = ''
		WHERE display_name != '' AND EXISTS (
			SELECT 1 FROM name_slots sl
			WHERE sl.user_id = node_overrides.user_id
			  AND sl.name = node_overrides.display_name
			  AND sl.node_key = node_overrides.node_key
		)`)
	return err
}

// ListSlotMigrationConflictsForUser 列出迁移待处理冲突:display_name 残留
// (回填没能迁走的行——同名竞争落选或名字/节点已被占)。userID<=0 返回全量。
// 已迁移赢家的 display_name 已被回填清理(issue #96),残留即冲突。
func (s *Store) ListSlotMigrationConflictsForUser(userID int64) ([]NodeOverride, error) {
	query := `
		SELECT o.node_key, o.display_name, o.region, o.favorite, o.updated_at
		FROM node_overrides o
		WHERE o.display_name != ''
		  AND NOT EXISTS (
			SELECT 1 FROM name_slots sl
			WHERE sl.user_id = o.user_id AND sl.name = o.display_name AND sl.node_key = o.node_key
		  )`
	args := []any{}
	if userID > 0 {
		query += ` AND o.user_id = ?`
		args = append(args, userID)
	}
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := []NodeOverride{}
	for rows.Next() {
		var o NodeOverride
		var fav int
		var updatedStr string
		if err := rows.Scan(&o.NodeKey, &o.DisplayName, &o.Region, &fav, &updatedStr); err != nil {
			return nil, err
		}
		o.Favorite = fav != 0
		o.UpdatedAt = parseSlotTime(updatedStr)
		result = append(result, o)
	}
	return result, rows.Err()
}
