package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/taliove/proxyhub/internal/store"
)

// loginCookie 只登录（不 setup），用于已初始化的场景。
func loginCookie(t *testing.T, h http.Handler, user, pass, ip string) *http.Cookie {
	t.Helper()
	w := doLogin(t, h, user, pass, ip)
	for _, c := range w.Result().Cookies() {
		if c.Name == "session" {
			return c
		}
	}
	t.Fatalf("no session cookie from login (status %d)", w.Code)
	return nil
}

func store_PullRecord(endpointID int64, ip string) store.PullRecord {
	return store.PullRecord{EndpointID: endpointID, IP: ip, UserAgent: "test"}
}

func timeNow() time.Time { return time.Now() }

// TestAuditAPI_LoginEventsRecorded 登录成功/失败后审计事件可查
func TestAuditAPI_LoginEventsRecorded(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")

	// 触发一次失败登录（错误密码）+ 一次成功登录
	doLogin(t, h, "owner", "wrong-password", "5.5.5.1")
	cookie := loginCookie(t, h, "owner", "a-very-strong-pass", "5.5.5.2")

	req := httptest.NewRequest("GET", "/api/audit/events", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("audit events status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}

	var resp struct {
		Total  int `json:"total"`
		Events []struct {
			EventType string `json:"event_type"`
			IP        string `json:"ip"`
		} `json:"events"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)

	var hasFailure, hasSuccess bool
	for _, e := range resp.Events {
		if e.EventType == "login_failure" {
			hasFailure = true
		}
		if e.EventType == "login_success" {
			hasSuccess = true
		}
	}
	if !hasFailure {
		t.Error("no login_failure event recorded")
	}
	if !hasSuccess {
		t.Error("no login_success event recorded")
	}
}

// TestAuditAPI_HoneypotRecorded 蜜罐命中记录审计
func TestAuditAPI_HoneypotRecorded(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	h := srv.Handler()
	// 先初始化系统（doSetup），否则 authCookie 里再 setup 会冲突
	doSetup(t, h, "owner", "a-very-strong-pass")

	// 蜜罐用户名尝试
	doLogin(t, h, "admin", "whatever", "6.6.6.6")

	cookie := loginCookie(t, h, "owner", "a-very-strong-pass", "6.6.6.7")
	req := httptest.NewRequest("GET", "/api/audit/events?event_type=honeypot_ban", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp struct {
		Total int `json:"total"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Total < 1 {
		t.Error("honeypot_ban event not recorded")
	}
}

// TestAuditAPI_BannedAndUnban 封禁列表 + 手动解封
func TestAuditAPI_BannedAndUnban(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")

	// 蜜罐命中 → 封禁 6.6.6.6
	doLogin(t, h, "root", "x", "6.6.6.6")

	cookie := loginCookie(t, h, "owner", "a-very-strong-pass", "7.7.7.7")

	// 查封禁列表
	req := httptest.NewRequest("GET", "/api/audit/banned", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("banned list status = %d, want 200", w.Code)
	}
	var resp struct {
		Banned []struct {
			IP string `json:"ip"`
		} `json:"banned"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	found := false
	for _, b := range resp.Banned {
		if b.IP == "6.6.6.6" {
			found = true
		}
	}
	if !found {
		t.Fatal("6.6.6.6 should be in banned list")
	}

	// 手动解封
	body, _ := json.Marshal(map[string]string{"ip": "6.6.6.6"})
	req = httptest.NewRequest("POST", "/api/audit/unban", bytes.NewReader(body))
	req.AddCookie(cookie)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("unban status = %d, want 200", w.Code)
	}

	// 校验 store 层已解封
	banned, _ := st.IsBanned("6.6.6.6", timeNow())
	if banned {
		t.Error("6.6.6.6 should be unbanned after /api/audit/unban")
	}
}

func TestStatsAPI_Global(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	cookie := authCookie(t, h)

	ep, _ := st.CreateEndpointForUser(1, "dev")
	st.RecordPull(store_PullRecord(ep.ID, "1.1.1.1"))
	st.RecordPull(store_PullRecord(ep.ID, "2.2.2.2"))

	req := httptest.NewRequest("GET", "/api/stats/global", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("global stats status = %d, want 200", w.Code)
	}
	var resp map[string]int
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["total_pulls"] != 2 {
		t.Errorf("total_pulls = %d, want 2", resp["total_pulls"])
	}
	if resp["unique_ips"] != 2 {
		t.Errorf("unique_ips = %d, want 2", resp["unique_ips"])
	}
}

func TestStatsAPI_Trend(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	cookie := authCookie(t, h)

	ep, _ := st.CreateEndpointForUser(1, "dev")
	st.RecordPull(store_PullRecord(ep.ID, "1.1.1.1"))

	req := httptest.NewRequest("GET", "/api/stats/trend?days=7", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("trend status = %d, want 200", w.Code)
	}
	var resp struct {
		Trend []struct {
			Alias string `json:"alias"`
			Count int    `json:"count"`
		} `json:"trend"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Trend) == 0 {
		t.Fatal("trend empty, want at least 1 point")
	}
	if resp.Trend[0].Alias != "dev" || resp.Trend[0].Count != 1 {
		t.Errorf("trend[0] = %+v, want alias=dev count=1", resp.Trend[0])
	}
}
