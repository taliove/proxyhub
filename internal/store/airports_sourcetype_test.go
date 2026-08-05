package store

import (
	"testing"

	"github.com/taliove/proxyhub/internal/subscription"
)

// TestCreateAirport_SourceType 来源类型:拉取型默认 'url';手动机场 url 空串 + 'manual'。
func TestCreateAirport_SourceType(t *testing.T) {
	s := newTestStore(t)

	urlAirport, err := s.CreateAirport("拉取机场", "https://example.com/sub")
	if err != nil {
		t.Fatalf("CreateAirport() error = %v", err)
	}
	manual, err := s.CreateManualAirportForUser(0, "手动机场")
	if err != nil {
		t.Fatalf("CreateManualAirportForUser() error = %v", err)
	}

	gotURL, err := s.GetAirportByID(urlAirport.ID)
	if err != nil {
		t.Fatalf("GetAirportByID(url) error = %v", err)
	}
	if gotURL.SourceType != AirportSourceURL {
		t.Errorf("url airport SourceType = %q, want %q", gotURL.SourceType, AirportSourceURL)
	}

	gotManual, err := s.GetAirportByID(manual.ID)
	if err != nil {
		t.Fatalf("GetAirportByID(manual) error = %v", err)
	}
	if gotManual.SourceType != AirportSourceManual {
		t.Errorf("manual airport SourceType = %q, want %q", gotManual.SourceType, AirportSourceManual)
	}
	if gotManual.URL != "" {
		t.Errorf("manual airport URL = %q, want empty", gotManual.URL)
	}

	// 列表扫描同样带出来源类型
	listed, err := s.ListAirports()
	if err != nil {
		t.Fatalf("ListAirports() error = %v", err)
	}
	byID := make(map[int64]*Airport, len(listed))
	for _, a := range listed {
		byID[a.ID] = a
	}
	if byID[manual.ID].SourceType != AirportSourceManual {
		t.Errorf("listed manual SourceType = %q, want %q", byID[manual.ID].SourceType, AirportSourceManual)
	}
}

// TestUpdateAirportUsage 用量信息覆盖更新;官网空时保留既有值(响应头缺官网不抹手填)。
func TestUpdateAirportUsage(t *testing.T) {
	s := newTestStore(t)
	a, err := s.CreateAirport("用量机场", "https://example.com/sub")
	if err != nil {
		t.Fatalf("CreateAirport() error = %v", err)
	}

	first := &subscription.UsageInfo{Upload: 100, Download: 200, Total: 1000, Expire: 1893456000, WebPageURL: "https://example.com"}
	if err := s.UpdateAirportUsage(a.ID, first); err != nil {
		t.Fatalf("UpdateAirportUsage(first) error = %v", err)
	}
	got, _ := s.GetAirportByID(a.ID)
	if got.UsageUpload != 100 || got.UsageDownload != 200 || got.UsageTotal != 1000 || got.UsageExpire != 1893456000 {
		t.Errorf("usage = %+v, want upload=100 download=200 total=1000 expire=1893456000", got)
	}
	if got.WebPageURL != "https://example.com" {
		t.Errorf("WebPageURL = %q, want https://example.com", got.WebPageURL)
	}

	// 第二次捕获无官网头:流量覆盖,官网保留
	second := &subscription.UsageInfo{Upload: 300, Download: 400, Total: 1000, Expire: 1893456000}
	if err := s.UpdateAirportUsage(a.ID, second); err != nil {
		t.Fatalf("UpdateAirportUsage(second) error = %v", err)
	}
	got, _ = s.GetAirportByID(a.ID)
	if got.UsageUpload != 300 || got.UsageDownload != 400 {
		t.Errorf("usage after second = upload %d download %d, want 300/400", got.UsageUpload, got.UsageDownload)
	}
	if got.WebPageURL != "https://example.com" {
		t.Errorf("WebPageURL wiped by header-less update: got %q", got.WebPageURL)
	}

	if err := s.UpdateAirportUsage(a.ID, nil); err != nil {
		t.Errorf("UpdateAirportUsage(nil) error = %v, want nil-op", err)
	}
}

// TestManualAirportNames 手动机场名集合按属主过滤(清空豁免的来源匹配键)。
func TestManualAirportNames(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateManualAirportForUser(7, "手动A"); err != nil {
		t.Fatalf("create manual A: %v", err)
	}
	if _, err := s.CreateManualAirportForUser(8, "手动B"); err != nil {
		t.Fatalf("create manual B: %v", err)
	}
	if _, err := s.CreateAirportForUser(7, "拉取C", "https://example.com/c"); err != nil {
		t.Fatalf("create url C: %v", err)
	}

	all, err := s.ManualAirportNames(0)
	if err != nil {
		t.Fatalf("ManualAirportNames(0) error = %v", err)
	}
	if !all["手动A"] || !all["手动B"] || all["拉取C"] {
		t.Errorf("ManualAirportNames(0) = %v, want {手动A, 手动B}", all)
	}

	only7, err := s.ManualAirportNames(7)
	if err != nil {
		t.Fatalf("ManualAirportNames(7) error = %v", err)
	}
	if !only7["手动A"] || only7["手动B"] {
		t.Errorf("ManualAirportNames(7) = %v, want {手动A}", only7)
	}
}

