package store

import (
	"errors"
	"testing"
)

func TestNameSlotCRUD(t *testing.T) {
	s := newTestStore(t)

	// 新建槽位(预建空槽)
	if err := s.CreateNameSlotForUser(1, "🇭🇰 香港01", "", false); err != nil {
		t.Fatalf("create empty slot: %v", err)
	}
	sl, err := s.GetNameSlotForUser(1, "🇭🇰 香港01")
	if err != nil {
		t.Fatalf("get slot: %v", err)
	}
	if sl.NodeKey != "" {
		t.Errorf("NodeKey = %q, want empty (预建空槽)", sl.NodeKey)
	}

	// 指派节点
	if err := s.UpdateNameSlotForUser(1, "🇭🇰 香港01", "", "hk.example.com:443", false); err != nil {
		t.Fatalf("assign node: %v", err)
	}
	sl, _ = s.GetNameSlotForUser(1, "🇭🇰 香港01")
	if sl.NodeKey != "hk.example.com:443" {
		t.Errorf("NodeKey = %q, want hk.example.com:443", sl.NodeKey)
	}

	// 改名
	if err := s.UpdateNameSlotForUser(1, "🇭🇰 香港01", "🇭🇰 香港-主", "", false); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if _, err := s.GetNameSlotForUser(1, "🇭🇰 香港01"); !errors.Is(err, ErrSlotNotFound) {
		t.Errorf("old name should be gone, err = %v", err)
	}
	sl, _ = s.GetNameSlotForUser(1, "🇭🇰 香港-主")
	if sl.NodeKey != "hk.example.com:443" {
		t.Errorf("after rename NodeKey = %q, want kept", sl.NodeKey)
	}

	// 摘下变空槽,再删除
	if err := s.UnassignNameSlotForUser(1, "🇭🇰 香港-主"); err != nil {
		t.Fatalf("unassign: %v", err)
	}
	sl, _ = s.GetNameSlotForUser(1, "🇭🇰 香港-主")
	if sl.NodeKey != "" {
		t.Errorf("after unassign NodeKey = %q, want empty", sl.NodeKey)
	}
	if err := s.DeleteNameSlotForUser(1, "🇭🇰 香港-主"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := s.DeleteNameSlotForUser(1, "🇭🇰 香港-主"); !errors.Is(err, ErrSlotNotFound) {
		t.Errorf("delete missing slot: err = %v, want ErrSlotNotFound", err)
	}
}

func TestNameSlotConflicts(t *testing.T) {
	s := newTestStore(t)

	mustCreate := func(name, key string) {
		t.Helper()
		if err := s.CreateNameSlotForUser(1, name, key, false); err != nil {
			t.Fatalf("create %q: %v", name, err)
		}
	}
	mustCreate("A", "a1.example.com:443")
	mustCreate("B", "b1.example.com:443")

	// 名字冲突
	err := s.CreateNameSlotForUser(1, "A", "", false)
	var ce *SlotConflictError
	if !errors.As(err, &ce) || ce.Kind != SlotConflictName {
		t.Errorf("name conflict: err = %v, want SlotConflictName", err)
	}

	// 节点占用冲突:新建槽位挂已占用节点
	err = s.CreateNameSlotForUser(1, "C", "a1.example.com:443", false)
	if !errors.As(err, &ce) || ce.Kind != SlotConflictNode || ce.HolderName != "A" {
		t.Errorf("node conflict: err = %v, want SlotConflictNode with holder A", err)
	}

	// force 新建:节点从旧槽位摘下,旧槽位变空槽
	if err := s.CreateNameSlotForUser(1, "C", "a1.example.com:443", true); err != nil {
		t.Fatalf("force create: %v", err)
	}
	slA, _ := s.GetNameSlotForUser(1, "A")
	if slA.NodeKey != "" {
		t.Errorf("slot A NodeKey = %q, want evicted to empty", slA.NodeKey)
	}

	// 转移确认:把 B 指到别的节点,不 force 报 reassign
	err = s.UpdateNameSlotForUser(1, "B", "", "b2.example.com:8443", false)
	if !errors.As(err, &ce) || ce.Kind != SlotConflictReassign || ce.HolderNodeKey != "b1.example.com:443" {
		t.Errorf("reassign conflict: err = %v, want SlotConflictReassign with current node", err)
	}
	// force 转移成功
	if err := s.UpdateNameSlotForUser(1, "B", "", "b2.example.com:8443", true); err != nil {
		t.Fatalf("force reassign: %v", err)
	}

	// 改名到已存在的名字:不可 force
	err = s.UpdateNameSlotForUser(1, "B", "C", "", true)
	if !errors.As(err, &ce) || ce.Kind != SlotConflictName {
		t.Errorf("rename onto existing: err = %v, want SlotConflictName", err)
	}
}

func TestNameSlotUserIsolation(t *testing.T) {
	s := newTestStore(t)

	// 不同用户可用同名/同节点,互不串扰
	if err := s.CreateNameSlotForUser(1, "X", "n.example.com:443", false); err != nil {
		t.Fatalf("user1 create: %v", err)
	}
	if err := s.CreateNameSlotForUser(2, "X", "n.example.com:443", false); err != nil {
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
	if err := s.CreateNameSlotForUser(1, "有名", "k.example.com:443", false); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateNameSlotForUser(1, "空槽", "", false); err != nil {
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
