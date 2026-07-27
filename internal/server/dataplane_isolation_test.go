package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

// scopedRequest 构造携带指定用户 scope 的测试请求(绕过 requireAuth 的直调路径)。
func scopedRequest(method, path string, scope UserScope) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	return req.WithContext(ContextWithUserScope(req.Context(), scope))
}

// TestDataPlane_ReadIsolation 数据面读接口按用户隔离:
// 普通用户只见自己名下的任务/刷新历史/统计;超管未 impersonate 看全量;
// 跨用户的详情读取一律 404,不暴露存在性。
func TestDataPlane_ReadIsolation(t *testing.T) {
	srv, st := newTestServer(t, nil)
	admin := UserScope{UserID: 1, Role: store.RoleSuperAdmin}
	member := UserScope{UserID: 2, Role: store.RoleUser}

	// 种子:admin 一条 running 全量刷新任务;member 一条不同 key 的任务
	// (避免与取消用例同 kind+key——取消闸门按"自己名下同 kind+key running"判定)。
	if _, err := st.Jobs().InsertForUser(1, "refresh", "all", nil); err != nil {
		t.Fatalf("insert admin job: %v", err)
	}
	memberJobID, err := st.Jobs().InsertForUser(2, "refresh", "airport-9", nil)
	if err != nil {
		t.Fatalf("insert member job: %v", err)
	}
	adminRun, err := st.CreateRefreshRunForUser(1, "manual", 0)
	if err != nil {
		t.Fatalf("create admin run: %v", err)
	}
	memberRun, err := st.CreateRefreshRunForUser(2, "manual", 0)
	if err != nil {
		t.Fatalf("create member run: %v", err)
	}

	t.Run("jobs list scoped", func(t *testing.T) {
		rec := httptest.NewRecorder()
		srv.handleListJobs(rec, scopedRequest(http.MethodGet, "/api/jobs", member))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		var list []JobInfo
		if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(list) != 1 || list[0].ID != memberJobID {
			t.Errorf("member sees %+v, want only own job %d", list, memberJobID)
		}

		rec = httptest.NewRecorder()
		srv.handleListJobs(rec, scopedRequest(http.MethodGet, "/api/jobs", admin))
		var all []JobInfo
		if err := json.Unmarshal(rec.Body.Bytes(), &all); err != nil {
			t.Fatalf("decode admin: %v", err)
		}
		if len(all) != 2 {
			t.Errorf("admin sees %d jobs, want 2 (global view)", len(all))
		}
	})

	t.Run("job detail cross-user 404", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := scopedRequest(http.MethodGet, "/api/jobs/1", member)
		req.SetPathValue("id", "1")
		srv.handleGetJobDetail(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404", rec.Code)
		}
	})

	t.Run("job result cross-user 404", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := scopedRequest(http.MethodGet, "/api/jobs/1/result", member)
		req.SetPathValue("id", "1")
		srv.handleGetJobResult(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404", rec.Code)
		}
	})

	t.Run("cancel cross-user conflict", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := scopedRequest(http.MethodPost, "/api/jobs/refresh/all/cancel", member)
		req.SetPathValue("kind", "refresh")
		req.SetPathValue("key", "all")
		srv.handleCancelJob(rec, req)
		if rec.Code != http.StatusConflict {
			t.Errorf("status = %d, want 409 (admin's running job invisible to member)", rec.Code)
		}
	})

	t.Run("refresh runs scoped", func(t *testing.T) {
		rec := httptest.NewRecorder()
		srv.handleListRefreshRuns(rec, scopedRequest(http.MethodGet, "/api/refresh/runs", member))
		var runs []*store.RefreshRun
		if err := json.Unmarshal(rec.Body.Bytes(), &runs); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(runs) != 1 || runs[0].ID != memberRun.ID {
			t.Errorf("member sees %+v, want only own run %d", runs, memberRun.ID)
		}

		// 跨用户读详情 404
		rec = httptest.NewRecorder()
		req := scopedRequest(http.MethodGet, "/api/refresh/runs/x", member)
		req.SetPathValue("id", strconv.FormatInt(adminRun.ID, 10))
		srv.handleGetRefreshRun(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("cross-user run detail status = %d, want 404", rec.Code)
		}

		// 超管全量可见
		rec = httptest.NewRecorder()
		srv.handleListRefreshRuns(rec, scopedRequest(http.MethodGet, "/api/refresh/runs", admin))
		var all []*store.RefreshRun
		if err := json.Unmarshal(rec.Body.Bytes(), &all); err != nil {
			t.Fatalf("decode admin: %v", err)
		}
		if len(all) != 2 {
			t.Errorf("admin sees %d runs, want 2 (global view)", len(all))
		}
	})
}

// TestManualRefresh_UsesCallerShard 手动刷新按请求者分片发起(ticket 07):
// 普通用户触发只聚合自己名下机场,不再走全局 StartRefreshJob。
func TestManualRefresh_UsesCallerShard(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	fn := srv.nodes.(*fakeNodes)

	rec := httptest.NewRecorder()
	srv.handleManualRefresh(rec, scopedRequest(http.MethodPost, "/api/aggregator/refresh",
		UserScope{UserID: 2, Role: store.RoleUser}))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if fn.lastRefreshUserID != 2 {
		t.Errorf("refresh started for user %d, want 2", fn.lastRefreshUserID)
	}
}

// TestDashboardTopNodes_ScopedToUserPool 优质节点只聚合请求者池内节点:
// 他用户池里的高分节点不出现在普通用户的榜单里。
func TestDashboardTopNodes_ScopedToUserPool(t *testing.T) {
	adminNode := &subscription.Node{
		Name: "admin-node", Server: "a.example.com", Port: 443, Type: "vmess",
		UUID: "00000000-0000-0000-0000-000000000000", Source: "airport", UserID: 1,
	}
	memberNode := &subscription.Node{
		Name: "member-node", Server: "b.example.com", Port: 443, Type: "vmess",
		UUID: "00000000-0000-0000-0000-000000000000", Source: "airport", UserID: 2,
	}
	srv, st := newTestServer(t, []*subscription.Node{adminNode, memberNode})

	// 两个节点都有完整体检记录(报告共享按 node_key,可见性由池过滤决定)
	seedFullExam(t, st, adminNode.NodeKey(), 95)
	seedFullExam(t, st, memberNode.NodeKey(), 60)

	rec := httptest.NewRecorder()
	srv.handleDashboardTopNodes(rec, scopedRequest(http.MethodGet, "/api/dashboard/top-nodes",
		UserScope{UserID: 2, Role: store.RoleUser}))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var views []topNodeView
	if err := json.Unmarshal(rec.Body.Bytes(), &views); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(views) != 1 || views[0].NodeKey != memberNode.NodeKey() {
		t.Errorf("member sees %+v, want only own pool node %s", views, memberNode.NodeKey())
	}
}
