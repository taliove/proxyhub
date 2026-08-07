package store

import (
	"errors"
	"testing"
)

// 订阅地址精选 node_picks 的 store 缝(spec #70 / issue #79):
// endpoints 表新增 JSON 文本列(NodeKey 数组,空=未配置),与 conditions 列同构。

// TestEndpointNodePicksColumnMigrated 证明 025 迁移接入迁移链路并执行:
// endpoints 表在裸库 Open 后即含 node_picks 列(ADD COLUMN,幂等标记为列存在性)。
func TestEndpointNodePicksColumnMigrated(t *testing.T) {
	st := newTestStore(t)
	cols := map[string]bool{}
	rows, err := st.db.Query(`SELECT name FROM pragma_table_info('endpoints')`)
	if err != nil {
		t.Fatalf("inspect endpoints columns: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan column: %v", err)
		}
		cols[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows err: %v", err)
	}
	if !cols["node_picks"] {
		t.Error("endpoints missing column \"node_picks\" after migration")
	}
}

// TestNewEndpointDefaultNodePicksEmpty 新建端点 node_picks 默认空(未配置=全量,零回归)。
func TestNewEndpointDefaultNodePicksEmpty(t *testing.T) {
	st := newTestStore(t)
	ep, err := st.CreateEndpoint("测试")
	if err != nil {
		t.Fatalf("CreateEndpoint: %v", err)
	}
	if ep.NodePicks != "" {
		t.Errorf("default NodePicks = %q, want empty", ep.NodePicks)
	}
}

// TestUpdateEndpointNodePicks_Roundtrip 落库后 GetByID/GetByPath/List 都读回同一 JSON。
func TestUpdateEndpointNodePicks_Roundtrip(t *testing.T) {
	st := newTestStore(t)
	ep, _ := st.CreateEndpoint("测试")

	raw := `["a.example.com:8388","b.example.com:443"]`
	if err := st.UpdateEndpointNodePicks(ep.ID, raw); err != nil {
		t.Fatalf("UpdateEndpointNodePicks: %v", err)
	}

	byID, err := st.GetEndpointByID(ep.ID)
	if err != nil {
		t.Fatalf("GetEndpointByID: %v", err)
	}
	if byID.NodePicks != raw {
		t.Errorf("GetByID NodePicks = %q, want %q", byID.NodePicks, raw)
	}

	byPath, err := st.GetEndpointByPath(ep.Path)
	if err != nil {
		t.Fatalf("GetEndpointByPath: %v", err)
	}
	if byPath.NodePicks != raw {
		t.Errorf("GetByPath NodePicks = %q, want %q", byPath.NodePicks, raw)
	}

	list, _ := st.ListEndpoints()
	if len(list) != 1 || list[0].NodePicks != raw {
		t.Errorf("List NodePicks = %q, want %q", list[0].NodePicks, raw)
	}
}

// TestUpdateEndpointNodePicks_EmptyClears 传空串清空精选(回到全量,零回归)。
func TestUpdateEndpointNodePicks_EmptyClears(t *testing.T) {
	st := newTestStore(t)
	ep, _ := st.CreateEndpoint("测试")
	_ = st.UpdateEndpointNodePicks(ep.ID, `["a.example.com:8388"]`)
	if err := st.UpdateEndpointNodePicks(ep.ID, ""); err != nil {
		t.Fatalf("clear node picks: %v", err)
	}
	got, _ := st.GetEndpointByID(ep.ID)
	if got.NodePicks != "" {
		t.Errorf("NodePicks = %q, want empty", got.NodePicks)
	}
}

// TestUpdateEndpointNodePicks_InvalidJSON 非法 JSON / 非字符串数组拒绝落库(边界校验)。
func TestUpdateEndpointNodePicks_InvalidJSON(t *testing.T) {
	st := newTestStore(t)
	ep, _ := st.CreateEndpoint("测试")
	for _, bad := range []string{"{not json", `{"a":1}`, `["a",1]`, `"just-a-string"`} {
		if err := st.UpdateEndpointNodePicks(ep.ID, bad); err == nil {
			t.Errorf("UpdateEndpointNodePicks(%q) expected error, got nil", bad)
		}
	}
}

// TestUpdateEndpointNodePicks_NotFound 不存在的端点返回 ErrNotFound。
func TestUpdateEndpointNodePicks_NotFound(t *testing.T) {
	st := newTestStore(t)
	err := st.UpdateEndpointNodePicks(999, `["a.example.com:8388"]`)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

// TestUpdateEndpointNodePicksForUser_OwnerIsolation 按属主写入(多租户,issue #79):
// 行属他人时 ErrNotFound(不暴露存在性);userID=0 为全局逃生舱,跳过属主校验。
func TestUpdateEndpointNodePicksForUser_OwnerIsolation(t *testing.T) {
	st := newTestStore(t)
	raw := `["a.example.com:8388"]`

	alice, err := st.CreateUser("alice", "hash", RoleUser, false)
	if err != nil {
		t.Fatalf("CreateUser alice: %v", err)
	}
	bob, err := st.CreateUser("bob", "hash", RoleUser, false)
	if err != nil {
		t.Fatalf("CreateUser bob: %v", err)
	}
	ep, err := st.CreateEndpointForUser(alice.ID, "alice-ep")
	if err != nil {
		t.Fatalf("CreateEndpointForUser: %v", err)
	}

	// bob 写 alice 的端点 -> ErrNotFound,且数据不变。
	if err := st.UpdateEndpointNodePicksForUser(bob.ID, ep.ID, raw); !errors.Is(err, ErrNotFound) {
		t.Errorf("bob write error = %v, want ErrNotFound", err)
	}
	got, _ := st.GetEndpointByID(ep.ID)
	if got.NodePicks != "" {
		t.Errorf("NodePicks after foreign write = %q, want empty", got.NodePicks)
	}

	// 属主本人写入成功。
	if err := st.UpdateEndpointNodePicksForUser(alice.ID, ep.ID, raw); err != nil {
		t.Fatalf("alice write: %v", err)
	}
	got, _ = st.GetEndpointByIDForUser(alice.ID, ep.ID)
	if got.NodePicks != raw {
		t.Errorf("NodePicks = %q, want %q", got.NodePicks, raw)
	}

	// userID=0 逃生舱:跳过属主校验。
	if err := st.UpdateEndpointNodePicksForUser(0, ep.ID, ""); err != nil {
		t.Fatalf("global write: %v", err)
	}
}
