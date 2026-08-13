package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/taliove/proxyhub/internal/store"
)

// issue #117:订阅链接重置的服务端全链验收——重置 API、宽限期旧链接可用、
// 过期语义、延长宽限、全部经 HTTP 接缝。

func postResetLink(t *testing.T, h http.Handler, cookie *http.Cookie, id int64) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost,
		"/api/endpoints/"+strconv.FormatInt(id, 10)+"/reset-link", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func postExtendGrace(t *testing.T, h http.Handler, cookie *http.Cookie, id int64) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost,
		"/api/endpoints/"+strconv.FormatInt(id, 10)+"/reset-link/extend", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestResetLinkAPI_RotatesAndServesGrace(t *testing.T) {
	srv, st := newTestServer(t, pullLogNodes())
	h := srv.Handler()
	cookie := authCookie(t, h)

	// 建端点并配一个配置字段(验证配置保留)
	ep, err := st.CreateEndpoint("我的订阅")
	if err != nil {
		t.Fatalf("create endpoint: %v", err)
	}
	if err := st.UpdateEndpointPublicNameForUser(ep.UserID, ep.ID, "旧名字"); err != nil {
		t.Fatalf("set public name: %v", err)
	}
	oldPath, oldToken := ep.Path, ep.Token

	// 重置
	w := postResetLink(t, h, cookie, ep.ID)
	if w.Code != http.StatusOK {
		t.Fatalf("reset status = %d: %s", w.Code, w.Body.String())
	}
	var resp store.Endpoint
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode reset response: %v", err)
	}
	if resp.Path == oldPath || resp.Token == oldToken {
		t.Errorf("response path/token not rotated: %+v", resp)
	}
	if resp.GraceExpiresAt == "" {
		t.Error("response grace_expires_at empty, want expiry for frontend display")
	}
	// 配置保留
	kept, _ := st.GetEndpointByID(ep.ID)
	if kept.PublicName != "旧名字" {
		t.Errorf("PublicName lost after reset: %q", kept.PublicName)
	}

	// 宽限期内旧链接照常下发
	req := httptest.NewRequest("GET", "/sub/"+oldPath+"?token="+oldToken, nil)
	req.RemoteAddr = "203.0.113.30:9999"
	wOld := httptest.NewRecorder()
	h.ServeHTTP(wOld, req)
	if wOld.Code != http.StatusOK {
		t.Fatalf("old link during grace: status = %d, want 200", wOld.Code)
	}

	// 新链接同样可用
	req2 := httptest.NewRequest("GET", "/sub/"+resp.Path+"?token="+resp.Token, nil)
	req2.RemoteAddr = "203.0.113.31:9999"
	wNew := httptest.NewRecorder()
	h.ServeHTTP(wNew, req2)
	if wNew.Code != http.StatusOK {
		t.Fatalf("new link: status = %d, want 200", wNew.Code)
	}

	// 旧链接 + 错 token:404(不暴露宽限存在)
	req3 := httptest.NewRequest("GET", "/sub/"+oldPath+"?token=wrong", nil)
	wBad := httptest.NewRecorder()
	h.ServeHTTP(wBad, req3)
	if wBad.Code != http.StatusNotFound {
		t.Errorf("old link wrong token: status = %d, want 404", wBad.Code)
	}

	// 宽限命中的拉取记独立状态
	statuses := pullStatusesFor(t, st, ep.ID)
	if statuses["203.0.113.30"][store.PullStatusGraceOK] != 1 {
		t.Errorf("grace pull statuses = %+v, want one grace_ok", statuses)
	}
	if statuses["203.0.113.31"][store.PullStatusOK] != 1 {
		t.Errorf("new link pull statuses = %+v, want one ok", statuses)
	}

	// 延长宽限 +3 天
	wExt := postExtendGrace(t, h, cookie, ep.ID)
	if wExt.Code != http.StatusOK {
		t.Fatalf("extend status = %d: %s", wExt.Code, wExt.Body.String())
	}
	var extResp store.Endpoint
	if err := json.Unmarshal(wExt.Body.Bytes(), &extResp); err != nil {
		t.Fatalf("decode extend response: %v", err)
	}
	if extResp.GraceExpiresAt <= resp.GraceExpiresAt {
		t.Errorf("extend did not move expiry: %q -> %q", resp.GraceExpiresAt, extResp.GraceExpiresAt)
	}
}

func TestResetLinkAPI_Isolation(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	cookie := authCookie(t, h)

	// 端点属于默认属主(测试库首个超管);换一个不存在/他人 id 应 404
	if w := postResetLink(t, h, cookie, 999999); w.Code != http.StatusNotFound {
		t.Errorf("reset unknown endpoint: status = %d, want 404", w.Code)
	}
	ep, _ := st.CreateEndpoint("未重置")
	if w := postExtendGrace(t, h, cookie, ep.ID); w.Code != http.StatusNotFound {
		t.Errorf("extend never-reset endpoint: status = %d, want 404", w.Code)
	}
}

func TestAdminResetLinkAPI(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	cookie := authCookie(t, h) // 测试库默认超管

	ep, _ := st.CreateEndpoint("被代为重置")
	req := httptest.NewRequest(http.MethodPost,
		"/api/admin/users/"+strconv.FormatInt(ep.UserID, 10)+"/endpoints/"+strconv.FormatInt(ep.ID, 10)+"/reset-link", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("admin reset status = %d: %s", w.Code, w.Body.String())
	}
	var resp store.Endpoint
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Path == ep.Path {
		t.Errorf("admin reset did not rotate path")
	}
}
