package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/taliove/proxyhub/internal/jobs"
)

// seedJobRecords 写入一组覆盖多种 kind/status 的任务记录,返回按插入顺序的 id 列表。
// 布局(下标即 id 顺序):0=refresh/failed 1=refresh/interrupted 2=batch_exam/done
// 3=exam/cancelled 4=refresh/running(Insert 后不落终态)。
func seedJobRecords(t *testing.T, srv *Server) []int64 {
	t.Helper()
	type seed struct {
		kind   string
		key    string
		status jobs.Status // 空串表示保持 running
	}
	seeds := []seed{
		{"refresh", "all", jobs.StatusFailed},
		{"refresh", "one", jobs.StatusInterrupted},
		{"batch_exam", "k1", jobs.StatusDone},
		{"exam", "n1", jobs.StatusCancelled},
		{"refresh", "r2", ""},
	}
	ids := make([]int64, 0, len(seeds))
	for _, sd := range seeds {
		id, err := srv.st.Jobs().Insert(sd.kind, sd.key, nil)
		if err != nil {
			t.Fatalf("Insert(%s/%s) error = %v", sd.kind, sd.key, err)
		}
		if sd.status != "" {
			if err := srv.st.Jobs().Finish(id, sd.status); err != nil {
				t.Fatalf("Finish(%d, %s) error = %v", id, sd.status, err)
			}
		}
		ids = append(ids, id)
	}
	return ids
}

// TestHandleListJobs_StatusFilter 覆盖 /api/jobs status 过滤契约:
// 单值行为逐字节不变;逗号多值 ANY 匹配;未知/空段忽略;全部段无效时返回空列表
// 而非未过滤;kind 过滤保持单值且可与 status 组合。
func TestHandleListJobs_StatusFilter(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	ids := seedJobRecords(t, srv)

	cases := []struct {
		name  string
		query string
		want  []int64 // 期望命中的记录 id(无序)
	}{
		{"无 status 参数返回全部", "", ids},
		{"单值等值行为不变", "status=failed", []int64{ids[0]}},
		{"逗号多值 ANY 匹配", "status=failed,interrupted", []int64{ids[0], ids[1]}},
		{"未知单值返回空列表", "status=bogus", nil},
		{"未知段与有效段混合", "status=bogus,failed", []int64{ids[0]}},
		{"尾逗号与重复段容忍", "status=failed,,failed", []int64{ids[0]}},
		{"全部空段返回空列表", "status=,,", nil},
		{"kind 过滤保持单值", "kind=refresh", []int64{ids[0], ids[1], ids[4]}},
		{"kind 与多值 status 组合", "kind=refresh&status=failed,interrupted", []int64{ids[0], ids[1]}},
		{"kind 与未知 status 组合返回空", "kind=refresh&status=bogus", nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			url := "/api/jobs"
			if tc.query != "" {
				url += "?" + tc.query
			}
			req := httptest.NewRequest(http.MethodGet, url, nil)
			w := httptest.NewRecorder()
			srv.handleListJobs(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", w.Code)
			}

			var got []JobInfo
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode response error = %v, body = %s", err, w.Body.String())
			}
			if len(tc.want) == 0 {
				// 空结果必须序列化为 [] 而非 null(前端按数组消费)
				if got == nil {
					t.Fatalf("empty result decoded as null, body = %s", w.Body.String())
				}
				return
			}

			gotIDs := make(map[int64]bool, len(got))
			for _, j := range got {
				gotIDs[j.ID] = true
			}
			if len(gotIDs) != len(tc.want) {
				t.Fatalf("got %d jobs %v, want %d %v", len(gotIDs), fmt.Sprint(gotIDs), len(tc.want), tc.want)
			}
			for _, id := range tc.want {
				if !gotIDs[id] {
					t.Fatalf("missing job id %d in %v", id, fmt.Sprint(gotIDs))
				}
			}
		})
	}
}
