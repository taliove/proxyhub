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
	markAllMFAEnrolled(t, testStore(t))
	w := doLoginEnrolled(t, h, user, pass, ip)
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

// TestAuditAPI_RegularUserForbidden 安全审计面是超管专属(含写操作解封 IP):
// 普通用户会话访问 events/banned/unban 一律 403。
func TestAuditAPI_RegularUserForbidden(t *testing.T) {
	srv, st := newTestServer(t, nil)
	seedSuperAdmin(t, st)
	memberID := seedRegularUser(t, st, "member", "member-pass-1")
	memberCookie := memberSession(t, srv, memberID)

	endpoints := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/audit/events"},
		{http.MethodGet, "/api/audit/banned"},
		{http.MethodPost, "/api/audit/ban"},
		{http.MethodPost, "/api/audit/unban"},
	}
	for _, ep := range endpoints {
		t.Run(ep.method+" "+ep.path, func(t *testing.T) {
			w := serveAdminHTTP(t, srv, memberCookie, ep.method, ep.path, nil)
			if w.Code != http.StatusForbidden {
				t.Errorf("status = %d, want 403; body=%s", w.Code, w.Body.String())
			}
		})
	}
}

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

// TestAuditAPI_BanIP_WithDuration tests manual IP banning with time duration
func TestAuditAPI_BanIP_WithDuration(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")
	cookie := loginCookie(t, h, "owner", "a-very-strong-pass", "7.7.7.7")

	// Ban for 1 hour
	body, _ := json.Marshal(map[string]string{"ip": "8.8.8.8", "duration": "1h"})
	req := httptest.NewRequest("POST", "/api/audit/ban", bytes.NewReader(body))
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ban status = %d, want 200; body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		Success     bool   `json:"success"`
		IP          string `json:"ip"`
		BannedUntil string `json:"banned_until"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if !resp.Success {
		t.Error("ban response success = false, want true")
	}
	if resp.IP != "8.8.8.8" {
		t.Errorf("ip = %s, want 8.8.8.8", resp.IP)
	}

	// Verify IP is banned in store
	banned, _ := st.IsBanned("8.8.8.8", timeNow())
	if !banned {
		t.Error("8.8.8.8 should be banned")
	}

	// Verify audit event was recorded
	filter := store.AuditFilter{EventTypes: []string{"ip_banned"}}
	events, _, _ := st.ListAuditEvents(filter, 10, 0)
	found := false
	for _, e := range events {
		if e.EventType == "ip_banned" {
			found = true
			// User agent should be captured (even if empty in tests)
			break
		}
	}
	if !found {
		t.Error("ip_banned audit event not recorded")
	}
}

// TestAuditAPI_BanIP_Permanent tests permanent IP banning
func TestAuditAPI_BanIP_Permanent(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")
	cookie := loginCookie(t, h, "owner", "a-very-strong-pass", "7.7.7.7")

	// Ban permanently
	body, _ := json.Marshal(map[string]string{"ip": "9.9.9.9", "duration": "permanent"})
	req := httptest.NewRequest("POST", "/api/audit/ban", bytes.NewReader(body))
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("permanent ban status = %d, want 200; body=%s", w.Code, w.Body.String())
	}

	// Verify IP is banned
	banned, _ := st.IsBanned("9.9.9.9", timeNow())
	if !banned {
		t.Error("9.9.9.9 should be permanently banned")
	}

	// Verify far-future ban (should still be banned 50 years from now)
	farFuture := timeNow().AddDate(50, 0, 0)
	stillBanned, _ := st.IsBanned("9.9.9.9", farFuture)
	if !stillBanned {
		t.Error("9.9.9.9 should still be banned 50 years from now (permanent)")
	}
}

// TestAuditAPI_BanIP_InvalidRequests tests error cases
func TestAuditAPI_BanIP_InvalidRequests(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")
	cookie := loginCookie(t, h, "owner", "a-very-strong-pass", "7.7.7.7")

	tests := []struct {
		name       string
		body       map[string]string
		wantStatus int
	}{
		{
			name:       "missing IP",
			body:       map[string]string{"duration": "1h"},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing duration",
			body:       map[string]string{"ip": "10.10.10.10"},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid duration format",
			body:       map[string]string{"ip": "10.10.10.10", "duration": "invalid"},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "negative duration",
			body:       map[string]string{"ip": "10.10.10.10", "duration": "-1h"},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "zero duration",
			body:       map[string]string{"ip": "10.10.10.10", "duration": "0"},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.body)
			req := httptest.NewRequest("POST", "/api/audit/ban", bytes.NewReader(body))
			req.AddCookie(cookie)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body=%s", w.Code, tt.wantStatus, w.Body.String())
			}
		})
	}
}

// TestAuditAPI_BanIP_RegularUserForbidden tests that regular users cannot ban IPs
func TestAuditAPI_BanIP_RegularUserForbidden(t *testing.T) {
	srv, st := newTestServer(t, nil)
	seedSuperAdmin(t, st)
	memberID := seedRegularUser(t, st, "member", "member-pass-1")
	memberCookie := memberSession(t, srv, memberID)

	body, _ := json.Marshal(map[string]string{"ip": "11.11.11.11", "duration": "1h"})
	w := serveAdminHTTP(t, srv, memberCookie, "POST", "/api/audit/ban", body)
	if w.Code != http.StatusForbidden {
		t.Errorf("regular user ban status = %d, want 403", w.Code)
	}
}

// TestAuditAPI_EventsWithGeo tests that audit events include geo information
func TestAuditAPI_EventsWithGeo(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")

	// Create a login event from a public IP
	publicIP := "8.8.8.8"

	// Seed geo data for the IP
	st.SaveGeo(store.GeoInfo{
		IP:      publicIP,
		Country: "美国",
		Region:  "加利福尼亚",
		City:    "山景城",
		ISP:     "Google LLC",
	})

	// Trigger a login failure to create an audit event
	doLogin(t, h, "owner", "wrong-password", publicIP)

	cookie := loginCookie(t, h, "owner", "a-very-strong-pass", "7.7.7.7")

	// Fetch audit events
	req := httptest.NewRequest("GET", "/api/audit/events", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("audit events status = %d, want 200", w.Code)
	}

	var resp struct {
		Events []struct {
			EventType string `json:"event_type"`
			IP        string `json:"ip"`
			Geo       struct {
				Country string `json:"country"`
				Region  string `json:"region"`
				City    string `json:"city"`
				ISP     string `json:"isp"`
			} `json:"geo"`
		} `json:"events"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)

	// Find the event with our IP
	found := false
	for _, e := range resp.Events {
		if e.IP == publicIP {
			found = true
			if e.Geo.Country != "美国" {
				t.Errorf("geo.country = %q, want 美国", e.Geo.Country)
			}
			if e.Geo.Region != "加利福尼亚" {
				t.Errorf("geo.region = %q, want 加利福尼亚", e.Geo.Region)
			}
			if e.Geo.City != "山景城" {
				t.Errorf("geo.city = %q, want 山景城", e.Geo.City)
			}
			if e.Geo.ISP != "Google LLC" {
				t.Errorf("geo.isp = %q, want Google LLC", e.Geo.ISP)
			}
			break
		}
	}
	if !found {
		t.Error("event with public IP not found in audit events")
	}
}

