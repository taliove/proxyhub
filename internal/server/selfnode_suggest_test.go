package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/taliove/proxyhub/internal/geoip"
)

// selfNodeByName 从库中按名称找回自建节点,便于断言保存后的落库结果。
func selfNodeByName(t *testing.T, srv *Server, name string) (regionCode string, found bool) {
	t.Helper()
	nodes, err := srv.st.ListAllSelfHostedNodes()
	if err != nil {
		t.Fatalf("list self nodes: %v", err)
	}
	for _, n := range nodes {
		if n.Name == name {
			return n.RegionCode, true
		}
	}
	return "", false
}

// handleSuggestSelfNode 只测外部可观察行为:server query -> {name, regionCode}。
// 依赖(DNS/离线 GeoIP)以 fake 注入,不触真实网络与内嵌库内容。
func TestHandleSuggestSelfNode(t *testing.T) {
	// countryLookup 断言不该被调用时用它,命中即让用例失败。
	failCountry := func(*testing.T) func(string) (string, error) {
		return func(string) (string, error) { return "", errors.New("countryLookup must not be called") }
	}

	cases := []struct {
		name       string
		server     string
		lookupHost func(string) ([]string, error)
		country    func(string) (string, error)
		wantName   string
		wantCode   string
	}{
		{
			name:     "IP 直连命中",
			server:   "8.8.8.8",
			country:  func(string) (string, error) { return "HK", nil },
			wantName: "自建香港",
			wantCode: "HK",
		},
		{
			name:       "域名 DNS 命中取首个 IP",
			server:     "hk.example.com",
			lookupHost: func(string) ([]string, error) { return []string{"1.2.3.4", "5.6.7.8"}, nil },
			country:    func(string) (string, error) { return "JP", nil },
			wantName:   "自建日本",
			wantCode:   "JP",
		},
		{
			name:       "DNS 失败退化为空",
			server:     "bad.invalid",
			lookupHost: func(string) ([]string, error) { return nil, errors.New("no such host") },
			country:    failCountry(t),
			wantName:   "",
			wantCode:   "",
		},
		{
			name:     "私网 ErrCountryNotFound 退化为空",
			server:   "192.168.0.1",
			country:  func(string) (string, error) { return "", geoip.ErrCountryNotFound },
			wantName: "",
			wantCode: "",
		},
		{
			name:     "未知国家码(中文名缺失)退化为空",
			server:   "8.8.8.8",
			country:  func(string) (string, error) { return "ZZ", nil },
			wantName: "",
			wantCode: "",
		},
		{
			name:     "server 为空退化为空",
			server:   "",
			country:  failCountry(t),
			wantName: "",
			wantCode: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := newTestServer(t, nil)
			if tc.lookupHost != nil {
				srv.lookupHost = tc.lookupHost
			}
			if tc.country != nil {
				srv.countryLookup = tc.country
			}

			target := "/api/self-nodes/suggest?server=" + url.QueryEscape(tc.server)
			req := httptest.NewRequest(http.MethodGet, target, nil)
			w := httptest.NewRecorder()
			srv.handleSuggestSelfNode(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200, body = %s", w.Code, w.Body.String())
			}
			var res map[string]string
			if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if res["name"] != tc.wantName {
				t.Errorf("name = %q, want %q", res["name"], tc.wantName)
			}
			if res["regionCode"] != tc.wantCode {
				t.Errorf("regionCode = %q, want %q", res["regionCode"], tc.wantCode)
			}
		})
	}
}

// createSelfNode 走 handleCreateSelfNode 发一次新增请求,返回响应记录。
func createSelfNode(t *testing.T, srv *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/self-nodes", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleCreateSelfNode(w, req)
	return w
}

// 保存兜底:名称为空 + 地区解析成功 -> 后端自动命名 "自建{中文名}" 落库,
// RegionCode 以后端重新解析为准。
func TestCreateSelfNode_EmptyNameRegionHitAutoNames(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	srv.countryLookup = func(string) (string, error) { return "HK", nil }

	w := createSelfNode(t, srv, `{"name":"","protocol":"vless","server":"8.8.8.8","port":443,"uuid":"00000000-0000-0000-0000-000000000000"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", w.Code, w.Body.String())
	}

	code, found := selfNodeByName(t, srv, "自建香港")
	if !found {
		t.Fatalf("node %q not persisted", "自建香港")
	}
	if code != "HK" {
		t.Errorf("RegionCode = %q, want HK (backend re-resolved)", code)
	}
}

// 保存兜底:名称为空 + 地区解析失败(私网 + 名称不可识别)-> 400 名称必填,不落库。
func TestCreateSelfNode_EmptyNameRegionMissRejected(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	srv.countryLookup = func(string) (string, error) { return "", geoip.ErrCountryNotFound }

	w := createSelfNode(t, srv, `{"name":"","protocol":"vless","server":"10.0.0.1","port":443,"uuid":"00000000-0000-0000-0000-000000000000"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body = %s", w.Code, w.Body.String())
	}

	nodes, err := srv.st.ListAllSelfHostedNodes()
	if err != nil {
		t.Fatalf("list self nodes: %v", err)
	}
	if len(nodes) != 0 {
		t.Errorf("persisted %d nodes, want 0 (rejected)", len(nodes))
	}
}

// 用户已填名称:保存兜底不改动名称;RegionCode 仍以后端重新解析为准。
func TestCreateSelfNode_UserNamePreserved(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	srv.countryLookup = func(string) (string, error) { return "HK", nil }

	w := createSelfNode(t, srv, `{"name":"我的主力节点","protocol":"vless","server":"8.8.8.8","port":443,"uuid":"00000000-0000-0000-0000-000000000000"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", w.Code, w.Body.String())
	}

	code, found := selfNodeByName(t, srv, "我的主力节点")
	if !found {
		t.Fatalf("node with user name not persisted")
	}
	if code != "HK" {
		t.Errorf("RegionCode = %q, want HK (backend re-resolved)", code)
	}
}
