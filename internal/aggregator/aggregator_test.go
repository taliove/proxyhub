package aggregator

import (
	"context"
	"encoding/base64"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/taliove/proxyhub/internal/config"
	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

func newTestAggregator(t *testing.T) (*Aggregator, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { st.Close() })

	return newTestAggregatorWithStore(t, st), st
}

// newTestAggregatorWithStore 在既有 store 上新建 Aggregator(模拟进程重启后的状态)。
func newTestAggregatorWithStore(t *testing.T, st *store.Store) *Aggregator {
	t.Helper()
	cfg := &config.Config{}
	cfg.HealthCheck.Timeout.Latency = 200 * time.Millisecond
	cfg.HealthCheck.Timeout.Request = 200 * time.Millisecond
	cfg.HealthCheck.Concurrent = 2
	cfg.HealthCheck.LatencyThreshold = 10000
	cfg.Filter.Deduplicate = true

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(cfg, nil, st, logger)
}

// subscriptionServer 返回一个提供有效订阅（1 个 trojan 节点）的测试服务器
func subscriptionServer(t *testing.T) *httptest.Server {
	t.Helper()
	content := base64.StdEncoding.EncodeToString([]byte("trojan://pw@127.0.0.1:1#HK 01"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(content))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestRunOnce_Success_RecordsRunAndEvents(t *testing.T) {
	agg, st := newTestAggregator(t)
	srv := subscriptionServer(t)
	st.CreateAirport("测试机场", srv.URL)

	if err := agg.RunOnce(context.Background(), store.RefreshTriggerManual); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}

	runs, err := st.ListRefreshRuns(10)
	if err != nil {
		t.Fatalf("ListRefreshRuns() error = %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("len(runs) = %d, want 1", len(runs))
	}
	run := runs[0]
	if run.Trigger != store.RefreshTriggerManual {
		t.Errorf("Trigger = %s, want manual", run.Trigger)
	}
	if run.Status != store.RefreshStatusSuccess {
		t.Errorf("Status = %s, want success", run.Status)
	}
	if run.TotalNodes != 1 {
		t.Errorf("TotalNodes = %d, want 1", run.TotalNodes)
	}
	if run.FinishedAt == nil {
		t.Error("FinishedAt should be set")
	}

	events, err := st.ListRefreshEvents(run.ID)
	if err != nil {
		t.Fatalf("ListRefreshEvents() error = %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected events to be recorded")
	}

	stages := make(map[string]bool)
	var mentionsAirport bool
	for _, e := range events {
		stages[e.Stage] = true
		if strings.Contains(e.Message, "测试机场") {
			mentionsAirport = true
		}
	}
	// 刷新阶段只有 fetch/check/done, filter 已挪到订阅生成时执行
	for _, want := range []string{"fetch", "check", "done"} {
		if !stages[want] {
			t.Errorf("missing stage %q in events", want)
		}
	}
	if !mentionsAirport {
		t.Error("events should mention airport name")
	}
}

func TestRunOnce_PartialFailure(t *testing.T) {
	agg, st := newTestAggregator(t)
	srv := subscriptionServer(t)
	st.CreateAirport("好机场", srv.URL)
	st.CreateAirport("坏机场", "http://127.0.0.1:1/sub")

	if err := agg.RunOnce(context.Background(), store.RefreshTriggerScheduled); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}

	runs, _ := st.ListRefreshRuns(1)
	if runs[0].Status != store.RefreshStatusPartial {
		t.Errorf("Status = %s, want partial", runs[0].Status)
	}
	if runs[0].Error == "" {
		t.Error("Error summary should not be empty on partial failure")
	}

	events, _ := st.ListRefreshEvents(runs[0].ID)
	var hasWarn bool
	for _, e := range events {
		if e.Level == "warn" && strings.Contains(e.Message, "坏机场") {
			hasWarn = true
		}
	}
	if !hasWarn {
		t.Error("expected a warn event for the failed airport")
	}
}

func TestRunOnce_AllFailed(t *testing.T) {
	agg, st := newTestAggregator(t)
	st.CreateAirport("坏机场", "http://127.0.0.1:1/sub")

	if err := agg.RunOnce(context.Background(), store.RefreshTriggerStartup); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}

	runs, _ := st.ListRefreshRuns(1)
	if runs[0].Status != store.RefreshStatusFailed {
		t.Errorf("Status = %s, want failed", runs[0].Status)
	}
}

// TestRunOnce_AllFailed_PreservesPool 验证:当本轮所有机场拉取失败时,
// 不能清空已有节点池 —— 全量拉取失败意味着"本轮没有数据",而非"节点都挂了"。
func TestRunOnce_AllFailed_PreservesPool(t *testing.T) {
	agg, st := newTestAggregator(t)

	// 预置节点池(直接注入,避免依赖健康检查能否连通测试节点)
	agg.SetNodesForUser(0, []*subscription.Node{
		{Name: "HK 01", Type: "trojan", Server: "1.2.3.4", Port: 443, Available: true},
		{Name: "US 01", Type: "trojan", Server: "5.6.7.8", Port: 443, Available: true},
	})
	before := len(agg.Nodes())

	// 唯一的机场拉不通,模拟机场整体不可达 / 全量拉取失败
	if _, err := st.CreateAirport("坏机场", "http://127.0.0.1:1/sub"); err != nil {
		t.Fatalf("CreateAirport: %v", err)
	}

	if err := agg.RunOnce(context.Background(), store.RefreshTriggerScheduled); err != nil {
		t.Fatalf("RunOnce error = %v", err)
	}

	if got := len(agg.Nodes()); got != before {
		t.Errorf("pool wiped on total fetch failure: got %d nodes, want %d preserved", got, before)
	}

	runs, _ := st.ListRefreshRuns(1)
	if runs[0].Status != store.RefreshStatusFailed {
		t.Errorf("Status = %s, want failed", runs[0].Status)
	}
}

