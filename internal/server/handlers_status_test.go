package server

// /api/status 版本自报测试:SetVersion 注入后,status 响应携带 version 与
// build_time;未注入时 version 为空串(向后兼容:initialized 字段语义不变)。
import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleStatusReportsVersion(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	srv.SetVersion("1.2.3", "2026-07-28_00:00:00")

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status code = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	var body struct {
		Initialized bool   `json:"initialized"`
		Version     string `json:"version"`
		BuildTime   string `json:"build_time"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode status body: %v", err)
	}
	if body.Version != "1.2.3" {
		t.Errorf("version = %q, want %q", body.Version, "1.2.3")
	}
	if body.BuildTime != "2026-07-28_00:00:00" {
		t.Errorf("build_time = %q, want %q", body.BuildTime, "2026-07-28_00:00:00")
	}
}

func TestHandleStatusWithoutVersionInjection(t *testing.T) {
	srv, _ := newTestServer(t, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status code = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	var body struct {
		Initialized *bool  `json:"initialized"`
		Version     string `json:"version"`
		BuildTime   string `json:"build_time"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode status body: %v", err)
	}
	if body.Initialized == nil {
		t.Error("missing initialized field (backward compatibility)")
	}
	if body.Version != "" {
		t.Errorf("version = %q, want empty when not injected", body.Version)
	}
	if body.BuildTime != "" {
		t.Errorf("build_time = %q, want empty when not injected", body.BuildTime)
	}
}
