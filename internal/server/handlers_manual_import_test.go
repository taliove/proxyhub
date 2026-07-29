package server

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/taliove/proxyhub/internal/aggregator"
	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

// 手动机场导入 fixture(example.com + 全零 UUID,凭证红线:合成值)。
const (
	importNodeSS     = "ss://YWVzLTI1Ni1nY206cGFzc3dvcmQ@node1.example.com:8388#HK 01"
	importNodeVless  = "vless://00000000-0000-0000-0000-000000000000@node2.example.com:443?security=tls#JP 01"
	importNodeDup    = "ss://YWVzLTI1Ni1nY206b3RoZXJwYXNz@node1.example.com:8388#HK 01-dup"
	importBrokenLine = "not-a-share-link"
)

// createManualAirport 经创建接口建手动机场,返回机场 id。
func createManualAirport(t *testing.T, s *Server, name string) int64 {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"name": name, "source_type": "manual"})
	req := httptest.NewRequest(http.MethodPost, "/airports", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleCreateAirport(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("create manual airport: status %d: %s", w.Code, w.Body.String())
	}
	var resp store.Airport
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	return resp.ID
}

func TestCreateAirport_ManualSourceType(t *testing.T) {
	s, _ := newTestServer(t, nil)

	body, _ := json.Marshal(map[string]any{
		"name":            "手动机场",
		"source_type":     "manual",
		"url":             "https://should-be-ignored.example.com/sub",
		"usage_remaining": 800,
		"usage_total":     1000,
		"usage_expire":    1893456000,
		"web_page_url":    "https://example.com",
	})
	req := httptest.NewRequest(http.MethodPost, "/airports", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleCreateAirport(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}

	var resp store.Airport
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.SourceType != store.AirportSourceManual {
		t.Errorf("SourceType = %q, want manual", resp.SourceType)
	}
	if resp.URL != "" {
		t.Errorf("URL = %q, want empty (载荷中的 url 被忽略)", resp.URL)
	}

	persisted, err := s.st.GetAirportByID(resp.ID)
	if err != nil {
		t.Fatalf("GetAirportByID: %v", err)
	}
	if persisted.UsageDownload != 200 || persisted.UsageTotal != 1000 || persisted.UsageExpire != 1893456000 {
		t.Errorf("usage = download %d total %d expire %d, want 200/1000/1893456000 (剩余 800 换算已用 200)",
			persisted.UsageDownload, persisted.UsageTotal, persisted.UsageExpire)
	}
	if persisted.WebPageURL != "https://example.com" {
		t.Errorf("WebPageURL = %q", persisted.WebPageURL)
	}
}

func TestCreateAirport_InvalidSourceType(t *testing.T) {
	s, _ := newTestServer(t, nil)
	body, _ := json.Marshal(map[string]any{"name": "x", "source_type": "bogus"})
	req := httptest.NewRequest(http.MethodPost, "/airports", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleCreateAirport(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestImportAirport_Success(t *testing.T) {
	s, _ := newTestServer(t, nil)
	id := createManualAirport(t, s, "手动机场")

	// base64 整段形态:2 条有效(其中 1 条同 NodeKey 重复,后条覆盖)+ 1 条坏行
	content := strings.Join([]string{importNodeSS, importBrokenLine, importNodeVless, importNodeDup}, "\n")
	payload, _ := json.Marshal(map[string]any{
		"content":         base64.StdEncoding.EncodeToString([]byte(content)),
		"usage_remaining": 500,
		"usage_total":     1000,
	})
	req := httptest.NewRequest(http.MethodPost, "/airports/1/import", bytes.NewReader(payload))
	req.SetPathValue("id", formatID(id))
	w := httptest.NewRecorder()
	s.handleImportAirport(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Imported int `json:"imported"`
		Failures []struct {
			Line   int    `json:"line"`
			Reason string `json:"reason"`
		} `json:"failures"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Imported != 2 {
		t.Errorf("imported = %d, want 2 (3 有效行 - 1 同 key 重复)", resp.Imported)
	}
	if len(resp.Failures) != 1 || resp.Failures[0].Line != 2 {
		t.Errorf("failures = %+v, want [{line:2}]", resp.Failures)
	}

	// 节点入池(fakeNodes 追加语义),来源标注机场名
	fakes := s.nodes.(*fakeNodes)
	var got int
	for _, n := range fakes.nodes {
		if n.Source == "手动机场" {
			got++
		}
	}
	if got != 2 {
		t.Errorf("pool nodes for 手动机场 = %d, want 2", got)
	}
	// 后条覆盖前条:同 NodeKey 保留 dup 行的密码
	for _, n := range fakes.nodes {
		if n.Server == "node1.example.com" && n.Password != "otherpass" {
			t.Errorf("dup node password = %q, want otherpass (后条覆盖前条)", n.Password)
		}
	}

	// 用量同贴落库
	ap, _ := s.st.GetAirportByID(id)
	if ap.UsageDownload != 500 || ap.UsageTotal != 1000 {
		t.Errorf("usage = %d/%d, want 500/1000", ap.UsageDownload, ap.UsageTotal)
	}
}

func TestImportAirport_PlaintextContent(t *testing.T) {
	s, _ := newTestServer(t, nil)
	id := createManualAirport(t, s, "手动机场")

	payload, _ := json.Marshal(map[string]any{"content": importNodeSS + "\n" + importNodeVless})
	req := httptest.NewRequest(http.MethodPost, "/airports/1/import", bytes.NewReader(payload))
	req.SetPathValue("id", formatID(id))
	w := httptest.NewRecorder()
	s.handleImportAirport(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Imported int `json:"imported"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Imported != 2 {
		t.Errorf("imported = %d, want 2 (明文多行形态)", resp.Imported)
	}
}

// TestImportAirport_URLAirportUsageIgnored 拉取型机场随贴用量字段被忽略(Check MEDIUM):
// URL 机场用量由响应头自动捕获,随贴不覆写(既不报错也不落库)。
func TestImportAirport_URLAirportUsageIgnored(t *testing.T) {
	s, st := newTestServer(t, nil)
	ap, err := st.CreateAirport("拉取机场", "https://example.com/sub")
	if err != nil {
		t.Fatalf("CreateAirport: %v", err)
	}
	// 预置捕获的用量(模拟响应头落库值)
	if err := st.UpdateAirportUsage(ap.ID, &subscription.UsageInfo{Upload: 1, Download: 2, Total: 100}); err != nil {
		t.Fatalf("UpdateAirportUsage: %v", err)
	}

	payload, _ := json.Marshal(map[string]any{
		"content":         importNodeSS,
		"usage_remaining": 999,
		"usage_total":     9999,
	})
	req := httptest.NewRequest(http.MethodPost, "/airports/1/import", bytes.NewReader(payload))
	req.SetPathValue("id", formatID(ap.ID))
	w := httptest.NewRecorder()
	s.handleImportAirport(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}

	got, _ := st.GetAirportByID(ap.ID)
	if got.UsageTotal != 100 || got.UsageDownload != 2 {
		t.Errorf("usage overwritten by import payload: total=%d download=%d, want 100/2 (随贴忽略)",
			got.UsageTotal, got.UsageDownload)
	}
}

// TestImportAirport_URLAirportAllowed 拉取型机场同样可粘贴导入(2026-07-29 拍板):
// 一次性导入,同一 upsert 语义;下次 URL 刷新成功自然覆盖回来。
func TestImportAirport_URLAirportAllowed(t *testing.T) {
	s, _ := newTestServer(t, nil)
	ap, err := s.st.CreateAirport("拉取机场", "https://example.com/sub")
	if err != nil {
		t.Fatalf("CreateAirport: %v", err)
	}
	payload, _ := json.Marshal(map[string]any{"content": importNodeSS + "\n" + importNodeVless})
	req := httptest.NewRequest(http.MethodPost, "/airports/1/import", bytes.NewReader(payload))
	req.SetPathValue("id", formatID(ap.ID))
	w := httptest.NewRecorder()
	s.handleImportAirport(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s (URL 机场粘贴导入应放行)", w.Code, w.Body.String())
	}
	var resp struct {
		Imported int `json:"imported"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Imported != 2 {
		t.Errorf("imported = %d, want 2", resp.Imported)
	}
	var got int
	for _, n := range s.nodes.(*fakeNodes).nodes {
		if n.Source == "拉取机场" {
			got++
		}
	}
	if got != 2 {
		t.Errorf("pool nodes for 拉取机场 = %d, want 2", got)
	}
}

func TestImportAirport_EmptyAndNoValidNodes(t *testing.T) {
	s, _ := newTestServer(t, nil)
	id := createManualAirport(t, s, "手动机场")

	// 空内容
	payload, _ := json.Marshal(map[string]any{"content": "   "})
	req := httptest.NewRequest(http.MethodPost, "/airports/1/import", bytes.NewReader(payload))
	req.SetPathValue("id", formatID(id))
	w := httptest.NewRecorder()
	s.handleImportAirport(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("empty content: status = %d, want 400", w.Code)
	}

	// 全部行解析失败:400 且带逐行失败明细
	payload, _ = json.Marshal(map[string]any{"content": importBrokenLine})
	req = httptest.NewRequest(http.MethodPost, "/airports/1/import", bytes.NewReader(payload))
	req.SetPathValue("id", formatID(id))
	w = httptest.NewRecorder()
	s.handleImportAirport(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("no valid nodes: status = %d, want 400", w.Code)
	}
	var resp struct {
		Imported int `json:"imported"`
		Failures []struct {
			Line int `json:"line"`
		} `json:"failures"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Imported != 0 || len(resp.Failures) != 1 {
		t.Errorf("resp = %+v, want imported=0 failures=1", resp)
	}
}

func TestImportAirport_TooLarge(t *testing.T) {
	s, _ := newTestServer(t, nil)
	id := createManualAirport(t, s, "手动机场")

	// 合法 JSON 前缀 + 超限 content:解码走到 MaxBytesReader 上限才失败(纯垃圾首字节
	// 会先撞 JSON 语法错误,测不到 413 分支)。
	big := `{"content":"` + strings.Repeat("a", manualImportMaxBytes) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/airports/1/import", strings.NewReader(big))
	req.SetPathValue("id", formatID(id))
	w := httptest.NewRecorder()
	s.handleImportAirport(w, req)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", w.Code)
	}
}

func TestImportAirport_Conflict(t *testing.T) {
	s, _ := newTestServer(t, nil)
	id := createManualAirport(t, s, "手动机场")
	s.nodes.(*fakeNodes).importErr = aggregator.ErrRefreshConflict

	payload, _ := json.Marshal(map[string]any{
		"content":         importNodeSS,
		"usage_remaining": 500,
		"usage_total":     1000,
	})
	req := httptest.NewRequest(http.MethodPost, "/airports/1/import", bytes.NewReader(payload))
	req.SetPathValue("id", formatID(id))
	w := httptest.NewRecorder()
	s.handleImportAirport(w, req)
	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409 (与进行中刷新/机场测试互斥)", w.Code)
	}

	// L3:冲突时用量不得被覆写(入池先于写用量)
	ap, _ := s.st.GetAirportByID(id)
	if ap.UsageTotal != 0 {
		t.Errorf("usage overwritten on conflict: total = %d, want 0 (未提供过用量)", ap.UsageTotal)
	}
}

// TestImportAirport_OtherUserAirport 跨用户导入他人手动机场:404 不暴露存在性(Check L2)。
func TestImportAirport_OtherUserAirport(t *testing.T) {
	s, st := newTestServer(t, nil)
	ap, err := st.CreateManualAirportForUser(1, "owner的手动机场")
	if err != nil {
		t.Fatalf("create manual: %v", err)
	}

	payload, _ := json.Marshal(map[string]any{"content": importNodeSS})
	req := httptest.NewRequest(http.MethodPost, "/airports/1/import", bytes.NewReader(payload))
	req.SetPathValue("id", formatID(ap.ID))
	req = req.WithContext(ContextWithUserScope(req.Context(), UserScope{UserID: 2, Role: store.RoleUser}))
	w := httptest.NewRecorder()
	s.handleImportAirport(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 (他人机场不暴露存在性)", w.Code)
	}
	if got := len(s.nodes.(*fakeNodes).nodes); got != 0 {
		t.Errorf("pool mutated by cross-user import: %d nodes", got)
	}
}

func TestAirportRefresh_ManualRejected(t *testing.T) {
	s, _ := newTestServer(t, nil)
	id := createManualAirport(t, s, "手动机场")

	req := httptest.NewRequest(http.MethodPost, "/airports/1/refresh", nil)
	req.SetPathValue("id", formatID(id))
	w := httptest.NewRecorder()
	s.handleAirportRefresh(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (手动机场的刷新语义是重新粘贴)", w.Code)
	}
}

func TestUpdateAirport_ManualUsageEdit(t *testing.T) {
	s, _ := newTestServer(t, nil)
	id := createManualAirport(t, s, "手动机场")

	body, _ := json.Marshal(map[string]any{
		"name":            "手动机场",
		"url":             "https://ignored.example.com/sub",
		"usage_remaining": 900,
		"usage_total":     1000,
		"web_page_url":    "https://example.com",
	})
	req := httptest.NewRequest(http.MethodPut, "/airports/1", bytes.NewReader(body))
	req.SetPathValue("id", formatID(id))
	w := httptest.NewRecorder()
	s.handleUpdateAirport(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}

	ap, _ := s.st.GetAirportByID(id)
	if ap.URL != "" {
		t.Errorf("URL = %q, want empty (编辑忽略载荷 url)", ap.URL)
	}
	if ap.UsageDownload != 100 || ap.UsageTotal != 1000 || ap.WebPageURL != "https://example.com" {
		t.Errorf("usage = %d/%d web %q, want 100/1000 web set", ap.UsageDownload, ap.UsageTotal, ap.WebPageURL)
	}
}

// formatID 测试内 int64 → 字符串(path value 设置)。
func formatID(id int64) string {
	return strconv.FormatInt(id, 10)
}
