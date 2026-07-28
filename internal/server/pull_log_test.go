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

// pullLogNodes 一个可下发的最小节点池,让 /sub 成功路径能走到内容生成。
func pullLogNodes() []*subscription.Node {
	return []*subscription.Node{
		{Name: "香港-01", Type: "ss", Server: "1.2.3.4", Port: 8388,
			Cipher: "aes-256-gcm", Password: "p", Available: true},
	}
}

// pullStatusesFor 汇总某订阅地址下 IP -> status -> 次数,方便断言留痕。
func pullStatusesFor(t *testing.T, st *store.Store, endpointID int64) map[string]map[string]int {
	t.Helper()
	stats, err := st.EndpointStats(endpointID)
	if err != nil {
		t.Fatalf("EndpointStats: %v", err)
	}
	out := map[string]map[string]int{}
	for _, s := range stats {
		if out[s.IP] == nil {
			out[s.IP] = map[string]int{}
		}
		out[s.IP][s.Status] = s.Count
	}
	return out
}

// TestSubscription_RecordsOKStatus 成功下发留 ok。
func TestSubscription_RecordsOKStatus(t *testing.T) {
	srv, st := newTestServer(t, pullLogNodes())
	h := srv.Handler()
	ep, _ := st.CreateEndpoint("ok 设备")

	req := httptest.NewRequest("GET", "/sub/"+ep.Path+"?token="+ep.Token, nil)
	req.RemoteAddr = "5.6.7.8:1234"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}

	got := pullStatusesFor(t, st, ep.ID)
	if got["5.6.7.8"][store.PullStatusOK] != 1 {
		t.Errorf("pull statuses = %+v, want one ok row for 5.6.7.8", got)
	}
}

// TestSubscription_RecordsBadTokenStatus token 不匹配留 bad_token,外部仍是 404。
func TestSubscription_RecordsBadTokenStatus(t *testing.T) {
	srv, st := newTestServer(t, pullLogNodes())
	h := srv.Handler()
	ep, _ := st.CreateEndpoint("坏 token 设备")

	req := httptest.NewRequest("GET", "/sub/"+ep.Path+"?token=wrong", nil)
	req.RemoteAddr = "9.9.9.9:2222"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	// 外部响应不得泄漏区分信息。
	if body := w.Body.String(); body != "404 page not found\n" {
		t.Errorf("body = %q, want the stock 404 body", body)
	}

	got := pullStatusesFor(t, st, ep.ID)
	if got["9.9.9.9"][store.PullStatusBadToken] != 1 {
		t.Errorf("pull statuses = %+v, want one bad_token row for 9.9.9.9", got)
	}
	if got["9.9.9.9"][store.PullStatusOK] != 0 {
		t.Errorf("blocked pull must not count as ok: %+v", got)
	}
}

// TestSubscription_RecordsDisabledStatus 禁用地址留 disabled。
func TestSubscription_RecordsDisabledStatus(t *testing.T) {
	srv, st := newTestServer(t, pullLogNodes())
	h := srv.Handler()
	ep, _ := st.CreateEndpoint("禁用设备")
	if err := st.SetEndpointEnabled(ep.ID, false); err != nil {
		t.Fatalf("SetEndpointEnabled: %v", err)
	}

	req := httptest.NewRequest("GET", "/sub/"+ep.Path+"?token="+ep.Token, nil)
	req.RemoteAddr = "7.7.7.7:3333"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}

	got := pullStatusesFor(t, st, ep.ID)
	if got["7.7.7.7"][store.PullStatusDisabled] != 1 {
		t.Errorf("pull statuses = %+v, want one disabled row for 7.7.7.7", got)
	}
}

// TestSubscription_UnknownPathRecordsBadToken 未知 path 没有可归属的地址,
// 留痕落全局桶(endpoint_id=0),不能污染任何真实地址的统计。
func TestSubscription_UnknownPathRecordsBadToken(t *testing.T) {
	srv, st := newTestServer(t, pullLogNodes())
	h := srv.Handler()
	ep, _ := st.CreateEndpoint("旁观设备")

	req := httptest.NewRequest("GET", "/sub/nonexistent?token=x", nil)
	req.RemoteAddr = "8.8.8.8:4444"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}

	if got := pullStatusesFor(t, st, ep.ID); len(got) != 0 {
		t.Errorf("existing endpoint stats polluted by unknown path: %+v", got)
	}
	got := pullStatusesFor(t, st, 0)
	if got["8.8.8.8"][store.PullStatusBadToken] != 1 {
		t.Errorf("global bucket = %+v, want one bad_token row for 8.8.8.8", got)
	}
}

// TestSubscription_EmptyPoolLeavesNoTrace 池空是服务端状态(503),不是客户端可
// 归因的拉取结果,不留痕。
func TestSubscription_EmptyPoolLeavesNoTrace(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	ep, _ := st.CreateEndpoint("空池设备")

	req := httptest.NewRequest("GET", "/sub/"+ep.Path+"?token="+ep.Token, nil)
	req.RemoteAddr = "4.4.4.4:5555"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
	if got := pullStatusesFor(t, st, ep.ID); len(got) != 0 {
		t.Errorf("empty pool must not leave a pull trace: %+v", got)
	}
}

// TestEndpointStatsAPI_ExposesStatus 单地址 IP 明细接口按 IP+status 分组返回。
func TestEndpointStatsAPI_ExposesStatus(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	cookie := authCookie(t, h)

	ep, _ := st.CreateEndpointForUser(1, "明细设备")
	st.RecordPull(store.PullRecord{EndpointID: ep.ID, IP: "1.1.1.1", Status: store.PullStatusOK})
	st.RecordPull(store.PullRecord{EndpointID: ep.ID, IP: "1.1.1.1", Status: store.PullStatusOK})
	st.RecordPull(store.PullRecord{EndpointID: ep.ID, IP: "1.1.1.1", Status: store.PullStatusBadToken})
	st.RecordPull(store.PullRecord{EndpointID: ep.ID, IP: "2.2.2.2", Status: store.PullStatusDisabled})

	req := httptest.NewRequest("GET", "/api/endpoints/"+strconv.FormatInt(ep.ID, 10)+"/stats", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}

	var resp []struct {
		IP     string `json:"ip"`
		Status string `json:"status"`
		Count  int    `json:"count"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v (body %s)", err, w.Body.String())
	}
	if len(resp) != 3 {
		t.Fatalf("len(resp) = %d, want 3 (ip x status rows): %+v", len(resp), resp)
	}
	seen := map[string]int{}
	for _, row := range resp {
		seen[row.IP+"/"+row.Status] = row.Count
	}
	for key, want := range map[string]int{
		"1.1.1.1/" + store.PullStatusOK:       2,
		"1.1.1.1/" + store.PullStatusBadToken: 1,
		"2.2.2.2/" + store.PullStatusDisabled: 1,
	} {
		if seen[key] != want {
			t.Errorf("row %s count = %d, want %d (all: %+v)", key, seen[key], want, seen)
		}
	}
}
