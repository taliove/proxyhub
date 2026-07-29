package aggregator

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

// manualNodeFixture 手动机场节点 fixture(example.com 合成值)。
func manualNodeFixture(source string) *subscription.Node {
	return &subscription.Node{
		Name:     "手动 01",
		Type:     "trojan",
		Server:   "manual1.example.com",
		Port:     443,
		Password: "pw",
		Source:   source,
		Region:   "HK",
	}
}

// TestFullRefresh_ManualAirportNodesSurvive 核心回归(spec-manual-airport-import T4):
// 手动机场节点经全量刷新仍在架、不标 stale——全量刷新跳过手动机场,
// per-source 合并只扫描成功拉取的来源。
func TestFullRefresh_ManualAirportNodesSurvive(t *testing.T) {
	agg, st := newTestAggregator(t)

	if _, err := st.CreateManualAirportForUser(0, "手动机场"); err != nil {
		t.Fatalf("create manual airport: %v", err)
	}
	// 预置手动机场节点入池(内存 + DB 快照)
	manual := manualNodeFixture("手动机场")
	agg.SetNodesForUser(0, []*subscription.Node{manual})
	if err := st.SaveNodePool(agg.Nodes()); err != nil {
		t.Fatalf("SaveNodePool: %v", err)
	}

	// 拉取型机场:正常返回一个节点
	srv := subscriptionServer(t)
	if _, err := st.CreateAirport("拉取机场", srv.URL); err != nil {
		t.Fatalf("CreateAirport: %v", err)
	}

	if err := agg.RunOnce(context.Background(), store.RefreshTriggerManual); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	var found *subscription.Node
	for _, n := range agg.Nodes() {
		if n.Source == "手动机场" {
			found = n
		}
	}
	if found == nil {
		t.Fatal("manual airport node dropped by full refresh")
	}
	if found.Stale {
		t.Error("manual airport node marked stale by full refresh (MergePool stale 陷阱)")
	}

	// DB 快照同样保留且不 stale
	persisted, err := st.LoadNodePool()
	if err != nil {
		t.Fatalf("LoadNodePool: %v", err)
	}
	var dbFound *subscription.Node
	for _, n := range persisted {
		if n.Source == "手动机场" {
			dbFound = n
		}
	}
	if dbFound == nil || dbFound.Stale {
		t.Errorf("persisted manual node = %+v, want present and not stale", dbFound)
	}
}

// TestFullRefresh_SkipsManualAirportFetch 手动机场不发起任何拉取:
// 即使其 url 列不可达,刷新也成功、无失败诊断。
func TestFullRefresh_SkipsManualAirportFetch(t *testing.T) {
	agg, st := newTestAggregator(t)

	if _, err := st.CreateManualAirportForUser(0, "手动机场"); err != nil {
		t.Fatalf("create manual airport: %v", err)
	}
	srv := subscriptionServer(t)
	if _, err := st.CreateAirport("拉取机场", srv.URL); err != nil {
		t.Fatalf("CreateAirport: %v", err)
	}

	if err := agg.RunOnce(context.Background(), store.RefreshTriggerManual); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	runs, err := st.ListRefreshRuns(1)
	if err != nil || len(runs) == 0 {
		t.Fatalf("ListRefreshRuns: %v", err)
	}
	if runs[0].Status != store.RefreshStatusSuccess {
		t.Errorf("status = %s, want success (手动机场不应计入失败)", runs[0].Status)
	}
}

