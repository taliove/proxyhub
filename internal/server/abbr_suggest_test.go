package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// abbr-suggest 只测外部可观察行为:名称 query -> {"abbr": ...} 响应体。
// 推导规则本身由 subscription 包测试覆盖,这里只验证 handler 正确复用并回传。
func TestHandleAbbrSuggest(t *testing.T) {
	srv, _ := newTestServer(t, nil)

	cases := []struct {
		name string // 用例说明
		in   string // name query 参数
		want string // 期望简称
	}{
		{"典型中文名剥离通用后缀", "极速机场", "JS"},
		{"英文名取词首字母", "FlowerCloud", "FC"},
		{"无法推导返回空串", "!!!", ""},
		{"名称为空返回空串", "", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			target := "/api/airports/abbr-suggest?name=" + url.QueryEscape(tc.in)
			req := httptest.NewRequest(http.MethodGet, target, nil)
			w := httptest.NewRecorder()
			srv.handleSuggestAbbr(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200, body = %s", w.Code, w.Body.String())
			}
			var res map[string]string
			if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if res["abbr"] != tc.want {
				t.Errorf("abbr = %q, want %q", res["abbr"], tc.want)
			}
		})
	}
}