// TestAuditAPI_EventsGeoPrivateIP tests that private/loopback IPs have empty geo
func TestAuditAPI_EventsGeoPrivateIP(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	h := srv.Handler()
	doSetup(t, h, "owner", "a-very-strong-pass")

	// Trigger events from private and loopback IPs
	doLogin(t, h, "owner", "wrong-password", "192.168.1.1")
	doLogin(t, h, "owner", "wrong-password", "127.0.0.1")

	cookie := loginCookie(t, h, "owner", "a-very-strong-pass", "7.7.7.7")

	// Fetch audit events
	req := httptest.NewRequest("GET", "/api/audit/events", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("audit events status = %d, want 200", w.Code)
	}

	var resp struct {
		Events []struct {
			IP  string `json:"ip"`
			Geo struct {
				Country string `json:"country"`
				Region  string `json:"region"`
				City    string `json:"city"`
				ISP     string `json:"isp"`
			} `json:"geo"`
		} `json:"events"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)

	// Check private/loopback IPs have empty geo
	for _, e := range resp.Events {
		if e.IP == "192.168.1.1" || e.IP == "127.0.0.1" {
			if e.Geo.Country != "" || e.Geo.Region != "" || e.Geo.City != "" || e.Geo.ISP != "" {
				t.Errorf("private/loopback IP %s should have empty geo, got %+v", e.IP, e.Geo)
			}
		}
	}
}
