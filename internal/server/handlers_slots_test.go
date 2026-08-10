package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

func slotReq(t *testing.T, method, target string, body any) (*httptest.ResponseRecorder, *http.Request) {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		reader = bytes.NewReader(data)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, target, reader)
	return httptest.NewRecorder(), req
}

// decodeSlotResp 解码响应体,失败即失败(不让零值静默通过断言)
func decodeSlotResp(t *testing.T, w *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(w.Body.Bytes(), v); err != nil {
		t.Fatalf("decode response: %v (body = %s)", err, w.Body.String())
	}
}

// TestWriteSlotError_UniqueConstraintTranslation 并发竞态兜底:裸 sqlite UNIQUE
// 错误必须翻译成 409 + kind=concurrent(#95 TODO 的落实分支,防 driver/匹配串漂移)
func TestWriteSlotError_UniqueConstraintTranslation(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantKind   string
	}{
		{"pk 冲突", errors.New("UNIQUE constraint failed: name_slots.user_id, name_slots.name"), 409, "concurrent"},
		{"部分唯一索引冲突", errors.New("UNIQUE constraint failed: name_slots.user_id, name_slots.node_key"), 409, "concurrent"},
		{"其它错误", errors.New("disk io error"), 500, ""},
	}
	for _, tc := range cases {
		w := httptest.NewRecorder()
		writeSlotError(srv, w, tc.err)
		if w.Code != tc.wantStatus {
			t.Errorf("%s: status = %d, want %d", tc.name, w.Code, tc.wantStatus)
		}
		if tc.wantKind != "" {
			var resp struct {
				Conflict struct {
					Kind string `json:"kind"`
				} `json:"conflict"`
			}
			decodeSlotResp(t, w, &resp)
			if resp.Conflict.Kind != tc.wantKind {
				t.Errorf("%s: kind = %q, want %q", tc.name, resp.Conflict.Kind, tc.wantKind)
			}
		}
	}
}

