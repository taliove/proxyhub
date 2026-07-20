package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestScheduleAPI_GetDefault 未配置时返回默认值(03:30 / disabled)。
func TestScheduleAPI_GetDefault(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	h := srv.Handler()
	cookie := authCookie(t, h)

	req := httptest.NewRequest("GET", "/api/settings/schedule", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	var resp scheduleConfig
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.RetagTime != "03:30" {
		t.Errorf("retag_time = %q, want 03:30", resp.RetagTime)
	}
	if resp.RetagEnabled {
		t.Error("retag_enabled = true, want false by default")
	}
}

// TestScheduleAPI_RequiresAuth 无会话拒绝。
func TestScheduleAPI_RequiresAuth(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	h := srv.Handler()

	req := httptest.NewRequest("GET", "/api/settings/schedule", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 without session", w.Code)
	}
}

// TestScheduleAPI_SaveAndGet 保存后读回一致,且落到调度器消费的 settings 键。
func TestScheduleAPI_SaveAndGet(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	cookie := authCookie(t, h)

	body, _ := json.Marshal(scheduleConfig{RetagTime: "02:15", RetagEnabled: true})
	req := httptest.NewRequest("PUT", "/api/settings/schedule", bytes.NewReader(body))
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}

	// 调度器读取 schedule_retag_time / schedule_retag_enabled,断言写入契约。
	if v, _ := st.GetSetting("schedule_retag_time"); v != "02:15" {
		t.Errorf("stored retag time = %q, want 02:15", v)
	}
	if v, _ := st.GetSetting("schedule_retag_enabled"); v != "true" {
		t.Errorf("stored retag enabled = %q, want true", v)
	}

	req2 := httptest.NewRequest("GET", "/api/settings/schedule", nil)
	req2.AddCookie(cookie)
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req2)
	var resp scheduleConfig
	json.Unmarshal(w2.Body.Bytes(), &resp)
	if resp.RetagTime != "02:15" || !resp.RetagEnabled {
		t.Errorf("readback = %+v, want {02:15 true}", resp)
	}
}

// TestScheduleAPI_RejectsInvalidTime 非零填充/越界时刻返回 400。
func TestScheduleAPI_RejectsInvalidTime(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	h := srv.Handler()
	cookie := authCookie(t, h)

	for _, bad := range []string{"3:30", "24:00", "12:60", "1230", "", "aa:bb"} {
		body, _ := json.Marshal(scheduleConfig{RetagTime: bad, RetagEnabled: true})
		req := httptest.NewRequest("PUT", "/api/settings/schedule", bytes.NewReader(body))
		req.AddCookie(cookie)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("PUT %q: status = %d, want 400", bad, w.Code)
		}
	}
}