// TestSetAirportUsageForUser 手动填写路径:按属主全量覆写(空官网 = 显式清空);
// 行属他人 ErrNotFound。
func TestSetAirportUsageForUser(t *testing.T) {
	s := newTestStore(t)
	a, err := s.CreateManualAirportForUser(7, "手动A")
	if err != nil {
		t.Fatalf("create manual: %v", err)
	}

	usage := &subscription.UsageInfo{Download: 500, Total: 1000, WebPageURL: "https://example.com"}
	if err := s.SetAirportUsageForUser(8, a.ID, usage); err != ErrNotFound {
		t.Errorf("SetAirportUsageForUser(other) error = %v, want ErrNotFound", err)
	}
	if err := s.SetAirportUsageForUser(7, a.ID, usage); err != nil {
		t.Fatalf("SetAirportUsageForUser(owner) error = %v", err)
	}
	got, _ := s.GetAirportByID(a.ID)
	if got.UsageDownload != 500 || got.UsageTotal != 1000 || got.WebPageURL != "https://example.com" {
		t.Errorf("usage = %+v, want download=500 total=1000 web set", got)
	}

	// 显式清空:空官网覆写,不保留(区别于拉取路径的 UpdateAirportUsage)
	if err := s.SetAirportUsageForUser(7, a.ID, &subscription.UsageInfo{}); err != nil {
		t.Fatalf("SetAirportUsageForUser(clear) error = %v", err)
	}
	got, _ = s.GetAirportByID(a.ID)
	if got.WebPageURL != "" || got.UsageTotal != 0 {
		t.Errorf("clear = web %q total %d, want empty/0 (显式清空不保留)", got.WebPageURL, got.UsageTotal)
	}
}

// TestSetAirportUsageForUser_WebPageSchemeWhitelist 手填路径同样过 scheme 白名单
// (Check H2:javascript: 等非 http(s) 官网入库即归空串)。
func TestSetAirportUsageForUser_WebPageSchemeWhitelist(t *testing.T) {
	s := newTestStore(t)
	a, err := s.CreateManualAirportForUser(0, "手动A")
	if err != nil {
		t.Fatalf("create manual: %v", err)
	}

	if err := s.SetAirportUsageForUser(0, a.ID, &subscription.UsageInfo{WebPageURL: "javascript:alert(1)"}); err != nil {
		t.Fatalf("SetAirportUsageForUser: %v", err)
	}
	got, _ := s.GetAirportByID(a.ID)
	if got.WebPageURL != "" {
		t.Errorf("WebPageURL = %q, want empty (非 http/https 归空串)", got.WebPageURL)
	}

	// 拉取路径(UpdateAirportUsage)同一防线
	if err := s.UpdateAirportUsage(a.ID, &subscription.UsageInfo{Total: 100, WebPageURL: "data:text/html,x"}); err != nil {
		t.Fatalf("UpdateAirportUsage: %v", err)
	}
	got, _ = s.GetAirportByID(a.ID)
	if got.WebPageURL != "" {
		t.Errorf("fetch path WebPageURL = %q, want empty", got.WebPageURL)
	}
}

// TestSetAirportWebPageURLForUser 拉取型机场官网手填:只动 web_page_url,
// 用量列(响应头捕获)不被触碰;行属他人 ErrNotFound;非 http/https 归空串。
func TestSetAirportWebPageURLForUser(t *testing.T) {
	s := newTestStore(t)
	a, err := s.CreateAirportForUser(7, "拉取A", "https://example.com/sub")
	if err != nil {
		t.Fatalf("create url airport: %v", err)
	}
	// 模拟响应头捕获的用量
	if err := s.UpdateAirportUsage(a.ID, &subscription.UsageInfo{Download: 500, Total: 1000}); err != nil {
		t.Fatalf("seed usage: %v", err)
	}

	if err := s.SetAirportWebPageURLForUser(8, a.ID, "https://example.com"); err != ErrNotFound {
		t.Errorf("SetAirportWebPageURLForUser(other) error = %v, want ErrNotFound", err)
	}
	if err := s.SetAirportWebPageURLForUser(7, a.ID, "https://example.com"); err != nil {
		t.Fatalf("SetAirportWebPageURLForUser(owner) error = %v", err)
	}
	got, _ := s.GetAirportByID(a.ID)
	if got.WebPageURL != "https://example.com" {
		t.Errorf("WebPageURL = %q, want set", got.WebPageURL)
	}
	if got.UsageDownload != 500 || got.UsageTotal != 1000 {
		t.Errorf("usage = %d/%d, want 500/1000 (用量列不被官网覆写触碰)", got.UsageDownload, got.UsageTotal)
	}

	// 显式清空
	if err := s.SetAirportWebPageURLForUser(7, a.ID, ""); err != nil {
		t.Fatalf("clear error = %v", err)
	}
	got, _ = s.GetAirportByID(a.ID)
	if got.WebPageURL != "" {
		t.Errorf("after clear WebPageURL = %q, want empty", got.WebPageURL)
	}

	// scheme 白名单
	if err := s.SetAirportWebPageURLForUser(7, a.ID, "javascript:alert(1)"); err != nil {
		t.Fatalf("sanitize error = %v", err)
	}
	got, _ = s.GetAirportByID(a.ID)
	if got.WebPageURL != "" {
		t.Errorf("sanitize WebPageURL = %q, want empty", got.WebPageURL)
	}
}
