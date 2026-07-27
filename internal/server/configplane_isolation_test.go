package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

// TestConfigPlane_TenantSettingsIsolation 配置面租户级设置(视角驱动):
// 普通用户只见租户级键(带 overridden 标记),写入只落本人 user_settings,
// 超管专属键被忽略;超管未 impersonate 读写全局。reset 删除覆盖回到跟随全局。
func TestConfigPlane_TenantSettingsIsolation(t *testing.T) {
	srv, st := newTestServer(t, nil)
	admin := UserScope{UserID: 1, Role: store.RoleSuperAdmin}
	member := UserScope{UserID: 2, Role: store.RoleUser}

	// 全局默认:filter_keywords = global-bad,ban_threshold = 5(超管专属)
	if err := st.SaveSystemSettings(map[string]string{
		"filter_keywords": "global-bad",
		"ban_threshold":   "5",
	}); err != nil {
		t.Fatalf("seed global settings: %v", err)
	}

	getSettings := func(scope UserScope) (map[string]string, map[string]bool, int) {
		rec := httptest.NewRecorder()
		srv.handleGetSettings(rec, scopedRequest(http.MethodGet, "/api/settings", scope))
		var resp struct {
			Settings   map[string]string `json:"settings"`
			Overridden map[string]bool   `json:"overridden"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		return resp.Settings, resp.Overridden, rec.Code
	}

	postSettings := func(scope UserScope, body string) int {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/settings", strings.NewReader(body))
		req = req.WithContext(ContextWithUserScope(req.Context(), scope))
		srv.handleSaveSettings(rec, req)
		return rec.Code
	}

	t.Run("member sees only tenant keys with effective values", func(t *testing.T) {
		settings, overridden, code := getSettings(member)
		if code != http.StatusOK {
			t.Fatalf("status = %d", code)
		}
		if settings["filter_keywords"] != "global-bad" {
			t.Errorf("effective filter_keywords = %q, want global default", settings["filter_keywords"])
		}
		if overridden["filter_keywords"] {
			t.Error("filter_keywords marked overridden before any override")
		}
		if _, leaked := settings["ban_threshold"]; leaked {
			t.Error("admin-only key ban_threshold leaked to regular user")
		}
	})

	t.Run("member writes only tenant keys; admin keys ignored", func(t *testing.T) {
		code := postSettings(member, `{"settings":{"filter_keywords":"user-bad","ban_threshold":"99"}}`)
		if code != http.StatusOK {
			t.Fatalf("status = %d", code)
		}
		// 租户级键落 user_settings
		if v, _ := st.GetUserSetting(2, "filter_keywords"); v != "user-bad" {
			t.Errorf("user setting = %q, want user-bad", v)
		}
		// 超管专属键不落:全局保持 5,用户表无覆盖
		if v, _ := st.GetSetting("ban_threshold"); v != "5" {
			t.Errorf("global ban_threshold = %q, want 5 (untouched)", v)
		}
		if _, err := st.GetUserSetting(2, "ban_threshold"); err == nil {
			t.Error("ban_threshold override landed in user_settings")
		}
		// 生效视图反映覆盖
		settings, overridden, _ := getSettings(member)
		if settings["filter_keywords"] != "user-bad" || !overridden["filter_keywords"] {
			t.Errorf("effective = %q overridden=%v, want user-bad true",
				settings["filter_keywords"], overridden["filter_keywords"])
		}
	})

	t.Run("reset returns to following global", func(t *testing.T) {
		if code := postSettings(member, `{"reset":["filter_keywords"]}`); code != http.StatusOK {
			t.Fatalf("status = %d", code)
		}
		settings, overridden, _ := getSettings(member)
		if settings["filter_keywords"] != "global-bad" || overridden["filter_keywords"] {
			t.Errorf("after reset = %q overridden=%v, want global-bad false",
				settings["filter_keywords"], overridden["filter_keywords"])
		}
	})

	t.Run("admin global view unchanged", func(t *testing.T) {
		settings, _, code := getSettings(admin)
		if code != http.StatusOK {
			t.Fatalf("status = %d", code)
		}
		if settings["ban_threshold"] != "5" {
			t.Errorf("admin global view missing ban_threshold: %v", settings)
		}
		if code := postSettings(admin, `{"settings":{"filter_keywords":"global-v2"}}`); code != http.StatusOK {
			t.Fatalf("admin save status = %d", code)
		}
		if v, _ := st.GetSetting("filter_keywords"); v != "global-v2" {
			t.Errorf("global filter_keywords = %q, want global-v2", v)
		}
		// member 未覆盖,跟随新全局值
		settings, _, _ = getSettings(member)
		if settings["filter_keywords"] != "global-v2" {
			t.Errorf("member follows global change: = %q, want global-v2", settings["filter_keywords"])
		}
	})
}

// TestConfigPlane_FilterChainPerUser 订阅过滤链按属主读租户级设置:
// 普通用户的关键词黑名单只作用于自己的下发链,不改变全局/他人。
func TestConfigPlane_FilterChainPerUser(t *testing.T) {
	srv, st := newTestServer(t, nil)

	if err := st.SetUserSetting(2, "filter_keywords", "badword"); err != nil {
		t.Fatalf("set user filter: %v", err)
	}
	if err := st.SetSetting("filter_keywords", "adminword"); err != nil {
		t.Fatalf("set global filter: %v", err)
	}

	nodes := filteredFixtureNodes()
	// member 的链:命中 user 的 "badword"
	memberOut := srv.filteredNodes(nodes, 2)
	for _, n := range memberOut {
		if strings.Contains(n.Name, "badword") {
			t.Errorf("member filter chain leaked badword node: %s", n.Name)
		}
	}
	// admin(uid 0 = 全局视角)的链:命中全局 "adminword","badword" 不受影响
	adminOut := srv.filteredNodes(nodes, 0)
	sawBad, sawAdmin := false, false
	for _, n := range adminOut {
		if strings.Contains(n.Name, "badword") {
			sawBad = true
		}
		if strings.Contains(n.Name, "adminword") {
			sawAdmin = true
		}
	}
	if !sawBad {
		t.Error("admin chain wrongly applied member's filter_keywords")
	}
	if sawAdmin {
		t.Error("admin chain did not apply global filter_keywords")
	}
}

// filteredFixtureNodes 过滤链夹具:自建豁免 + 两个机场节点(全零 UUID + example.com)。
func filteredFixtureNodes() []*subscription.Node {
	return []*subscription.Node{
		{Name: "good badword node", Server: "a.example.com", Port: 443, Type: "vmess",
			UUID: "00000000-0000-0000-0000-000000000000", Source: "airport", Available: true},
		{Name: "good adminword node", Server: "b.example.com", Port: 443, Type: "vmess",
			UUID: "00000000-0000-0000-0000-000000000000", Source: "airport", Available: true},
	}
}
