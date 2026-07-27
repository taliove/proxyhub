package store

import (
	"path/filepath"
	"testing"
)

// TestNodeOwnershipScope_PerUserBlocks 屏蔽名单按属主隔离(021 联合主键):
// 同一 node_key 可被两个用户独立屏蔽/取消,互不影响;读侧按用户各见各的。
func TestNodeOwnershipScope_PerUserBlocks(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	const key = "a.example.com:443"
	if err := st.BlockNodeForUser(1, key); err != nil {
		t.Fatalf("block user1: %v", err)
	}
	// 同 key 另一用户再屏蔽:联合主键下不冲突(单列主键时此行会静默 DO NOTHING)
	if err := st.BlockNodeForUser(2, key); err != nil {
		t.Fatalf("block user2: %v", err)
	}

	// user2 取消屏蔽不影响 user1
	if err := st.UnblockNodeForUser(2, key); err != nil {
		t.Fatalf("unblock user2: %v", err)
	}
	b1, err := st.ListBlockedNodesForUser(1)
	if err != nil {
		t.Fatalf("list user1: %v", err)
	}
	if !b1[key] {
		t.Error("user1 block lost after user2 unblocked same key")
	}
	b2, err := st.ListBlockedNodesForUser(2)
	if err != nil {
		t.Fatalf("list user2: %v", err)
	}
	if b2[key] {
		t.Error("user2 unblock did not take effect")
	}
}

// TestNodeOwnershipScope_PerUserOverrides 覆盖层按属主隔离(021 联合主键):
// 同一 node_key 两个用户各写各的覆盖,读侧按用户返回各自值。
func TestNodeOwnershipScope_PerUserOverrides(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	const key = "a.example.com:443"
	if err := st.SetNodeOverrideForUser(1, key, "admin-name", "HK"); err != nil {
		t.Fatalf("override user1: %v", err)
	}
	if err := st.SetNodeOverrideForUser(2, key, "member-name", "JP"); err != nil {
		t.Fatalf("override user2: %v", err)
	}

	o1, err := st.ListNodeOverridesForUser(1)
	if err != nil {
		t.Fatalf("list user1: %v", err)
	}
	if o1[key].DisplayName != "admin-name" || o1[key].Region != "HK" {
		t.Errorf("user1 override = %+v, want admin-name/HK", o1[key])
	}
	o2, err := st.ListNodeOverridesForUser(2)
	if err != nil {
		t.Fatalf("list user2: %v", err)
	}
	if o2[key].DisplayName != "member-name" || o2[key].Region != "JP" {
		t.Errorf("user2 override = %+v, want member-name/JP", o2[key])
	}

	// user1 清除不影响 user2
	if err := st.ClearNodeOverrideForUser(1, key); err != nil {
		t.Fatalf("clear user1: %v", err)
	}
	o2, _ = st.ListNodeOverridesForUser(2)
	if o2[key].DisplayName != "member-name" {
		t.Error("user2 override lost after user1 cleared same key")
	}
}

// TestMigrateNodeOwnershipScope_Idempotent 迁移幂等:Open 已执行一次,
// 再调 migrateNodeOwnershipScope 不重建、不丢数据。
func TestMigrateNodeOwnershipScope_Idempotent(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	if err := st.BlockNodeForUser(7, "a.example.com:443"); err != nil {
		t.Fatalf("seed block: %v", err)
	}
	if err := st.migrateNodeOwnershipScope(); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	b, _ := st.ListBlockedNodesForUser(7)
	if !b["a.example.com:443"] {
		t.Error("data lost after idempotent re-migration")
	}

	// 新装库主键已是联合形态
	scoped, err := st.hasUserIDInPK("node_blocks")
	if err != nil || !scoped {
		t.Errorf("node_blocks pk scoped = %v, err = %v, want true", scoped, err)
	}
	scoped, err = st.hasUserIDInPK("node_overrides")
	if err != nil || !scoped {
		t.Errorf("node_overrides pk scoped = %v, err = %v, want true", scoped, err)
	}
}
