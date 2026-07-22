package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/taliove/proxyhub/internal/airporttest"
	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

// newEndpointTestServer 包装 newTestServer 并给正延迟阈值(生产配置要求 >0;
// 零值阈值会把带延迟的 fixture 节点全部过滤掉)。
func newEndpointTestServer(t *testing.T, nodes []*subscription.Node) (*Server, *store.Store) {
	t.Helper()
	srv, st := newTestServer(t, nodes)
	srv.cfg.HealthCheck.LatencyThreshold = 1000
	return srv, st
}

// endpointTestPool 两个可用机场节点(HK ss + JP vmess)。
// fixture 纪律:example.com + 全零 UUID,无任何真实凭证。
func endpointTestPool() []*subscription.Node {
	return []*subscription.Node{
		{Name: "香港A", Type: "ss", Server: "a.example.com", Port: 8388, Cipher: "aes-256-gcm", Password: "p", Available: true, Latency: 100, Region: "HK", Source: "机场甲"},
		{Name: "日本B", Type: "vmess", Server: "b.example.com", Port: 443, UUID: "00000000-0000-0000-0000-000000000000", Available: true, Latency: 200, Region: "JP", Source: "机场甲"},
	}
}

// stubProbeChecker 桩检活:全部可用,延迟按序号递增(50,51,...),避免真实网络。
type stubProbeChecker struct{}

func (stubProbeChecker) CheckAll(_ context.Context, nodes []*subscription.Node) []*airporttest.HealthCheckResult {
	results := make([]*airporttest.HealthCheckResult, 0, len(nodes))
	for i, n := range nodes {
		results = append(results, &airporttest.HealthCheckResult{Node: n, Available: true, Latency: 50 + i})
	}
	return results
}

// doEndpointRequest 带 session cookie 走完整路由(PathValue 依赖 mux)。
func doEndpointRequest(t *testing.T, h http.Handler, cookie *http.Cookie, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// endpointTestView 拉取验证 + 池快照响应的测试视图。
type endpointTestView struct {
	Pull map[string]struct {
		Valid      bool   `json:"valid"`
		NodeCount  int    `json:"node_count"`
		DurationMs int64  `json:"duration_ms"`
		Error      string `json:"error"`
	} `json:"pull"`
	Snapshot struct {
		Total       int      `json:"total"`
		Available   int      `json:"available"`
		MeanLatency float64  `json:"mean_latency_ms"`
		RegionCount int      `json:"region_count"`
		Regions     []string `json:"regions"`
	} `json:"snapshot"`
}

func decodeEndpointTestView(t *testing.T, w *httptest.ResponseRecorder) endpointTestView {
	t.Helper()
	var resp endpointTestView
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse test response: %v (body %s)", err, w.Body.String())
	}
	return resp
}

