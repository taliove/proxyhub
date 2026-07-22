package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/taliove/proxyhub/internal/speedtest"
)

// speedtestAPIPaths 本机实测全部端点(方法与路径)。
func speedtestAPIPaths() [][2]string {
	return [][2]string{
		{"GET", "/api/speedtest/ping"},
		{"GET", "/api/speedtest/download"},
		{"POST", "/api/speedtest/upload"},
		{"POST", "/api/speedtest/results"},
		{"GET", "/api/speedtest/results"},
		{"DELETE", "/api/speedtest/results/1"},
	}
}

func doReq(t *testing.T, h http.Handler, method, path string, body io.Reader, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, body)
	req.RemoteAddr = "10.0.0.1:1000"
	if cookie != nil {
		req.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// TestSpeedtest_Unauthenticated 未认证访问全部测速端点一律 401(到达鉴权层)。
func TestSpeedtest_Unauthenticated(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	h := srv.Handler()

	for _, ep := range speedtestAPIPaths() {
		if w := doReq(t, h, ep[0], ep[1], nil, nil); w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: status = %d, want 401", ep[0], ep[1], w.Code)
		}
	}
}

// TestSpeedtest_SitePathPrefix 配置 Site Path 后:前缀下未认证 401(可达鉴权层)、
// 无前缀一律 404,行为与全站一致(照 sitepath_routing_test.go 模板)。
func TestSpeedtest_SitePathPrefix(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	cookie := authCookie(t, h)
	if err := st.SetSitePath(testSitePath); err != nil {
		t.Fatalf("SetSitePath: %v", err)
	}

	// 前缀下:未认证 401,认证后可达
	for _, ep := range speedtestAPIPaths() {
		prefixed := "/" + testSitePath + ep[1]
		if w := doReq(t, h, ep[0], prefixed, nil, nil); w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: status = %d, want 401 (reachable under prefix)", ep[0], prefixed, w.Code)
		}
	}
	if w := doReq(t, h, "GET", "/"+testSitePath+"/api/speedtest/ping", nil, cookie); w.Code != http.StatusOK {
		t.Errorf("GET /<site-path>/api/speedtest/ping authed: status = %d, want 200", w.Code)
	}

	// 无前缀:一律 404
	for _, ep := range speedtestAPIPaths() {
		if w := doReq(t, h, ep[0], ep[1], nil, cookie); w.Code != http.StatusNotFound {
			t.Errorf("%s %s: status = %d, want 404 (prefixless blocked)", ep[0], ep[1], w.Code)
		}
	}
}

// TestSpeedtest_Ping 延迟探测:认证后 200,极小响应体,禁缓存。
func TestSpeedtest_Ping(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	h := srv.Handler()
	cookie := authCookie(t, h)

	w := doReq(t, h, "GET", "/api/speedtest/ping", nil, cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("ping status = %d, want 200", w.Code)
	}
	if w.Body.Len() == 0 || w.Body.Len() > 64 {
		t.Errorf("ping body len = %d, want tiny (>0, <=64)", w.Body.Len())
	}
	if cc := w.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Errorf("ping Cache-Control = %q, want no-store", cc)
	}
}

// TestSpeedtest_Download 下行发流:不可压缩随机字节、显式禁压缩头、时长内结束。
func TestSpeedtest_Download(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	h := srv.Handler()
	cookie := authCookie(t, h)

	start := time.Now()
	w := doReq(t, h, "GET", "/api/speedtest/download?duration_ms=1200", nil, cookie)
	elapsed := time.Since(start)

	if w.Code != http.StatusOK {
		t.Fatalf("download status = %d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/octet-stream" {
		t.Errorf("download Content-Type = %q, want application/octet-stream", ct)
	}
	if cc := w.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Errorf("download Cache-Control = %q, want no-store", cc)
	}
	if ce := w.Header().Get("Content-Encoding"); ce != "identity" {
		t.Errorf("download Content-Encoding = %q, want identity (explicit no-compression)", ce)
	}
	if w.Body.Len() < speedtest.DownloadBlockSize {
		t.Errorf("download body = %d bytes, want at least one block (%d)", w.Body.Len(), speedtest.DownloadBlockSize)
	}
	if elapsed > 10*time.Second {
		t.Errorf("download elapsed %v, want bounded near requested duration", elapsed)
	}
}

// TestParseDownloadDuration 时长参数钳制:缺省/非法取默认,越界钳到 [Min, Max]。
func TestParseDownloadDuration(t *testing.T) {
	cases := []struct {
		query string
		want  time.Duration
	}{
		{"", speedtest.DefaultDownloadDuration},
		{"?duration_ms=abc", speedtest.DefaultDownloadDuration},
		{"?duration_ms=0", speedtest.MinDownloadDuration},
		{"?duration_ms=1", speedtest.MinDownloadDuration},
		{"?duration_ms=5000", 5 * time.Second},
		{"?duration_ms=999999999", speedtest.MaxDownloadDuration},
	}
	for _, c := range cases {
		req := httptest.NewRequest("GET", "/api/speedtest/download"+c.query, nil)
		if got := parseDownloadDuration(req); got != c.want {
			t.Errorf("parseDownloadDuration(%q) = %v, want %v", c.query, got, c.want)
		}
	}
}

// TestSpeedtest_Upload 上行收流:读丢弃计数,返回字节数。
func TestSpeedtest_Upload(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	h := srv.Handler()
	cookie := authCookie(t, h)

	payload := bytes.Repeat([]byte("u"), 100_000)
	req := httptest.NewRequest("POST", "/api/speedtest/upload", bytes.NewReader(payload))
	req.RemoteAddr = "10.0.0.1:1000"
	req.ContentLength = -1 // chunked,不设 ContentLength(照 streamUpload 策略)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("upload status = %d, want 200", w.Code)
	}
	var resp map[string]int64
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("upload response not JSON: %v", err)
	}
	if resp["bytes"] != 100_000 {
		t.Errorf("upload bytes = %d, want 100000", resp["bytes"])
	}
}