// TestRefresh_PersistsAirportUsage 拉取落库用量(spec-manual-airport-import T5):
// 带 subscription-userinfo 头的拉取成功后,airports 行带用量信息。
func TestRefresh_PersistsAirportUsage(t *testing.T) {
	agg, st := newTestAggregator(t)

	node := "trojan://pw@node1.example.com:443#HK 01"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("subscription-userinfo", "upload=100; download=200; total=1000; expire=1893456000")
		w.Header().Set("profile-web-page-url", "https://example.com")
		w.Write([]byte(base64.StdEncoding.EncodeToString([]byte(node))))
	}))
	t.Cleanup(srv.Close)

	ap, err := st.CreateAirport("拉取机场", srv.URL)
	if err != nil {
		t.Fatalf("CreateAirport: %v", err)
	}
	if err := agg.RunOnce(context.Background(), store.RefreshTriggerManual); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	got, err := st.GetAirportByID(ap.ID)
	if err != nil {
		t.Fatalf("GetAirportByID: %v", err)
	}
	if got.UsageUpload != 100 || got.UsageDownload != 200 || got.UsageTotal != 1000 || got.UsageExpire != 1893456000 {
		t.Errorf("usage = %d/%d/%d expire=%d, want 100/200/1000 expire=1893456000",
			got.UsageUpload, got.UsageDownload, got.UsageTotal, got.UsageExpire)
	}
	if got.WebPageURL != "https://example.com" {
		t.Errorf("WebPageURL = %q, want https://example.com", got.WebPageURL)
	}
}

// TestRefresh_NoUsageHeadersKeepsExisting 机场不报用量头时保留既有落库值(不清零)。
func TestRefresh_NoUsageHeadersKeepsExisting(t *testing.T) {
	agg, st := newTestAggregator(t)

	srv := subscriptionServer(t)
	ap, err := st.CreateAirport("拉取机场", srv.URL)
	if err != nil {
		t.Fatalf("CreateAirport: %v", err)
	}
	if err := st.UpdateAirportUsage(ap.ID, &subscription.UsageInfo{Upload: 1, Download: 2, Total: 100, WebPageURL: "https://example.com"}); err != nil {
		t.Fatalf("UpdateAirportUsage: %v", err)
	}

	if err := agg.RunOnce(context.Background(), store.RefreshTriggerManual); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	got, _ := st.GetAirportByID(ap.ID)
	if got.UsageTotal != 100 || got.WebPageURL != "https://example.com" {
		t.Errorf("usage wiped by header-less fetch: total=%d web=%q", got.UsageTotal, got.WebPageURL)
	}
}

// TestPurgeAirportNodes_ManualExempt 清空豁免手动机场节点(2026-07-29 决策):
// 手动机场无 URL 可拉,清空后永不回来;拉取型机场节点照常清除。
func TestPurgeAirportNodes_ManualExempt(t *testing.T) {
	agg, st := newTestAggregator(t)

	if _, err := st.CreateManualAirportForUser(0, "手动机场"); err != nil {
		t.Fatalf("create manual airport: %v", err)
	}
	if _, err := st.CreateAirport("拉取机场", "https://example.com/sub"); err != nil {
		t.Fatalf("CreateAirport: %v", err)
	}

	agg.SetNodesForUser(0, []*subscription.Node{
		manualNodeFixture("手动机场"),
		{Name: "拉取 01", Type: "trojan", Server: "url1.example.com", Port: 443, Password: "pw", Source: "拉取机场"},
		{Name: "自建 01", Type: "trojan", Server: "self1.example.com", Port: 443, Password: "pw", Source: subscription.SourceSelfHosted},
	})
	if err := st.SaveNodePool(agg.Nodes()); err != nil {
		t.Fatalf("SaveNodePool: %v", err)
	}

	removed, err := agg.PurgeAirportNodes()
	if err != nil {
		t.Fatalf("PurgeAirportNodes: %v", err)
	}
	if removed != 1 {
		t.Errorf("removed = %d, want 1 (仅拉取型机场节点)", removed)
	}

	remaining := agg.Nodes()
	if len(remaining) != 2 {
		t.Fatalf("pool = %d nodes, want 2 (手动 + 自建)", len(remaining))
	}
	sources := map[string]bool{}
	for _, n := range remaining {
		sources[n.Source] = true
	}
	if !sources["手动机场"] || !sources[subscription.SourceSelfHosted] {
		t.Errorf("remaining sources = %v, want 手动机场 + self-hosted", sources)
	}

	// DB 快照同步豁免
	persisted, err := st.LoadNodePool()
	if err != nil {
		t.Fatalf("LoadNodePool: %v", err)
	}
	if len(persisted) != 2 {
		t.Errorf("persisted pool = %d nodes, want 2 (DB 双清同步豁免)", len(persisted))
	}
}