func TestHandleEndpointTest_PullAndSnapshot(t *testing.T) {
	srv, st := newEndpointTestServer(t, endpointTestPool())
	h := srv.Handler()
	cookie := authCookie(t, h)
	ep, _ := st.CreateEndpoint("diag")

	w := doEndpointRequest(t, h, cookie, http.MethodPost, fmt.Sprintf("/api/endpoints/%d/test", ep.ID), "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	resp := decodeEndpointTestView(t, w)

	clash := resp.Pull["clash"]
	if !clash.Valid || clash.NodeCount != 2 {
		t.Errorf("clash = valid %v count %d err %q, want valid/2", clash.Valid, clash.NodeCount, clash.Error)
	}
	v2ray := resp.Pull["v2ray"]
	if !v2ray.Valid || v2ray.NodeCount != 2 {
		t.Errorf("v2ray = valid %v count %d err %q, want valid/2", v2ray.Valid, v2ray.NodeCount, v2ray.Error)
	}

	if resp.Snapshot.Total != 2 || resp.Snapshot.Available != 2 {
		t.Errorf("snapshot = %d/%d, want 2/2", resp.Snapshot.Available, resp.Snapshot.Total)
	}
	if resp.Snapshot.MeanLatency != 150 {
		t.Errorf("mean_latency = %v, want 150 (mean over available nodes)", resp.Snapshot.MeanLatency)
	}
	if resp.Snapshot.RegionCount != 2 {
		t.Errorf("region_count = %d, want 2 (HK+JP)", resp.Snapshot.RegionCount)
	}

	// 拉取验证不记 pull_logs(ADR 0027 决策 1:统计只反映真实客户端拉取)
	stats, err := st.EndpointStats(ep.ID)
	if err != nil {
		t.Fatalf("EndpointStats: %v", err)
	}
	if len(stats) != 0 {
		t.Errorf("test pull must not record pull_logs, got %d rows", len(stats))
	}
}

func TestHandleEndpointTest_NotFound(t *testing.T) {
	srv, _ := newEndpointTestServer(t, endpointTestPool())
	h := srv.Handler()
	cookie := authCookie(t, h)

	w := doEndpointRequest(t, h, cookie, http.MethodPost, "/api/endpoints/99999/test", "")
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// 禁用态可测(ADR 0027 决策 4):测"如果启用会下发什么",不被启停拦截。
func TestHandleEndpointTest_DisabledEndpoint(t *testing.T) {
	srv, st := newEndpointTestServer(t, endpointTestPool())
	h := srv.Handler()
	cookie := authCookie(t, h)
	ep, _ := st.CreateEndpoint("disabled")
	if err := st.SetEndpointEnabled(ep.ID, false); err != nil {
		t.Fatalf("SetEndpointEnabled: %v", err)
	}

	w := doEndpointRequest(t, h, cookie, http.MethodPost, fmt.Sprintf("/api/endpoints/%d/test", ep.ID), "")
	if w.Code != http.StatusOK {
		t.Fatalf("disabled endpoint must be testable, status = %d, body = %s", w.Code, w.Body.String())
	}
	resp := decodeEndpointTestView(t, w)
	if !resp.Pull["clash"].Valid || resp.Pull["clash"].NodeCount != 2 {
		t.Errorf("disabled endpoint pull = %+v, want valid/2", resp.Pull["clash"])
	}
}

func TestHandleEndpointTest_EmptyPool(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	cookie := authCookie(t, h)
	ep, _ := st.CreateEndpoint("empty")

	w := doEndpointRequest(t, h, cookie, http.MethodPost, fmt.Sprintf("/api/endpoints/%d/test", ep.ID), "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	resp := decodeEndpointTestView(t, w)
	for _, format := range []string{"clash", "v2ray"} {
		got := resp.Pull[format]
		if got.Valid || got.NodeCount != 0 || got.Error == "" {
			t.Errorf("%s = %+v, want invalid/0 with reason on empty pool", format, got)
		}
	}
	if resp.Snapshot.Total != 0 || resp.Snapshot.RegionCount != 0 {
		t.Errorf("snapshot = %+v, want zeros on empty pool", resp.Snapshot)
	}
}

func TestHandleEndpointTest_ConditionsRespected(t *testing.T) {
	srv, st := newEndpointTestServer(t, endpointTestPool())
	h := srv.Handler()
	cookie := authCookie(t, h)
	ep, _ := st.CreateEndpoint("hk")
	setConditions(t, srv, ep.ID, `{"regions":["HK"]}`)

	w := doEndpointRequest(t, h, cookie, http.MethodPost, fmt.Sprintf("/api/endpoints/%d/test", ep.ID), "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	resp := decodeEndpointTestView(t, w)
	if resp.Pull["clash"].NodeCount != 1 || resp.Pull["v2ray"].NodeCount != 1 {
		t.Errorf("node counts = clash %d v2ray %d, want 1/1 (HK only)",
			resp.Pull["clash"].NodeCount, resp.Pull["v2ray"].NodeCount)
	}
	if resp.Snapshot.Total != 1 || resp.Snapshot.RegionCount != 1 {
		t.Errorf("snapshot = %+v, want total 1 region 1", resp.Snapshot)
	}
}

func TestHandleEndpointTest_InvalidTemplate(t *testing.T) {
	srv, st := newEndpointTestServer(t, endpointTestPool())
	h := srv.Handler()
	cookie := authCookie(t, h)
	ep, _ := st.CreateEndpoint("badtmpl")
	if err := st.SetClashTemplate("just a scalar, not a mapping"); err != nil {
		t.Fatalf("SetClashTemplate: %v", err)
	}

	w := doEndpointRequest(t, h, cookie, http.MethodPost, fmt.Sprintf("/api/endpoints/%d/test", ep.ID), "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	resp := decodeEndpointTestView(t, w)
	clash := resp.Pull["clash"]
	if clash.Valid || clash.Error == "" {
		t.Errorf("clash = %+v, want invalid with reason on broken template", clash)
	}
	if !resp.Pull["v2ray"].Valid {
		t.Errorf("v2ray = %+v, want still valid (independent of clash template)", resp.Pull["v2ray"])
	}
}

// 池快照的可用 x/y 不是恒等 y/y:自建节点豁免可用性过滤,不可用时会拉低 x。
func TestHandleEndpointTest_UnavailableSelfHostedCounts(t *testing.T) {
	pool := append(endpointTestPool(), &subscription.Node{
		Name: "自建", Type: "ss", Server: "self.example.com", Port: 8388,
		Cipher: "aes-256-gcm", Password: "p", Available: false,
		Source: subscription.SourceSelfHosted,
	})
	srv, st := newEndpointTestServer(t, pool)
	h := srv.Handler()
	cookie := authCookie(t, h)
	if err := st.CreateSelfHostedNode(&store.SelfHostedNode{
		Name: "自建", Protocol: "ss", Server: "self.example.com", Port: 8388,
		Cipher: "aes-256-gcm", Password: "p", Enabled: true,
	}); err != nil {
		t.Fatalf("CreateSelfHostedNode: %v", err)
	}
	ep, _ := st.CreateEndpoint("with-self")

	w := doEndpointRequest(t, h, cookie, http.MethodPost, fmt.Sprintf("/api/endpoints/%d/test", ep.ID), "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	resp := decodeEndpointTestView(t, w)
	if resp.Snapshot.Total != 3 || resp.Snapshot.Available != 2 {
		t.Errorf("snapshot = %d/%d, want 2/3 (unavailable self-hosted still deliverable)",
			resp.Snapshot.Available, resp.Snapshot.Total)
	}
}

// probeRunView 实测轮询响应的测试视图。
type probeRunView struct {
	RunID      string `json:"run_id"`
	EndpointID int64  `json:"endpoint_id"`
	Full       bool   `json:"full"`
	Status     string `json:"status"`
	Total      int    `json:"total"`
	Sampled    int    `json:"sampled"`
	Checked    int    `json:"checked"`
	Error      string `json:"error"`
}

func decodeProbeRunView(t *testing.T, w *httptest.ResponseRecorder) probeRunView {
	t.Helper()
	var resp probeRunView
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse probe run: %v (body %s)", err, w.Body.String())
	}
	return resp
}

// pollProbeRun 轮询实测 run 到终态(completed/failed),超时失败。
func pollProbeRun(t *testing.T, h http.Handler, cookie *http.Cookie, endpointID int64, runID string) probeRunView {
	t.Helper()
	path := fmt.Sprintf("/api/endpoints/%d/test/probe/%s", endpointID, runID)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		w := doEndpointRequest(t, h, cookie, http.MethodGet, path, "")
		if w.Code != http.StatusOK {
			t.Fatalf("poll status = %d, body = %s", w.Code, w.Body.String())
		}
		run := decodeProbeRunView(t, w)
		if run.Status == probeRunCompleted || run.Status == probeRunFailed {
			return run
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("run %s did not reach terminal state within 5s", runID)
	return probeRunView{}
}

func TestHandleEndpointTestProbe_SampledRun(t *testing.T) {
	srv, st := newEndpointTestServer(t, endpointTestPool())
	srv.probeChecker = stubProbeChecker{}
	h := srv.Handler()
	cookie := authCookie(t, h)
	ep, _ := st.CreateEndpoint("probe")

	w := doEndpointRequest(t, h, cookie, http.MethodPost, fmt.Sprintf("/api/endpoints/%d/test/probe", ep.ID), `{"full":false}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	created := decodeProbeRunView(t, w)
	if created.RunID == "" {
		t.Fatal("response must carry run_id handle")
	}
	if created.Total != 2 {
		t.Errorf("total = %d, want 2 deliverable nodes", created.Total)
	}

	final := pollProbeRun(t, h, cookie, ep.ID, created.RunID)
	if final.Status != probeRunCompleted {
		t.Fatalf("final status = %q (err %q), want completed", final.Status, final.Error)
	}
	if final.Sampled != 2 || final.Checked != 2 {
		t.Errorf("progress = checked %d sampled %d, want 2/2", final.Checked, final.Sampled)
	}

	// 实测写回池(与健康检查同语义):Available/Latency 按桩结果更新
	latencyByServer := map[string]int{}
	for _, n := range srv.nodes.Nodes() {
		if !n.Available {
			t.Errorf("node %s must be available after probe writeback", n.Server)
		}
		latencyByServer[n.Server] = n.Latency
	}
	if latencyByServer["a.example.com"] != 50 || latencyByServer["b.example.com"] != 51 {
		t.Errorf("writeback latencies = %v, want a=50 b=51", latencyByServer)
	}
}

func TestHandleEndpointTestProbe_FullFlagEchoed(t *testing.T) {
	srv, st := newEndpointTestServer(t, endpointTestPool())
	srv.probeChecker = stubProbeChecker{}
	h := srv.Handler()
	cookie := authCookie(t, h)
	ep, _ := st.CreateEndpoint("probe-full")

	w := doEndpointRequest(t, h, cookie, http.MethodPost, fmt.Sprintf("/api/endpoints/%d/test/probe", ep.ID), `{"full":true}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	created := decodeProbeRunView(t, w)
	if !created.Full {
		t.Error("full flag must be echoed on the run handle")
	}
	pollProbeRun(t, h, cookie, ep.ID, created.RunID)
}

// 禁用态实测(ADR 0027 决策 4):与拉取验证对称,不被启停拦截。
func TestHandleEndpointTestProbe_DisabledEndpoint(t *testing.T) {
	srv, st := newEndpointTestServer(t, endpointTestPool())
	srv.probeChecker = stubProbeChecker{}
	h := srv.Handler()
	cookie := authCookie(t, h)
	ep, _ := st.CreateEndpoint("probe-disabled")
	if err := st.SetEndpointEnabled(ep.ID, false); err != nil {
		t.Fatalf("SetEndpointEnabled: %v", err)
	}

	w := doEndpointRequest(t, h, cookie, http.MethodPost, fmt.Sprintf("/api/endpoints/%d/test/probe", ep.ID), `{"full":false}`)
	if w.Code != http.StatusOK {
		t.Fatalf("disabled endpoint must be probeable, status = %d, body = %s", w.Code, w.Body.String())
	}
	created := decodeProbeRunView(t, w)
	pollProbeRun(t, h, cookie, ep.ID, created.RunID)
}

func TestHandleEndpointTestProbe_NotFound(t *testing.T) {
	srv, _ := newEndpointTestServer(t, endpointTestPool())
	h := srv.Handler()
	cookie := authCookie(t, h)

	w := doEndpointRequest(t, h, cookie, http.MethodPost, "/api/endpoints/99999/test/probe", `{"full":false}`)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestHandleEndpointTestProbe_EmptyPool(t *testing.T) {
	srv, st := newTestServer(t, nil)
	h := srv.Handler()
	cookie := authCookie(t, h)
	ep, _ := st.CreateEndpoint("probe-empty")

	w := doEndpointRequest(t, h, cookie, http.MethodPost, fmt.Sprintf("/api/endpoints/%d/test/probe", ep.ID), `{"full":false}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (nothing deliverable to probe)", w.Code)
	}
}

// 轮询不存在的 run(含服务重启后内存态丢失)返回 404,前端据此提示重跑。
func TestHandleGetEndpointTestProbe_UnknownRun(t *testing.T) {
	srv, st := newEndpointTestServer(t, endpointTestPool())
	h := srv.Handler()
	cookie := authCookie(t, h)
	ep, _ := st.CreateEndpoint("probe-404")

	w := doEndpointRequest(t, h, cookie, http.MethodGet, fmt.Sprintf("/api/endpoints/%d/test/probe/no-such-run", ep.ID), "")
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 for unknown run", w.Code)
	}
}

func TestHandleGetEndpointTestProbe_WrongEndpoint(t *testing.T) {
	srv, st := newEndpointTestServer(t, endpointTestPool())
	srv.probeChecker = stubProbeChecker{}
	h := srv.Handler()
	cookie := authCookie(t, h)
	ep1, _ := st.CreateEndpoint("owner")
	ep2, _ := st.CreateEndpoint("other")

	w := doEndpointRequest(t, h, cookie, http.MethodPost, fmt.Sprintf("/api/endpoints/%d/test/probe", ep1.ID), `{"full":false}`)
	created := decodeProbeRunView(t, w)

	w = doEndpointRequest(t, h, cookie, http.MethodGet, fmt.Sprintf("/api/endpoints/%d/test/probe/%s", ep2.ID, created.RunID), "")
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 when run belongs to another endpoint", w.Code)
	}
	pollProbeRun(t, h, cookie, ep1.ID, created.RunID)
}

func TestHandleListEndpoints_Availability(t *testing.T) {
	srv, st := newEndpointTestServer(t, endpointTestPool())
	h := srv.Handler()
	cookie := authCookie(t, h)
	epAll, _ := st.CreateEndpoint("all")
	epHK, _ := st.CreateEndpoint("hk")
	setConditions(t, srv, epHK.ID, `{"regions":["HK"]}`)

	w := doEndpointRequest(t, h, cookie, http.MethodGet, "/api/endpoints", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var items []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &items); err != nil {
		t.Fatalf("parse list: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d endpoints, want 2", len(items))
	}

	byAlias := map[string]map[string]any{}
	for _, item := range items {
		byAlias[item["alias"].(string)] = item
	}

	assertAvailability := func(alias string, wantAvail, wantTotal int) {
		t.Helper()
		item, ok := byAlias[alias]
		if !ok {
			t.Fatalf("endpoint %q missing from list", alias)
		}
		avail, ok := item["availability"].(map[string]any)
		if !ok {
			t.Fatalf("endpoint %q missing availability field", alias)
		}
		if int(avail["available"].(float64)) != wantAvail || int(avail["total"].(float64)) != wantTotal {
			t.Errorf("endpoint %q availability = %v/%v, want %d/%d",
				alias, avail["available"], avail["total"], wantAvail, wantTotal)
		}
	}
	assertAvailability("all", 2, 2)
	assertAvailability("hk", 1, 1)

	// 既有字段不动(加性变更,不破坏既有消费方)
	all := byAlias["all"]
	for _, field := range []string{"id", "alias", "path", "token", "enabled", "created_at"} {
		if _, ok := all[field]; !ok {
			t.Errorf("existing field %q missing from list item", field)
		}
	}
	if all["path"] != epAll.Path {
		t.Errorf("path = %v, want %v", all["path"], epAll.Path)
	}
}

// snapshotDeliverable 纯函数口径:可用数、均值只算可用节点,地区去非空去重排序。
func TestSnapshotDeliverable(t *testing.T) {
	nodes := []*subscription.Node{
		{Name: "a", Available: true, Latency: 100, Region: "HK"},
		{Name: "b", Available: true, Latency: 300, Region: "HK"},
		{Name: "c", Available: false, Latency: 999, Region: "US"},
		{Name: "d", Available: true, Latency: 200, Region: ""},
	}
	snap := snapshotDeliverable(nodes)
	if snap.Total != 4 || snap.Available != 3 {
		t.Errorf("snapshot = %d/%d, want 3/4", snap.Available, snap.Total)
	}
	if snap.MeanLatency != 200 {
		t.Errorf("mean = %v, want 200 (available only)", snap.MeanLatency)
	}
	if snap.RegionCount != 2 {
		t.Errorf("region_count = %d, want 2 (HK/US; empty skipped)", snap.RegionCount)
	}
	if len(snap.Regions) != 2 || snap.Regions[0] != "HK" || snap.Regions[1] != "US" {
		t.Errorf("regions = %v, want [HK US] sorted", snap.Regions)
	}

	empty := snapshotDeliverable(nil)
	if empty.Total != 0 || empty.MeanLatency != 0 || empty.RegionCount != 0 || len(empty.Regions) != 0 {
		t.Errorf("empty snapshot = %+v, want zeros", empty)
	}
}

func TestValidateClashContent(t *testing.T) {
	if _, err := validateClashContent([]byte(":\tnot yaml")); err == nil {
		t.Error("invalid yaml must be rejected")
	}
	count, err := validateClashContent([]byte("proxies:\n  - {name: a}\n  - {name: b}\n"))
	if err != nil || count != 2 {
		t.Errorf("valid clash = count %d err %v, want 2/nil", count, err)
	}
}

func TestValidateV2RayContent(t *testing.T) {
	if _, err := validateV2RayContent([]byte("!!!not-base64!!!")); err == nil {
		t.Error("invalid base64 must be rejected")
	}
	if _, err := validateV2RayContent([]byte("")); err == nil {
		t.Error("empty content must be rejected")
	}
	// "ss://x\nvmess://y\n" 的 base64
	count, err := validateV2RayContent([]byte("c3M6Ly94CnZtZXNzOi8veQo="))
	if err != nil || count != 2 {
		t.Errorf("valid v2ray = count %d err %v, want 2/nil", count, err)
	}
}