// TestSpeedtest_ResultsCRUD 落库 → 查询(全量/直连/按 key) → 删除 → 再删 404。
func TestSpeedtest_ResultsCRUD(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	h := srv.Handler()
	cookie := authCookie(t, h)

	save := func(nodeKey string) int64 {
		t.Helper()
		body, _ := json.Marshal(map[string]any{
			"node_key": nodeKey, "down_mbps": 88.5, "up_mbps": 20.25,
			"idle_latency_ms": 12.3, "jitter_ms": 1.2, "client_info": "test-agent",
		})
		w := doReq(t, h, "POST", "/api/speedtest/results", bytes.NewReader(body), cookie)
		if w.Code != http.StatusOK {
			t.Fatalf("save status = %d, want 200 (body: %s)", w.Code, w.Body.String())
		}
		var resp map[string]int64
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("save response not JSON: %v", err)
		}
		if resp["id"] == 0 {
			t.Fatal("save returned id 0")
		}
		return resp["id"]
	}

	directID := save("")
	save("1.2.3.4:8388")

	// 全量
	w := doReq(t, h, "GET", "/api/speedtest/results", nil, cookie)
	var all []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &all); err != nil {
		t.Fatalf("list all not JSON: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("list all len = %d, want 2", len(all))
	}
	// id DESC:后写在前;字段完整透出
	if all[0]["node_key"] != "1.2.3.4:8388" || all[0]["down_mbps"] != 88.5 ||
		all[0]["up_mbps"] != 20.25 || all[0]["idle_latency_ms"] != 12.3 ||
		all[0]["jitter_ms"] != 1.2 || all[0]["client_info"] != "test-agent" {
		t.Errorf("list entry mismatch: %+v", all[0])
	}

	// 直连桶(node_key 参数存在但为空)
	w = doReq(t, h, "GET", "/api/speedtest/results?node_key=", nil, cookie)
	var direct []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &direct); err != nil {
		t.Fatalf("list direct not JSON: %v", err)
	}
	if len(direct) != 1 || direct[0]["node_key"] != "" {
		t.Errorf("direct filter = %+v, want only the direct entry", direct)
	}

	// 具体 key
	w = doReq(t, h, "GET", "/api/speedtest/results?node_key=1.2.3.4:8388", nil, cookie)
	var keyed []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &keyed); err != nil {
		t.Fatalf("list keyed not JSON: %v", err)
	}
	if len(keyed) != 1 || keyed[0]["node_key"] != "1.2.3.4:8388" {
		t.Errorf("key filter = %+v, want only the keyed entry", keyed)
	}

	// 删除
	w = doReq(t, h, "DELETE", "/api/speedtest/results/"+strconv.FormatInt(directID, 10), nil, cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("delete status = %d, want 200", w.Code)
	}
	w = doReq(t, h, "DELETE", "/api/speedtest/results/"+strconv.FormatInt(directID, 10), nil, cookie)
	if w.Code != http.StatusNotFound {
		t.Errorf("re-delete status = %d, want 404", w.Code)
	}
}

// TestSpeedtest_SaveResultValidation 非法数值(负数)与坏 JSON 一律 400。
func TestSpeedtest_SaveResultValidation(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	h := srv.Handler()
	cookie := authCookie(t, h)

	cases := []string{
		`{bad json`,
		`{"down_mbps":-1}`,
		`{"up_mbps":-0.5}`,
		`{"idle_latency_ms":-3}`,
		`{"jitter_ms":-0.1}`,
	}
	for _, body := range cases {
		w := doReq(t, h, "POST", "/api/speedtest/results", strings.NewReader(body), cookie)
		if w.Code != http.StatusBadRequest {
			t.Errorf("save %s: status = %d, want 400", body, w.Code)
		}
	}

	// 空负载(全零值)合法:用户可能只测了下行
	w := doReq(t, h, "POST", "/api/speedtest/results", strings.NewReader(`{}`), cookie)
	if w.Code != http.StatusOK {
		t.Errorf("save empty payload: status = %d, want 200", w.Code)
	}
}
