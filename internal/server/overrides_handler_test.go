package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

// TestNodeOverride_SetAndClear 验证覆盖层设置与清除
func TestNodeOverride_SetAndClear(t *testing.T) {
	srv, st := newTestServer(t, nil)

	// 设置覆盖
	body, _ := json.Marshal(map[string]any{
		"node_key":     "1.1.1.1:8388",
		"display_name": "自定义名",
		"region":       "HK",
	})
	req := httptest.NewRequest(http.MethodPut, "/api/nodes/override", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleSetNodeOverride(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("set override status = %d, body = %s", w.Code, w.Body.String())
	}

	// 验证已写入
	overrides, err := st.ListNodeOverrides()
	if err != nil {
		t.Fatalf("ListNodeOverrides error = %v", err)
	}
	o, ok := overrides["1.1.1.1:8388"]
	if !ok {
		t.Fatal("override not found after set")
	}
	if o.DisplayName != "自定义名" || o.Region != "HK" {
		t.Errorf("override = %+v, want 自定义名/HK", o)
	}

	// 清除覆盖
	clearBody, _ := json.Marshal(map[string]any{"node_key": "1.1.1.1:8388"})
	clearReq := httptest.NewRequest(http.MethodDelete, "/api/nodes/override", bytes.NewReader(clearBody))
	clearW := httptest.NewRecorder()
	srv.handleClearNodeOverride(clearW, clearReq)
	if clearW.Code != http.StatusOK {
		t.Fatalf("clear override status = %d", clearW.Code)
	}

	// 验证已删除
	overrides, _ = st.ListNodeOverrides()
	if _, ok := overrides["1.1.1.1:8388"]; ok {
		t.Error("override should be removed after clear")
	}
}

// TestNodeOverride_MissingKey 验证缺失 node_key 返回 400
func TestNodeOverride_MissingKey(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	body, _ := json.Marshal(map[string]any{"display_name": "x"})
	req := httptest.NewRequest(http.MethodPut, "/api/nodes/override", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleSetNodeOverride(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// TestCleanupNodes_MixedList 验证清理混合列表：机场屏蔽、自建禁用
func TestCleanupNodes_MixedList(t *testing.T) {
	airportNode := &subscription.Node{
		Name: "机场节点", Type: "ss", Server: "1.1.1.1", Port: 8388, Source: "机场A",
	}
	selfNode := &subscription.Node{
		Name: "自建节点", Type: "trojan", Server: "2.2.2.2", Port: 443,
		Source: subscription.SourceSelfHosted,
	}
	srv, st := newTestServer(t, []*subscription.Node{airportNode, selfNode})

	// 创建对应的自建节点记录（cleanup 按 node_key 反查 self_node id）
	if err := st.CreateSelfHostedNode(&store.SelfHostedNode{
		Name: "自建节点", Protocol: "trojan", Server: "2.2.2.2", Port: 443, Enabled: true,
	}); err != nil {
		t.Fatalf("create self node: %v", err)
	}

	// 清理：机场屏蔽 + 自建禁用
	airportBody, _ := json.Marshal(map[string]any{
		"node_keys": []string{airportNode.NodeKey()},
		"action":    "block",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/nodes/cleanup", bytes.NewReader(airportBody))
	w := httptest.NewRecorder()
	srv.handleCleanupNodes(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("cleanup airport status = %d, body = %s", w.Code, w.Body.String())
	}
	var res map[string]any
	json.Unmarshal(w.Body.Bytes(), &res)
	if res["blocked"].(float64) != 1 {
		t.Errorf("blocked = %v, want 1", res["blocked"])
	}

	// 验证机场节点已进屏蔽名单
	blocked, _ := st.ListBlockedNodes()
	if !blocked[airportNode.NodeKey()] {
		t.Error("airport node should be blocked")
	}

	// 清理自建节点（禁用）
	selfBody, _ := json.Marshal(map[string]any{
		"node_keys": []string{selfNode.NodeKey()},
		"action":    "disable",
	})
	selfReq := httptest.NewRequest(http.MethodPost, "/api/nodes/cleanup", bytes.NewReader(selfBody))
	selfW := httptest.NewRecorder()
	srv.handleCleanupNodes(selfW, selfReq)
	if selfW.Code != http.StatusOK {
		t.Fatalf("cleanup self status = %d", selfW.Code)
	}
	json.Unmarshal(selfW.Body.Bytes(), &res)
	if res["disabled"].(float64) != 1 {
		t.Errorf("disabled = %v, want 1", res["disabled"])
	}

	// 验证自建节点已禁用（不再出现在启用列表）
	enabled, _ := st.ListSelfHostedNodes()
	for _, n := range enabled {
		if n.Server == "2.2.2.2" {
			t.Error("self node should be disabled (not in enabled list)")
		}
	}
}
