package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/taliove/proxyhub/internal/aggregator"
	"github.com/taliove/proxyhub/internal/store"
)

// authedCookie 完成初始化+登录，返回会话 Cookie
func authedCookie(t *testing.T, h http.Handler) *http.Cookie {
	t.Helper()
	doSetup(t, h, "realuser", "password123456")
	markAllMFAEnrolled(t, testStore(t))
	w := doLogin(t, h, "realuser", "password123456", "10.0.0.2:1000")
	cookies := w.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatalf("login did not set session cookie (status %d, body %s)", w.Code, w.Body.String())
	}
	return cookies[0]
}

func authedRequest(t *testing.T, h http.Handler, method, path string, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.RemoteAddr = "10.0.0.2:1000"
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestManualRefresh_ReturnsRunID(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	h := srv.Handler()
	cookie := authedCookie(t, h)

	w := authedRequest(t, h, "POST", "/api/aggregator/refresh", cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}

	var resp struct {
		OK      bool   `json:"ok"`
		JobID   int64  `json:"jobId"`
		Kind    string `json:"kind"`
		Key     string `json:"key"`
		Started bool   `json:"started"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !resp.OK || resp.JobID != 42 || resp.Kind != "refresh" || resp.Key != "all" || !resp.Started {
		t.Errorf("resp = %+v, want ok=true jobId=42 kind=refresh key=all started=true", resp)
	}
	if got := srv.nodes.(*fakeNodes).lastTrigger; got != store.RefreshTriggerManual {
		t.Errorf("trigger = %s, want manual", got)
	}
}

func TestManualRefresh_InternalError(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	srv.nodes.(*fakeNodes).refreshErr = errors.New("boom")
	h := srv.Handler()
	cookie := authedCookie(t, h)

	w := authedRequest(t, h, "POST", "/api/aggregator/refresh", cookie)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 (body %s)", w.Code, w.Body.String())
	}
}

func TestManualRefresh_ConflictWhenInProgress(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	srv.nodes.(*fakeNodes).refreshErr = aggregator.ErrRefreshConflict
	h := srv.Handler()
	cookie := authedCookie(t, h)

	w := authedRequest(t, h, "POST", "/api/aggregator/refresh", cookie)
	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409 (body %s)", w.Code, w.Body.String())
	}
}

func TestRefreshRuns_ListAndDetail(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	cookie := authedCookie(t, h)

	first, _ := st.CreateRefreshRun(store.RefreshTriggerStartup, 0)
	second, _ := st.CreateRefreshRun(store.RefreshTriggerManual, 0)
	st.AppendRefreshEvent(second.ID, "info", "fetch", "拉取「机场A」…", "")
	st.AppendRefreshEvent(second.ID, "warn", "fetch", "「机场A」拉取失败", `{"airport":"机场A"}`)
	st.FinishRefreshRun(second.ID, store.RefreshStatusFailed, 0, 0, 0, "1/1 机场拉取失败")
	st.InsertRefreshFetchDiag(&store.RefreshFetchDiag{
		RunID: second.ID, Airport: "机场A", AirportID: 1,
		HTTPStatus: 200, DurationMs: 123, NodeCount: 10, ParseFailures: 2,
	})

	// 列表：最新在前
	w := authedRequest(t, h, "GET", "/api/refresh/runs", cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("list status = %d (body %s)", w.Code, w.Body.String())
	}
	var runs []store.RefreshRun
	if err := json.Unmarshal(w.Body.Bytes(), &runs); err != nil {
		t.Fatalf("unmarshal list: %v", err)
	}
	if len(runs) != 2 || runs[0].ID != second.ID || runs[1].ID != first.ID {
		t.Errorf("runs = %+v, want newest first", runs)
	}

	// 详情：run + events + diags
	w = authedRequest(t, h, "GET", "/api/refresh/runs/"+itoa(second.ID), cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("detail status = %d (body %s)", w.Code, w.Body.String())
	}
	var detail struct {
		Run    store.RefreshRun         `json:"run"`
		Events []store.RefreshEvent     `json:"events"`
		Diags  []store.RefreshFetchDiag `json:"diags"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &detail); err != nil {
		t.Fatalf("unmarshal detail: %v", err)
	}
	if detail.Run.ID != second.ID || detail.Run.Status != store.RefreshStatusFailed {
		t.Errorf("detail.Run = %+v", detail.Run)
	}
	if len(detail.Events) != 2 || detail.Events[1].Level != "warn" {
		t.Errorf("detail.Events = %+v", detail.Events)
	}
	if len(detail.Diags) != 1 {
		t.Fatalf("detail.Diags = %+v, want 1 row", detail.Diags)
	}
	d := detail.Diags[0]
	if d.Airport != "机场A" || d.HTTPStatus != 200 || d.DurationMs != 123 || d.NodeCount != 10 || d.ParseFailures != 2 {
		t.Errorf("detail.Diags[0] = %+v", d)
	}

	// 无诊断的 run:diags 为空数组而非 null
	w = authedRequest(t, h, "GET", "/api/refresh/runs/"+itoa(first.ID), cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("first detail status = %d (body %s)", w.Code, w.Body.String())
	}
	var firstDetail struct {
		Diags []store.RefreshFetchDiag `json:"diags"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &firstDetail); err != nil {
		t.Fatalf("unmarshal first detail: %v", err)
	}
	if firstDetail.Diags == nil || len(firstDetail.Diags) != 0 {
		t.Errorf("first diags = %+v, want empty array", firstDetail.Diags)
	}

	// 不存在的 run → 404
	w = authedRequest(t, h, "GET", "/api/refresh/runs/99999", cookie)
	if w.Code != http.StatusNotFound {
		t.Errorf("missing run status = %d, want 404", w.Code)
	}
}

func TestRefreshRuns_RequireAuth(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	h := srv.Handler()

	for _, tc := range []struct{ method, path string }{
		{"POST", "/api/aggregator/refresh"},
		{"GET", "/api/refresh/runs"},
		{"GET", "/api/refresh/runs/1"},
	} {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s status = %d, want 401 without session", tc.method, tc.path, w.Code)
		}
	}
}

func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}

func TestAirportRefresh_StartsJob(t *testing.T) {
	srv, st := newTestServer(t, nil)
	if _, err := st.CreateAirport("测试机场", "http://example.com/sub"); err != nil {
		t.Fatalf("CreateAirport() error = %v", err)
	}
	h := srv.Handler()
	cookie := authedCookie(t, h)

	w := authedRequest(t, h, "POST", "/api/airports/1/refresh", cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}

	var resp struct {
		OK      bool   `json:"ok"`
		JobID   int64  `json:"jobId"`
		Kind    string `json:"kind"`
		Key     string `json:"key"`
		Started bool   `json:"started"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !resp.OK || resp.JobID != 43 || resp.Kind != "refresh" || resp.Key != "airport-1" || !resp.Started {
		t.Errorf("resp = %+v, want ok=true jobId=43 kind=refresh key=airport-1 started=true", resp)
	}
}

func TestAirportRefresh_ConflictWithFullRefresh(t *testing.T) {
	srv, st := newTestServer(t, nil)
	if _, err := st.CreateAirport("测试机场", "http://example.com/sub"); err != nil {
		t.Fatalf("CreateAirport() error = %v", err)
	}
	srv.nodes.(*fakeNodes).refreshErr = aggregator.ErrRefreshConflict
	h := srv.Handler()
	cookie := authedCookie(t, h)

	w := authedRequest(t, h, "POST", "/api/airports/1/refresh", cookie)
	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409 (body %s)", w.Code, w.Body.String())
	}
}

func TestAirportRefresh_NotFound(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	h := srv.Handler()
	cookie := authedCookie(t, h)

	w := authedRequest(t, h, "POST", "/api/airports/99999/refresh", cookie)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 (body %s)", w.Code, w.Body.String())
	}
}

func TestAirportRefresh_InvalidID(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	h := srv.Handler()
	cookie := authedCookie(t, h)

	w := authedRequest(t, h, "POST", "/api/airports/abc/refresh", cookie)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (body %s)", w.Code, w.Body.String())
	}
}
