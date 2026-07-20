package store

import "testing"

func TestNodeBlocks_BlockAndList(t *testing.T) {
	st := newTestStore(t)

	if err := st.BlockNode("1.2.3.4:443"); err != nil {
		t.Fatalf("BlockNode() error = %v", err)
	}
	if err := st.BlockNode("5.6.7.8:8388"); err != nil {
		t.Fatalf("BlockNode() error = %v", err)
	}

	blocked, err := st.ListBlockedNodes()
	if err != nil {
		t.Fatalf("ListBlockedNodes() error = %v", err)
	}
	if len(blocked) != 2 {
		t.Fatalf("len(blocked) = %d, want 2", len(blocked))
	}
	if !blocked["1.2.3.4:443"] || !blocked["5.6.7.8:8388"] {
		t.Errorf("blocked set missing entries: %v", blocked)
	}
}

func TestNodeBlocks_BlockIdempotent(t *testing.T) {
	st := newTestStore(t)

	if err := st.BlockNode("1.2.3.4:443"); err != nil {
		t.Fatalf("first BlockNode() error = %v", err)
	}
	// 重复屏蔽同一节点不应报错（幂等）
	if err := st.BlockNode("1.2.3.4:443"); err != nil {
		t.Fatalf("duplicate BlockNode() error = %v", err)
	}

	blocked, _ := st.ListBlockedNodes()
	if len(blocked) != 1 {
		t.Errorf("len(blocked) = %d, want 1 (idempotent)", len(blocked))
	}
}

func TestNodeBlocks_Unblock(t *testing.T) {
	st := newTestStore(t)

	st.BlockNode("1.2.3.4:443")
	st.BlockNode("5.6.7.8:8388")

	if err := st.UnblockNode("1.2.3.4:443"); err != nil {
		t.Fatalf("UnblockNode() error = %v", err)
	}

	blocked, _ := st.ListBlockedNodes()
	if len(blocked) != 1 || blocked["1.2.3.4:443"] {
		t.Errorf("after unblock, blocked = %v, want only 5.6.7.8:8388", blocked)
	}
}

func TestNodeBlocks_ListEmpty(t *testing.T) {
	st := newTestStore(t)

	blocked, err := st.ListBlockedNodes()
	if err != nil {
		t.Fatalf("ListBlockedNodes() on empty error = %v", err)
	}
	if len(blocked) != 0 {
		t.Errorf("empty ListBlockedNodes() = %d, want 0", len(blocked))
	}
}

func TestNodeBlocks_UnblockNonexistent(t *testing.T) {
	st := newTestStore(t)
	// 取消不存在的屏蔽不应报错
	if err := st.UnblockNode("9.9.9.9:1"); err != nil {
		t.Errorf("UnblockNode(nonexistent) error = %v, want nil", err)
	}
}

func TestNodeBlocks_BatchBlockAndUnblock(t *testing.T) {
	st := newTestStore(t)

	keys := []string{"1.1.1.1:80", "2.2.2.2:443", "3.3.3.3:8388"}
	if err := st.BlockNodes(keys); err != nil {
		t.Fatalf("BlockNodes() error = %v", err)
	}
	blocked, _ := st.ListBlockedNodes()
	if len(blocked) != 3 {
		t.Fatalf("after BlockNodes len = %d, want 3", len(blocked))
	}

	// 批量取消其中两个
	if err := st.UnblockNodes([]string{"1.1.1.1:80", "3.3.3.3:8388"}); err != nil {
		t.Fatalf("UnblockNodes() error = %v", err)
	}
	blocked, _ = st.ListBlockedNodes()
	if len(blocked) != 1 || !blocked["2.2.2.2:443"] {
		t.Errorf("after batch unblock, blocked = %v, want only 2.2.2.2:443", blocked)
	}
}

func TestNodeBlocks_BatchBlockIdempotentAndEmpty(t *testing.T) {
	st := newTestStore(t)

	// 空列表：no-op，不报错
	if err := st.BlockNodes(nil); err != nil {
		t.Errorf("BlockNodes(nil) error = %v, want nil", err)
	}
	// 含重复与空串：去空、幂等
	if err := st.BlockNodes([]string{"a:1", "a:1", ""}); err != nil {
		t.Fatalf("BlockNodes(dup) error = %v", err)
	}
	blocked, _ := st.ListBlockedNodes()
	if len(blocked) != 1 || !blocked["a:1"] {
		t.Errorf("blocked = %v, want only a:1", blocked)
	}
}
