package alert

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeSettings 实现 SettingsSource，返回固定的 webhook
type fakeSettings struct {
	webhook string
}

func (f fakeSettings) GetSetting(key string) (string, error) {
	if key == "feishu_webhook" {
		return f.webhook, nil
	}
	return "", nil
}

func TestAlert_SendsCard(t *testing.T) {
	var received []byte
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	a := NewAlerter(fakeSettings{webhook: ts.URL})
	if err := a.Alert("测试标题", "测试内容"); err != nil {
		t.Fatalf("Alert() error = %v", err)
	}

	var msg map[string]any
	if err := json.Unmarshal(received, &msg); err != nil {
		t.Fatalf("payload is not valid JSON: %v", err)
	}
	if msg["msg_type"] != "interactive" {
		t.Errorf("msg_type = %v, want interactive", msg["msg_type"])
	}
	if !strings.Contains(string(received), "测试标题") {
		t.Error("payload missing title")
	}
}

func TestAlert_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	a := NewAlerter(fakeSettings{webhook: ts.URL})
	if err := a.Alert("t", "c"); err == nil {
		t.Error("Alert() expected error on 500, got nil")
	}
}

func TestAlert_NoWebhookConfigured(t *testing.T) {
	// 未配置 webhook 时应静默跳过，不报错
	a := NewAlerter(fakeSettings{webhook: ""})
	if err := a.Alert("t", "c"); err != nil {
		t.Errorf("Alert() with empty webhook should be silent, got error: %v", err)
	}
}

func TestAlertAirportDown(t *testing.T) {
	var received []byte
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	a := NewAlerter(fakeSettings{webhook: ts.URL})
	if err := a.AlertAirportDown("机场A", 50); err != nil {
		t.Fatalf("AlertAirportDown() error = %v", err)
	}
	if !strings.Contains(string(received), "机场A") {
		t.Error("payload missing airport name")
	}
}

func TestAlertLowAvailability(t *testing.T) {
	var received []byte
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	a := NewAlerter(fakeSettings{webhook: ts.URL})
	if err := a.AlertLowAvailability(3, 10); err != nil {
		t.Fatalf("AlertLowAvailability() error = %v", err)
	}
	if !strings.Contains(string(received), "3") {
		t.Error("payload missing available count")
	}
}
