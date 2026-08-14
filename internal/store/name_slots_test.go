package store

import (
	"errors"
	"testing"
)

func TestNameSlotCRUD(t *testing.T) {
	s := newTestStore(t)

	// 新建槽位(预建空槽),响应携带新 ID(issue #112)
	id, err := s.CreateNameSlotForUser(1, "🇭🇰 香港01", "", false)
	if err != nil {
		t.Fatalf("create empty slot: %v", err)
	}
	if id <= 0 {
		t.Fatalf("create returned id = %d, want positive", id)
	}
	sl, err := s.GetNameSlotByIDForUser(1, id)
	if err != nil {
		t.Fatalf("get slot by id: %v", err)
	}
	if sl.Name != "🇭🇰 香港01" || sl.NodeKey != "" {
		t.Errorf("slot = %+v, want name kept + empty node (预建空槽)", sl)
	}

	// 指派节点
	if err := s.UpdateNameSlotForUser(1, id, "", "hk.example.com:443", false); err != nil {
		t.Fatalf("assign node: %v", err)
	}
	sl, _ = s.GetNameSlotByIDForUser(1, id)
	if sl.NodeKey != "hk.example.com:443" {
		t.Errorf("NodeKey = %q, want hk.example.com:443", sl.NodeKey)
	}

	// 改名(ID 寻址,身份不随名字变)
	if err := s.UpdateNameSlotForUser(1, id, "🇭🇰 香港-主", "", false); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if _, err := s.GetNameSlotForUser(1, "🇭🇰 香港01"); !errors.Is(err, ErrSlotNotFound) {
		t.Errorf("old name should be gone, err = %v", err)
	}
	sl, _ = s.GetNameSlotByIDForUser(1, id)
	if sl.Name != "🇭🇰 香港-主" || sl.NodeKey != "hk.example.com:443" {
		t.Errorf("after rename slot = %+v, want new name + node kept", sl)
	}

	// 摘下变空槽,再删除
	if err := s.UnassignNameSlotForUser(1, id); err != nil {
		t.Fatalf("unassign: %v", err)
	}
	sl, _ = s.GetNameSlotByIDForUser(1, id)
	if sl.NodeKey != "" {
		t.Errorf("after unassign NodeKey = %q, want empty", sl.NodeKey)
	}
	if err := s.DeleteNameSlotForUser(1, id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := s.DeleteNameSlotForUser(1, id); !errors.Is(err, ErrSlotNotFound) {
		t.Errorf("delete missing slot: err = %v, want ErrSlotNotFound", err)
	}
}

func TestNameSlotConflicts(t *testing.T) {
	s := newTestStore(t)

	mustCreate := func(name, key string) int64 {
		t.Helper()
		id, err := s.CreateNameSlotForUser(1, name, key, false)
		if err != nil {
			t.Fatalf("create %q: %v", name, err)
		}
		return id
	}
	mustCreate("A", "a1.example.com:443")
	idB := mustCreate("B", "b1.example.com:443")

	// 名字冲突
	_, err := s.CreateNameSlotForUser(1, "A", "", false)
	var ce *SlotConflictError
	if !errors.As(err, &ce) || ce.Kind != SlotConflictName {
		t.Errorf("name conflict: err = %v, want SlotConflictName", err)
	}

	// 节点占用冲突:新建槽位挂已占用节点
	_, err = s.CreateNameSlotForUser(1, "C", "a1.example.com:443", false)
	if !errors.As(err, &ce) || ce.Kind != SlotConflictNode || ce.HolderName != "A" {
		t.Errorf("node conflict: err = %v, want SlotConflictNode with holder A", err)
	}

	// force 新建:节点从旧槽位摘下,旧槽位变空槽
	if _, err := s.CreateNameSlotForUser(1, "C", "a1.example.com:443", true); err != nil {
		t.Fatalf("force create: %v", err)
	}
	slA, _ := s.GetNameSlotForUser(1, "A")
	if slA.NodeKey != "" {
		t.Errorf("slot A NodeKey = %q, want evicted to empty", slA.NodeKey)
	}

	// 转移确认:把 B 指到别的节点,不 force 报 reassign
	err = s.UpdateNameSlotForUser(1, idB, "", "b2.example.com:8443", false)
	if !errors.As(err, &ce) || ce.Kind != SlotConflictReassign || ce.HolderNodeKey != "b1.example.com:443" {
		t.Errorf("reassign conflict: err = %v, want SlotConflictReassign with current node", err)
	}
	// force 转移成功
	if err := s.UpdateNameSlotForUser(1, idB, "", "b2.example.com:8443", true); err != nil {
		t.Fatalf("force reassign: %v", err)
	}

	// 改名到已存在的名字:不可 force
	err = s.UpdateNameSlotForUser(1, idB, "C", "", true)
	if !errors.As(err, &ce) || ce.Kind != SlotConflictName {
		t.Errorf("rename onto existing: err = %v, want SlotConflictName", err)
	}
}

func TestNameSlotUserIsolation(t *testing.T) {
	s := newTestStore(t)

	// 不同用户可用同名/同节点,互不串扰
	if _, err := s.CreateNameSlotForUser(1, "X", "n.example.com:443", false); err != nil {
		t.Fatalf("user1 create: %v", err)
	}
	if _, err := s.CreateNameSlotForUser(2, "X", "n.example.com:443", false); err != nil {
		t.Fatalf("user2 create same name+node should be allowed: %v", err)
	}
	slots, err := s.ListNameSlotsForUser(1)
	if err != nil || len(slots) != 1 {
		t.Errorf("user1 slots = %v, %v; want 1", slots, err)
	}
	all, _ := s.ListNameSlotsForUser(0)
	if len(all) != 2 {
		t.Errorf("admin view slots = %d, want 2", len(all))
	}
}

func TestSlotNameByNodeKey(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateNameSlotForUser(1, "有名", "k.example.com:443", false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateNameSlotForUser(1, "空槽", "", false); err != nil {
		t.Fatal(err)
	}
	m, err := s.SlotNameByNodeKeyForUser(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 1 || m["k.example.com:443"] != "有名" {
		t.Errorf("map = %v, want only assigned slot", m)
	}
}

func TestMigrateOverridesToNameSlots(t *testing.T) {
	s := newTestStore(t)

	// 存量覆盖:一个正常行 + 一组同名冲突(两行同名不同节点)
	if err := s.SetNodeOverrideForUser(1, "hk1.example.com:443", "🇭🇰 香港01", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.SetNodeOverrideForUser(1, "old.example.com:1", "重名", ""); err != nil {
		t.Fatal(err)
	}
	// 保证后者 updated_at 更新——直接改库,避免同秒时间戳比较不稳定
	if _, err := s.db.Exec(
		`UPDATE node_overrides SET updated_at = datetime('now', '-1 hour') WHERE user_id = 1 AND node_key = 'old.example.com:1'`); err != nil {
		t.Fatal(err)
	}
	if err := s.SetNodeOverrideForUser(1, "new.example.com:2", "重名", ""); err != nil {
		t.Fatal(err)
	}

	if err := s.migrateOverridesToNameSlots(); err != nil {
		t.Fatalf("backfill: %v", err)
	}

	// 正常行迁成槽位
	sl, err := s.GetNameSlotForUser(1, "🇭🇰 香港01")
	if err != nil || sl.NodeKey != "hk1.example.com:443" {
		t.Errorf("migrated slot = %+v, %v", sl, err)
	}

	// 同名冲突:new 更新,占住槽位
	sl, err = s.GetNameSlotForUser(1, "重名")
	if err != nil || sl.NodeKey != "new.example.com:2" {
		t.Errorf("conflict winner = %+v, %v; want new.example.com:2", sl, err)
	}

	// 落选行成为待处理冲突;赢家行 display_name 已清,不会误入冲突区
	conflicts, err := s.ListSlotMigrationConflictsForUser(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(conflicts) != 1 || conflicts[0].NodeKey != "old.example.com:1" {
		t.Errorf("conflicts = %+v, want only old.example.com:1", conflicts)
	}

	// 赢家行的 display_name 已被回填清理(issue #96:命名链切槽位层后旧值不再消费);
	// 落选冲突行保留 display_name 供人工待处理露出
	overrides, err := s.ListNodeOverridesForUser(1)
	if err != nil {
		t.Fatal(err)
	}
	if got := overrides["hk1.example.com:443"].DisplayName; got != "" {
		t.Errorf("winner display_name = %q, want cleared", got)
	}
	if got := overrides["old.example.com:1"].DisplayName; got != "重名" {
		t.Errorf("loser display_name = %q, want preserved for manual resolution", got)
	}

	// 幂等:再跑一次,槽位数不变,无报错
	if err := s.migrateOverridesToNameSlots(); err != nil {
		t.Fatalf("re-run backfill: %v", err)
	}
	slots, _ := s.ListNameSlotsForUser(1)
	if len(slots) != 2 {
		t.Errorf("after re-run slots = %d, want 2", len(slots))
	}
}

// TestNameSlotIndexTemplateDuplicates issue #113:含 {index} 的模板名查重放行——
// 同模板可建多槽(挂不同节点或空槽);不含 {index} 的字面名重名仍 409 冲突。
func TestNameSlotIndexTemplateDuplicates(t *testing.T) {
	s := newTestStore(t)

	// 同模板(含 {index})可建多个:挂不同节点 + 预建空槽
	id1, err := s.CreateNameSlotForUser(1, "主节点-{region}-{index}", "a.example.com:443", false)
	if err != nil {
		t.Fatalf("first template slot: %v", err)
	}
	id2, err := s.CreateNameSlotForUser(1, "主节点-{region}-{index}", "b.example.com:443", false)
	if err != nil {
		t.Fatalf("duplicate template name should be allowed: %v", err)
	}
	id3, err := s.CreateNameSlotForUser(1, "主节点-{region}-{index}", "", false)
	if err != nil {
		t.Fatalf("empty slot with duplicate template name should be allowed: %v", err)
	}
	if id1 == id2 || id2 == id3 {
		t.Errorf("ids must be distinct: %d %d %d", id1, id2, id3)
	}
	if !(id1 < id2 && id2 < id3) {
		t.Errorf("id order should follow creation order: %d %d %d", id1, id2, id3)
	}

	// 不含 {index} 的字面名重名仍冲突
	if _, err := s.CreateNameSlotForUser(1, "字面名", "", false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateNameSlotForUser(1, "字面名", "", false); err == nil {
		t.Error("duplicate literal name should conflict")
	} else {
		var ce *SlotConflictError
		if !errors.As(err, &ce) || ce.Kind != SlotConflictName {
			t.Errorf("literal dup: err = %v, want SlotConflictName", err)
		}
	}

	// 改名:改到已存在的 {index} 模板名放行;改到已存在字面名仍冲突
	if err := s.UpdateNameSlotForUser(1, id3, "主节点-{region}-{index}", "", false); err != nil {
		t.Errorf("rename onto existing {index} template should be allowed: %v", err)
	}
	if err := s.UpdateNameSlotForUser(1, id1, "字面名", "", false); err == nil {
		t.Error("rename onto existing literal name should conflict")
	}

	// 多租户:同一 {index} 模板名在其他用户空间同样可建(隔离不变)
	if _, err := s.CreateNameSlotForUser(2, "主节点-{region}-{index}", "a.example.com:443", false); err != nil {
		t.Errorf("other user same template name should be allowed: %v", err)
	}
}

// 无 id 列)重建为自增 ID 主键;存量行数据与创建时间无损,ID 序 = 创建顺序;
// 字面名部分唯一索引就位(重名 {index} 模板在 DB 层已放行,#113 的应用层放行
// 才会真正用到);迁移幂等。
func TestMigrateNameSlotsIdentity(t *testing.T) {
	s := newTestStore(t)

	// 模拟旧表结构(028 形态:(user_id, name) 主键,无 id)
	mustExec := func(q string, args ...any) {
		t.Helper()
		if _, err := s.db.Exec(q, args...); err != nil {
			t.Fatalf("exec %q: %v", q, err)
		}
	}
	mustExec(`DROP TABLE name_slots`)
	mustExec(`CREATE TABLE name_slots (
		user_id    INTEGER NOT NULL DEFAULT 0,
		name       TEXT NOT NULL,
		node_key   TEXT NOT NULL DEFAULT '',
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (user_id, name)
	)`)
	mustExec(`CREATE UNIQUE INDEX idx_name_slots_node
		ON name_slots(user_id, node_key) WHERE node_key != ''`)
	mustExec(`INSERT INTO name_slots (user_id, name, node_key, created_at, updated_at)
		VALUES (1, '先建', 'a.example.com:443', '2026-01-01 08:00:00', '2026-01-01 08:00:00')`)
	mustExec(`INSERT INTO name_slots (user_id, name, node_key, created_at, updated_at)
		VALUES (1, '后建', '', '2026-02-02 09:00:00', '2026-02-02 09:00:00')`)

	if err := s.migrateNameSlotsIdentity(); err != nil {
		t.Fatalf("identity migration: %v", err)
	}

	slots, err := s.ListNameSlotsForUser(1)
	if err != nil || len(slots) != 2 {
		t.Fatalf("slots = %v, %v; want 2", slots, err)
	}
	var first, second NameSlot
	for _, sl := range slots {
		if sl.ID <= 0 {
			t.Errorf("slot %q missing assigned id", sl.Name)
		}
		if sl.Name == "先建" {
			first = sl
		} else {
			second = sl
		}
	}
	// 数据无损:created_at 原样保留
	if got := first.CreatedAt.Format("2006-01-02 15:04:05"); got != "2026-01-01 08:00:00" {
		t.Errorf("先建 created_at = %q, want 2026-01-01 08:00:00", got)
	}
	if first.NodeKey != "a.example.com:443" {
		t.Errorf("先建 node_key = %q, want kept", first.NodeKey)
	}
	// ID 序 = 创建顺序(按 rowid 拷贝)
	if first.ID >= second.ID {
		t.Errorf("id order: 先建 id=%d should be < 后建 id=%d", first.ID, second.ID)
	}

	// 字面名部分唯一索引:重名字面名被拒,重名 {index} 模板放行(DB 层就绪)
	if _, err := s.db.Exec(
		`INSERT INTO name_slots (user_id, name) VALUES (1, '先建')`); err == nil {
		t.Error("duplicate literal name should violate idx_name_slots_name_literal")
	}
	if _, err := s.db.Exec(
		`INSERT INTO name_slots (user_id, name) VALUES (1, '主节点-{index}')`); err != nil {
		t.Fatalf("first {index} template insert: %v", err)
	}
	if _, err := s.db.Exec(
		`INSERT INTO name_slots (user_id, name) VALUES (1, '主节点-{index}')`); err != nil {
		t.Errorf("duplicate {index} template should be allowed by partial index: %v", err)
	}

	// 幂等:再跑一次,无报错、数据不变
	if err := s.migrateNameSlotsIdentity(); err != nil {
		t.Fatalf("re-run identity migration: %v", err)
	}
	slots, _ = s.ListNameSlotsForUser(1)
	if len(slots) != 4 {
		t.Errorf("after re-run slots = %d, want 4", len(slots))
	}
}
