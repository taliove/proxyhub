package store

import "testing"

func sampleSelfNode() *SelfHostedNode {
	return &SelfHostedNode{
		Name: "自建香港", Protocol: "ss", Server: "1.1.1.1", Port: 8388,
		Cipher: "aes-256-gcm", Password: "pw", Enabled: true,
	}
}

func TestSelfHostedNode_UpdateChangesFields(t *testing.T) {
	st := newTestStore(t)
	n := sampleSelfNode()
	if err := st.CreateSelfHostedNode(n); err != nil {
		t.Fatalf("CreateSelfHostedNode() error = %v", err)
	}
	// 取回 ID
	all, _ := st.ListAllSelfHostedNodes()
	if len(all) != 1 {
		t.Fatalf("len = %d, want 1", len(all))
	}
	id := all[0].ID

	updated := &SelfHostedNode{
		ID: id, Name: "自建日本", Protocol: "trojan", Server: "2.2.2.2", Port: 443,
		Password: "newpw", TLS: true, Enabled: true,
	}
	if err := st.UpdateSelfHostedNode(updated); err != nil {
		t.Fatalf("UpdateSelfHostedNode() error = %v", err)
	}

	all, _ = st.ListAllSelfHostedNodes()
	got := all[0]
	if got.Name != "自建日本" || got.Protocol != "trojan" || got.Server != "2.2.2.2" ||
		got.Port != 443 || got.Password != "newpw" || !got.TLS {
		t.Errorf("update did not apply: %+v", got)
	}
}

func TestSelfHostedNode_UpdateNonexistent(t *testing.T) {
	st := newTestStore(t)
	err := st.UpdateSelfHostedNode(&SelfHostedNode{ID: 999, Name: "x", Protocol: "ss", Server: "a", Port: 1})
	if err == nil {
		t.Error("UpdateSelfHostedNode(nonexistent) expected error, got nil")
	}
}

func TestSelfHostedNode_ToggleEnabled(t *testing.T) {
	st := newTestStore(t)
	n := sampleSelfNode()
	st.CreateSelfHostedNode(n)
	all, _ := st.ListAllSelfHostedNodes()
	id := all[0].ID

	// 禁用
	if err := st.SetSelfHostedNodeEnabled(id, false); err != nil {
		t.Fatalf("SetSelfHostedNodeEnabled(false) error = %v", err)
	}
	// ListSelfHostedNodes 只返回启用的 → 应为空
	enabled, _ := st.ListSelfHostedNodes()
	if len(enabled) != 0 {
		t.Errorf("disabled node still in ListSelfHostedNodes: %d", len(enabled))
	}
	// ListAll 仍能看到（禁用状态）
	all, _ = st.ListAllSelfHostedNodes()
	if len(all) != 1 || all[0].Enabled {
		t.Errorf("ListAll should show disabled node, got %+v", all)
	}

	// 重新启用
	if err := st.SetSelfHostedNodeEnabled(id, true); err != nil {
		t.Fatalf("SetSelfHostedNodeEnabled(true) error = %v", err)
	}
	enabled, _ = st.ListSelfHostedNodes()
	if len(enabled) != 1 {
		t.Errorf("re-enabled node missing from ListSelfHostedNodes: %d", len(enabled))
	}
}

func TestSelfHostedNode_ListAllIncludesDisabled(t *testing.T) {
	st := newTestStore(t)
	a := sampleSelfNode()
	a.Name = "启用的"
	b := sampleSelfNode()
	b.Name = "禁用的"
	b.Port = a.Port + 1 // 身份唯一约束(023):夹具须用不同身份,不能靠重复行凑数
	b.Enabled = false
	st.CreateSelfHostedNode(a)
	st.CreateSelfHostedNode(b)

	all, err := st.ListAllSelfHostedNodes()
	if err != nil {
		t.Fatalf("ListAllSelfHostedNodes() error = %v", err)
	}
	if len(all) != 2 {
		t.Errorf("ListAll = %d, want 2 (incl disabled)", len(all))
	}
	enabled, _ := st.ListSelfHostedNodes()
	if len(enabled) != 1 {
		t.Errorf("ListSelfHostedNodes = %d, want 1 (enabled only)", len(enabled))
	}
}
