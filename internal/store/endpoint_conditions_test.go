package store

import (
	"errors"
	"testing"
)

// TestEndpointConditionsColumnMigrated 证明 013 迁移接入迁移链路并执行:
// endpoints 表在裸库 Open 后即含 conditions 列(ADD COLUMN,幂等标记为列存在性)。
func TestEndpointConditionsColumnMigrated(t *testing.T) {
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
	if !cols["conditions"] {
		t.Error("endpoints missing column \"conditions\" after migration")
	}
}

// TestNewEndpointDefaultConditionsEmpty 新建端点 conditions 默认空(旧行为=全量)。
func TestNewEndpointDefaultConditionsEmpty(t *testing.T) {
	st := newTestStore(t)
	ep, err := st.CreateEndpoint("测试")
	if err != nil {
		t.Fatalf("CreateEndpoint: %v", err)
	}
	if ep.Conditions != "" {
		t.Errorf("default Conditions = %q, want empty", ep.Conditions)
	}
}

// TestUpdateEndpointConditions_Roundtrip 落库后 GetByID/GetByPath/List 都读回同一 JSON。
func TestUpdateEndpointConditions_Roundtrip(t *testing.T) {
	st := newTestStore(t)
	ep, _ := st.CreateEndpoint("测试")

	raw := `{"regions":["HK"],"tags":["nf-full"]}`
	if err := st.UpdateEndpointConditions(ep.ID, raw); err != nil {
		t.Fatalf("UpdateEndpointConditions: %v", err)
	}

	byID, err := st.GetEndpointByID(ep.ID)
	if err != nil {
		t.Fatalf("GetEndpointByID: %v", err)
	}
	if byID.Conditions != raw {
		t.Errorf("GetByID Conditions = %q, want %q", byID.Conditions, raw)
	}

	byPath, err := st.GetEndpointByPath(ep.Path)
	if err != nil {
		t.Fatalf("GetEndpointByPath: %v", err)
	}
	if byPath.Conditions != raw {
		t.Errorf("GetByPath Conditions = %q, want %q", byPath.Conditions, raw)
	}

	list, _ := st.ListEndpoints()
	if len(list) != 1 || list[0].Conditions != raw {
		t.Errorf("List Conditions = %q, want %q", list[0].Conditions, raw)
	}
}

// TestUpdateEndpointConditions_EmptyClears 传空串清空条件(回到全量)。
func TestUpdateEndpointConditions_EmptyClears(t *testing.T) {
	st := newTestStore(t)
	ep, _ := st.CreateEndpoint("测试")
	_ = st.UpdateEndpointConditions(ep.ID, `{"regions":["HK"]}`)
	if err := st.UpdateEndpointConditions(ep.ID, ""); err != nil {
		t.Fatalf("clear conditions: %v", err)
	}
	got, _ := st.GetEndpointByID(ep.ID)
	if got.Conditions != "" {
		t.Errorf("Conditions = %q, want empty", got.Conditions)
	}
}

// TestUpdateEndpointConditions_InvalidJSON 非法 JSON 拒绝落库(边界校验)。
func TestUpdateEndpointConditions_InvalidJSON(t *testing.T) {
	st := newTestStore(t)
	ep, _ := st.CreateEndpoint("测试")
	if err := st.UpdateEndpointConditions(ep.ID, "{not json"); err == nil {
		t.Error("UpdateEndpointConditions(invalid) expected error, got nil")
	}
}

// TestUpdateEndpointConditions_NotFound 不存在的端点返回 ErrNotFound。
func TestUpdateEndpointConditions_NotFound(t *testing.T) {
	st := newTestStore(t)
	err := st.UpdateEndpointConditions(999, `{}`)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}
