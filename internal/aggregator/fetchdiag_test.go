package aggregator

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/taliove/proxyhub/internal/store"
)

// diagSubscriptionServer 返回状态与内容可定制的订阅服务器(ticket 0018)。
// content 为原始文本(非 base64),便于混合有效/无效行。
func diagSubscriptionServer(t *testing.T, status int, content string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		w.Write([]byte(base64.StdEncoding.EncodeToString([]byte(content))))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// 全量刷新拉取环节:每个机场落一行结构化诊断,成功带解析统计,失败带 HTTP 状态与错误。
func TestFetchAirports_WritesFetchDiags(t *testing.T) {
	agg, st := newTestAggregator(t)

	good := diagSubscriptionServer(t, http.StatusOK, strings.Join([]string{
		"trojan://pw@example.com:443#HK node1",
		"trojan://pw@example.com:444#US node2",
		"broken-line",
	}, "\n"))
	bad := diagSubscriptionServer(t, http.StatusServiceUnavailable, "")

	apGood, err := st.CreateAirport("机场好", good.URL)
	if err != nil {
		t.Fatalf("CreateAirport() error = %v", err)
	}
	apBad, err := st.CreateAirport("机场坏", bad.URL)
	if err != nil {
		t.Fatalf("CreateAirport() error = %v", err)
	}

	run, err := st.CreateRefreshRun(store.RefreshTriggerManual, 0)
	if err != nil {
		t.Fatalf("CreateRefreshRun() error = %v", err)
	}
	rl := &runLog{st: st, logger: agg.logger, runID: run.ID}

	if _, err := agg.fetchAirports(context.Background(), rl, nil); err != nil {
		t.Fatalf("fetchAirports() error = %v", err)
	}

	diags, err := st.ListRefreshFetchDiags(run.ID)
	if err != nil {
		t.Fatalf("ListRefreshFetchDiags() error = %v", err)
	}
	if len(diags) != 2 {
		t.Fatalf("len(diags) = %d, want 2", len(diags))
	}

	byAirport := make(map[string]*store.RefreshFetchDiag, len(diags))
	for _, d := range diags {
		byAirport[d.Airport] = d
	}

	g := byAirport["机场好"]
	if g == nil {
		t.Fatal("missing diag for 机场好")
	}
	if g.HTTPStatus != 200 || g.NodeCount != 2 || g.ParseFailures != 1 || g.Error != "" {
		t.Errorf("good diag = %+v, want 200/2 nodes/1 failure/no error", g)
	}
	if g.AirportID != apGood.ID {
		t.Errorf("good diag AirportID = %d, want %d", g.AirportID, apGood.ID)
	}

	b := byAirport["机场坏"]
	if b == nil {
		t.Fatal("missing diag for 机场坏")
	}
	if b.HTTPStatus != 503 || b.NodeCount != 0 || b.Error == "" {
		t.Errorf("bad diag = %+v, want 503/0 nodes/error", b)
	}
	if b.AirportID != apBad.ID {
		t.Errorf("bad diag AirportID = %d, want %d", b.AirportID, apBad.ID)
	}
}

// 单机场刷新路径(runSingle)同样采集诊断,不只全量。
func TestRunSingle_WritesFetchDiag(t *testing.T) {
	agg, st := newTestAggregator(t)

	srv := diagSubscriptionServer(t, http.StatusOK, strings.Join([]string{
		"trojan://pw@example.com:443#HK node1",
		"broken-line",
	}, "\n"))
	ap, err := st.CreateAirport("单机场", srv.URL)
	if err != nil {
		t.Fatalf("CreateAirport() error = %v", err)
	}

	jobID, _, started, err := agg.StartAirportRefreshJob(store.RefreshTriggerManual, ap.ID)
	if err != nil || !started {
		t.Fatalf("StartAirportRefreshJob() started = %v, err = %v", started, err)
	}
	waitJobStatus(t, st, jobID)
	run := waitRefreshRun(t, st, jobID)

	diags, err := st.ListRefreshFetchDiags(run.ID)
	if err != nil {
		t.Fatalf("ListRefreshFetchDiags() error = %v", err)
	}
	if len(diags) != 1 {
		t.Fatalf("len(diags) = %d, want 1", len(diags))
	}
	d := diags[0]
	if d.Airport != "单机场" || d.HTTPStatus != 200 || d.NodeCount != 1 || d.ParseFailures != 1 || d.Error != "" {
		t.Errorf("diag = %+v, want 单机场/200/1 node/1 failure/no error", d)
	}
}

// 单机场拉取失败:诊断行带 HTTP 状态与错误,run 记 failed。
func TestRunSingle_FetchFailureWritesDiag(t *testing.T) {
	agg, st := newTestAggregator(t)

	srv := diagSubscriptionServer(t, http.StatusUnauthorized, "")
	ap, err := st.CreateAirport("鉴权失败机场", srv.URL)
	if err != nil {
		t.Fatalf("CreateAirport() error = %v", err)
	}

	jobID, _, started, err := agg.StartAirportRefreshJob(store.RefreshTriggerManual, ap.ID)
	if err != nil || !started {
		t.Fatalf("StartAirportRefreshJob() started = %v, err = %v", started, err)
	}
	waitJobStatus(t, st, jobID)
	run := waitRefreshRun(t, st, jobID)

	diags, err := st.ListRefreshFetchDiags(run.ID)
	if err != nil {
		t.Fatalf("ListRefreshFetchDiags() error = %v", err)
	}
	if len(diags) != 1 {
		t.Fatalf("len(diags) = %d, want 1", len(diags))
	}
	d := diags[0]
	if d.HTTPStatus != 401 || d.NodeCount != 0 || d.Error == "" {
		t.Errorf("diag = %+v, want 401/0 nodes/error", d)
	}
}