func TestAutoRefreshEnabled_DefaultsOn(t *testing.T) {
	agg, _ := newTestAggregator(t)
	if !agg.autoRefreshEnabled() {
		t.Error("scheduled refresh should default ON when the setting is absent")
	}
}

func TestAutoRefreshEnabled_RespectsSetting(t *testing.T) {
	agg, st := newTestAggregator(t)
	cases := []struct {
		val  string
		want bool
	}{
		{"true", true},
		{"false", false}, // 只有显式 "false" 关闭
		{"", true},       // 空值视为默认开
		{"yes", true},    // 非 "false" 一律视为开
	}
	for _, c := range cases {
		if err := st.SaveSystemSettings(map[string]string{"scheduled_refresh_enabled": c.val}); err != nil {
			t.Fatalf("SaveSystemSettings(%q): %v", c.val, err)
		}
		if got := agg.autoRefreshEnabled(); got != c.want {
			t.Errorf("autoRefreshEnabled() with %q = %v, want %v", c.val, got, c.want)
		}
	}
}

func TestRunOnce_ManualRunsEvenWhenScheduledDisabled(t *testing.T) {
	agg, st := newTestAggregator(t)
	srv := subscriptionServer(t)
	if _, err := st.CreateAirport("测试机场", srv.URL); err != nil {
		t.Fatalf("CreateAirport: %v", err)
	}
	if err := st.SaveSystemSettings(map[string]string{"scheduled_refresh_enabled": "false"}); err != nil {
		t.Fatalf("SaveSystemSettings: %v", err)
	}

	// 手动刷新不受“定时刷新”开关影响，始终执行整条流水线
	if err := agg.RunOnce(context.Background(), store.RefreshTriggerManual); err != nil {
		t.Fatalf("manual RunOnce error = %v", err)
	}
	// LastUpdate 由 execute() 在流水线跑完时写入，非零即证明手动刷新确实执行了
	// （节点是否留存取决于健康检查，与开关无关，故不以池是否为空判断）
	if agg.LastUpdate().IsZero() {
		t.Error("manual refresh must run the pipeline even when scheduled refresh is disabled")
	}

	runs, err := st.ListRefreshRuns(10)
	if err != nil {
		t.Fatalf("ListRefreshRuns: %v", err)
	}
	if len(runs) != 1 || runs[0].Trigger != store.RefreshTriggerManual {
		t.Errorf("expected exactly one manual refresh run, got %+v", runs)
	}
}

func TestRunOnce_PersistsNodePool(t *testing.T) {
	agg, st := newTestAggregator(t)
	srv := subscriptionServer(t)
	st.CreateAirport("测试机场", srv.URL)

	if err := agg.RunOnce(context.Background(), store.RefreshTriggerManual); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}

	// 成功刷新后，内存池应与库里持久化的快照一致
	mem := agg.Nodes()
	persisted, err := st.LoadNodePool()
	if err != nil {
		t.Fatalf("LoadNodePool() error = %v", err)
	}
	if len(persisted) != len(mem) {
		t.Fatalf("persisted pool = %d nodes, memory pool = %d", len(persisted), len(mem))
	}
	for i := range mem {
		if persisted[i].Name != mem[i].Name || persisted[i].Server != mem[i].Server {
			t.Errorf("persisted[%d] = %s@%s, want %s@%s",
				i, persisted[i].Name, persisted[i].Server, mem[i].Name, mem[i].Server)
		}
	}
}

func TestNew_RestoresNodePoolFromSnapshot(t *testing.T) {
	// 先用 store 持久化一份快照，模拟上一次成功刷新的结果
	_, st := newTestAggregator(t)
	snapshot := []*subscription.Node{
		{Name: "HK 01", Type: "trojan", Server: "1.2.3.4", Port: 443, Password: "p", TLS: true, Available: true, Latency: 60},
		{Name: "JP 01", Type: "ss", Server: "5.6.7.8", Port: 8388, Cipher: "aes-256-gcm", Password: "p", Available: true, Latency: 90},
	}
	if err := st.SaveNodePool(snapshot); err != nil {
		t.Fatalf("SaveNodePool() error = %v", err)
	}

	// 新聚合器复用同一个 store（模拟进程重启），New 应回填内存池
	cfg := &config.Config{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	agg := New(cfg, nil, st, logger)

	got := agg.Nodes()
	if len(got) != len(snapshot) {
		t.Fatalf("restored pool = %d nodes, want %d (restart lost nodes)", len(got), len(snapshot))
	}
	if got[0].Name != "HK 01" || got[1].Name != "JP 01" {
		t.Errorf("restored pool order/content wrong: %s, %s", got[0].Name, got[1].Name)
	}
}