func TestSlotsAPI_CreateListDelete(t *testing.T) {
	node := &subscription.Node{
		Name: "机场原名", Server: "a.example.com", Port: 443, Source: "机场A",
		Region: "HK", Available: true, Latency: 88,
	}
	srv, _ := newTestServer(t, []*subscription.Node{node})

	// 预建空槽
	w, req := slotReq(t, http.MethodPost, "/api/slots", map[string]any{"name": "🇭🇰 香港01"})
	srv.handleCreateSlot(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("create empty slot status = %d, body = %s", w.Code, w.Body.String())
	}

	// 名字必填
	w, req = slotReq(t, http.MethodPost, "/api/slots", map[string]any{"name": "  "})
	srv.handleCreateSlot(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("blank name status = %d, want 400", w.Code)
	}

	// 幽灵节点拒收
	w, req = slotReq(t, http.MethodPost, "/api/slots",
		map[string]any{"name": "X", "node_key": "ghost.example.com:1"})
	srv.handleCreateSlot(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("ghost node_key status = %d, want 400", w.Code)
	}

	// 指派池中节点
	w, req = slotReq(t, http.MethodPost, "/api/slots",
		map[string]any{"name": "🇯🇵 日本01", "node_key": node.NodeKey()})
	srv.handleCreateSlot(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("create with node status = %d, body = %s", w.Code, w.Body.String())
	}

	// 列表:空槽标记 + 节点摘要
	w, req = slotReq(t, http.MethodGet, "/api/slots", nil)
	srv.handleListSlots(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list status = %d", w.Code)
	}
	var resp struct {
		Slots []struct {
			Name    string `json:"name"`
			Empty   bool   `json:"empty"`
			NodeKey string `json:"node_key"`
			Node    *struct {
				Name      string `json:"name"`
				Available bool   `json:"available"`
				Latency   int    `json:"latency"`
			} `json:"node"`
		} `json:"slots"`
		Conflicts []any `json:"conflicts"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Slots) != 2 {
		t.Fatalf("slots = %d, want 2", len(resp.Slots))
	}
	byName := map[string]int{}
	for i, s := range resp.Slots {
		byName[s.Name] = i
	}
	if !resp.Slots[byName["🇭🇰 香港01"]].Empty {
		t.Error("🇭🇰 香港01 should be empty slot")
	}
	jp := resp.Slots[byName["🇯🇵 日本01"]]
	if jp.Empty || jp.Node == nil || jp.Node.Name != "机场原名" || !jp.Node.Available || jp.Node.Latency != 88 {
		t.Errorf("🇯🇵 日本01 view = %+v, want bound node summary", jp)
	}

	// 删除(名字含非 ASCII,URL 用占位符,PathValue 注入真实名——生产路径由
	// mux 解码后 PathValue 返回原文,与此同态)
	w, req = slotReq(t, http.MethodDelete, "/api/slots/x", nil)
	req.SetPathValue("name", "🇭🇰 香港01")
	srv.handleDeleteSlot(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete status = %d", w.Code)
	}
	w, req = slotReq(t, http.MethodDelete, "/api/slots/x", nil)
	req.SetPathValue("name", "不存在")
	srv.handleDeleteSlot(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("delete missing status = %d, want 404", w.Code)
	}
}

// TestSlotsAPI_SelfHostedNodeAssignable 自建节点可指派(回归:poolNodeIndex
// 未做 serve-time 自建合并时,自建节点键被误判 unknown node_key)
func TestSlotsAPI_SelfHostedNodeAssignable(t *testing.T) {
	selfNode := &subscription.Node{
		Name: "自建HK", Type: "trojan", Server: "self.example.com", Port: 443,
		Source: subscription.SourceSelfHosted,
	}
	srv, st := newTestServer(t, []*subscription.Node{selfNode})
	// 自建节点落库(serve-time 合并读的是 self_hosted_nodes 表,不是内存池)
	if err := st.CreateSelfHostedNode(&store.SelfHostedNode{
		Name: "自建HK", Protocol: "trojan", Server: "self.example.com", Port: 443, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}

	w, req := slotReq(t, http.MethodPost, "/api/slots",
		map[string]any{"name": "自建主力", "node_key": selfNode.NodeKey()})
	srv.handleCreateSlot(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("assign self-hosted node status = %d, body = %s", w.Code, w.Body.String())
	}
}

// TestSlotsAPI_ProbeGrid 24 小时探测网格:按小时聚合,旧→新 24 格,
// 全通/全断/部分通/无数据四态;响应带 monitor_enabled 开关回显(issue #103)
func TestSlotsAPI_ProbeGrid(t *testing.T) {
	node := &subscription.Node{Name: "A", Server: "a.example.com", Port: 443, Source: "机场A"}
	srv, st := newTestServer(t, []*subscription.Node{node})
	now := time.Now()

	// 当前小时:2 通 1 断(部分通);1 小时前:全断;3 小时前:全通
	put := func(hoursAgo int, ok bool) {
		if err := st.SaveMonitorSample(node.NodeKey(), ok, 50, now.Add(-time.Duration(hoursAgo)*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	put(0, true)
	put(0, true)
	put(0, false)
	put(1, false)
	put(3, true)

	if err := st.CreateNameSlotForUser(0, "格", node.NodeKey(), false); err != nil {
		t.Fatal(err)
	}

	w, req := slotReq(t, http.MethodGet, "/api/slots", nil)
	srv.handleListSlots(w, req)
	var resp struct {
		MonitorEnabled bool `json:"monitor_enabled"`
		Slots          []struct {
			Name      string `json:"name"`
			ProbeGrid []int  `json:"probe_grid"`
		} `json:"slots"`
	}
	decodeSlotResp(t, w, &resp)
	if resp.MonitorEnabled {
		t.Error("monitor_enabled should be false by default")
	}
	if len(resp.Slots) != 1 || len(resp.Slots[0].ProbeGrid) != 24 {
		t.Fatalf("probe_grid = %+v, want 24 cells", resp.Slots)
	}
	grid := resp.Slots[0].ProbeGrid
	if grid[23] != 2 {
		t.Errorf("current hour = %d, want 2 (mixed)", grid[23])
	}
	if grid[22] != 3 {
		t.Errorf("1h ago = %d, want 3 (down)", grid[22])
	}
	if grid[21] != 0 {
		t.Errorf("2h ago = %d, want 0 (no data)", grid[21])
	}
	if grid[20] != 1 {
		t.Errorf("3h ago = %d, want 1 (ok)", grid[20])
	}
}

func TestSlotsAPI_ConflictAndForceTransfer(t *testing.T) {
	nodeA := &subscription.Node{Name: "A", Server: "a.example.com", Port: 443, Source: "机场A"}
	nodeB := &subscription.Node{Name: "B", Server: "b.example.com", Port: 443, Source: "机场A"}
	srv, _ := newTestServer(t, []*subscription.Node{nodeA, nodeB})

	create := func(name, key string, force bool) *httptest.ResponseRecorder {
		t.Helper()
		w, req := slotReq(t, http.MethodPost, "/api/slots",
			map[string]any{"name": name, "node_key": key, "force": force})
		srv.handleCreateSlot(w, req)
		return w
	}
	if w := create("主力", nodeA.NodeKey(), false); w.Code != http.StatusOK {
		t.Fatalf("create 主力: %d %s", w.Code, w.Body.String())
	}

	// 同名冲突 → 409 + kind=name_taken
	if w := create("主力", "", false); w.Code != http.StatusConflict {
		t.Fatalf("dup name status = %d, want 409", w.Code)
	} else {
		var resp struct {
			Conflict struct {
				Kind string `json:"kind"`
			} `json:"conflict"`
		}
		decodeSlotResp(t, w, &resp)
		if resp.Conflict.Kind != "name_taken" {
			t.Errorf("conflict kind = %q, want name_taken", resp.Conflict.Kind)
		}
	}

	// 节点被占 → 409 + holder_name
	w := create("备用", nodeA.NodeKey(), false)
	if w.Code != http.StatusConflict {
		t.Fatalf("occupied node status = %d, want 409", w.Code)
	}
	var resp struct {
		Conflict struct {
			Kind       string `json:"kind"`
			HolderName string `json:"holder_name"`
		} `json:"conflict"`
	}
	decodeSlotResp(t, w, &resp)
	if resp.Conflict.Kind != "node_occupied" || resp.Conflict.HolderName != "主力" {
		t.Errorf("conflict = %+v, want node_occupied by 主力", resp.Conflict)
	}

	// 转移:把"主力"改指 nodeB,不 force → 409 reassign(带当前挂载节点)
	update := func(name string, body map[string]any) *httptest.ResponseRecorder {
		t.Helper()
		w, req := slotReq(t, http.MethodPut, "/api/slots/x", body)
		req.SetPathValue("name", name)
		srv.handleUpdateSlot(w, req)
		return w
	}
	w = update("主力", map[string]any{"node_key": nodeB.NodeKey()})
	if w.Code != http.StatusConflict {
		t.Fatalf("reassign status = %d, want 409", w.Code)
	}
	var reassignResp struct {
		Conflict struct {
			Kind          string `json:"kind"`
			HolderNodeKey string `json:"holder_node_key"`
		} `json:"conflict"`
	}
	decodeSlotResp(t, w, &reassignResp)
	if reassignResp.Conflict.Kind != "reassign" || reassignResp.Conflict.HolderNodeKey != nodeA.NodeKey() {
		t.Errorf("reassign conflict = %+v, want reassign holding %s", reassignResp.Conflict, nodeA.NodeKey())
	}

	// force 转移成功
	if w := update("主力", map[string]any{"node_key": nodeB.NodeKey(), "force": true}); w.Code != http.StatusOK {
		t.Fatalf("force reassign: %d %s", w.Code, w.Body.String())
	}

	// 改名
	if w := update("主力", map[string]any{"new_name": "主力v2"}); w.Code != http.StatusOK {
		t.Fatalf("rename: %d %s", w.Code, w.Body.String())
	}
	// 摘下变空槽(node_key 显式空串)
	if w := update("主力v2", map[string]any{"node_key": ""}); w.Code != http.StatusOK {
		t.Fatalf("unassign: %d %s", w.Code, w.Body.String())
	}
	w, req := slotReq(t, http.MethodGet, "/api/slots", nil)
	srv.handleListSlots(w, req)
	var listResp struct {
		Slots []struct {
			Name  string `json:"name"`
			Empty bool   `json:"empty"`
		} `json:"slots"`
	}
	decodeSlotResp(t, w, &listResp)
	if len(listResp.Slots) != 1 || listResp.Slots[0].Name != "主力v2" || !listResp.Slots[0].Empty {
		t.Errorf("after rename+unassign slots = %+v, want single empty 主力v2", listResp.Slots)
	}
}
