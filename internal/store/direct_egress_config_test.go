package store

import (
	"testing"

	"github.com/taliove/proxyhub/internal/detection"
)

func TestGetDirectEgressConfig_Defaults(t *testing.T) {
	st := newTestStore(t)

	cfg := st.GetDirectEgressConfig()
	def := detection.DefaultDirectEgressConfig()

	// 未设置任何项:fail-open,默认开 + 默认 DoH 端点 + 网卡空(自动识别)
	if !cfg.Enabled {
		t.Error("Enabled = false, want true (fail-open default)")
	}
	if cfg.DoHURL != def.DoHURL {
		t.Errorf("DoHURL = %q, want %q", cfg.DoHURL, def.DoHURL)
	}
	if cfg.Interface != "" {
		t.Errorf("Interface = %q, want empty (auto detect)", cfg.Interface)
	}
}

func TestGetDirectEgressConfig_Overrides(t *testing.T) {
	st := newTestStore(t)

	settings := map[string]string{
		settingDirectEgressEnabled:   "false",
		settingDirectEgressDoHURL:    "https://1.1.1.1/dns-query",
		settingDirectEgressInterface: "en0",
	}
	for k, v := range settings {
		if err := st.SetSetting(k, v); err != nil {
			t.Fatalf("SetSetting(%s) error = %v", k, err)
		}
	}

	cfg := st.GetDirectEgressConfig()
	if cfg.Enabled {
		t.Error("Enabled = true, want false (explicit off)")
	}
	if cfg.DoHURL != "https://1.1.1.1/dns-query" {
		t.Errorf("DoHURL = %q, want https://1.1.1.1/dns-query", cfg.DoHURL)
	}
	if cfg.Interface != "en0" {
		t.Errorf("Interface = %q, want en0", cfg.Interface)
	}
}

func TestGetDirectEgressConfig_EnabledValues(t *testing.T) {
	st := newTestStore(t)

	// 显式 "0" 同样视为关;其他非空值(含 "true")视为开(fail-open)
	st.SetSetting(settingDirectEgressEnabled, "0")
	if st.GetDirectEgressConfig().Enabled {
		t.Error(`Enabled("0") = true, want false`)
	}
	st.SetSetting(settingDirectEgressEnabled, "true")
	if !st.GetDirectEgressConfig().Enabled {
		t.Error(`Enabled("true") = false, want true`)
	}
	st.SetSetting(settingDirectEgressEnabled, "garbage")
	if !st.GetDirectEgressConfig().Enabled {
		t.Error(`Enabled("garbage") = false, want true (fail-open on unparsable)`)
	}
}

// TestDirectEgressSettingsRoundtrip 三个设置键经 settings 表读写往返不丢。
func TestDirectEgressSettingsRoundtrip(t *testing.T) {
	st := newTestStore(t)

	want := map[string]string{
		settingDirectEgressEnabled:   "true",
		settingDirectEgressDoHURL:    "https://223.5.5.5/dns-query",
		settingDirectEgressInterface: "en1",
	}
	if err := st.SaveSystemSettings(want); err != nil {
		t.Fatalf("SaveSystemSettings() error = %v", err)
	}
	got, err := st.GetSystemSettings()
	if err != nil {
		t.Fatalf("GetSystemSettings() error = %v", err)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("settings[%q] = %q, want %q", k, got[k], v)
		}
	}
}
