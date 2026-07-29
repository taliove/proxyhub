package aggregator

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/taliove/proxyhub/internal/jobs"
	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

// gatedSubscriptionServer 返回可闸门控制的订阅服务器:close(release) 前请求一直阻塞。
func gatedSubscriptionServer(t *testing.T, release <-chan struct{}) *httptest.Server {
	t.Helper()
	content := base64.StdEncoding.EncodeToString([]byte("trojan://pw@127.0.0.1:1#HK 01"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		w.Write([]byte(content))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// waitJobStatus 轮询 jobs 表直到任务到达终态。
func waitJobStatus(t *testing.T, st *store.Store, jobID int64) jobs.Status {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		rec, err := st.Jobs().Get(jobID)
		if err == nil && rec.Status != jobs.StatusRunning {
			return rec.Status
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("job %d did not reach terminal state in time", jobID)
	return ""
}

// waitRefreshRun 轮询 refresh_runs 直到出现关联 jobID 的记录(runLog 在 Run goroutine 里异步创建)。
func waitRefreshRun(t *testing.T, st *store.Store, jobID int64) *store.RefreshRun {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		runs, err := st.ListRefreshRuns(10)
		if err == nil {
			for _, r := range runs {
				if r.JobID == jobID {
					return r
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no refresh run linked to job %d", jobID)
	return nil
}

func TestStartRefreshJob_AttachesAndLinksRun(t *testing.T) {
	agg, st := newTestAggregator(t)
	release := make(chan struct{})
	srv := gatedSubscriptionServer(t, release)
	if _, err := st.CreateAirport("慢机场", srv.URL); err != nil {
		t.Fatalf("CreateAirport() error = %v", err)
	}

	jobID, key, started, err := agg.StartRefreshJob(store.RefreshTriggerManual)
	if err != nil {
		t.Fatalf("StartRefreshJob() error = %v", err)
	}
	if jobID == 0 || key != "all" || !started {
		t.Fatalf("got jobID=%d key=%q started=%v, want non-zero/all/true", jobID, key, started)
	}

	// 同 key 重复触发:附加到进行中任务,不报错、不新建
	jobID2, _, started2, err := agg.StartRefreshJob(store.RefreshTriggerManual)
	if err != nil {
		t.Fatalf("second StartRefreshJob() error = %v", err)
	}
	if started2 || jobID2 != jobID {
		t.Errorf("second trigger: started=%v jobID=%d, want attach to %d", started2, jobID2, jobID)
	}

	// params 记录 trigger
	rec, err := st.Jobs().Get(jobID)
	if err != nil {
		t.Fatalf("Jobs().Get() error = %v", err)
	}
	var p RefreshJobParams
	if err := json.Unmarshal(rec.Params, &p); err != nil {
		t.Fatalf("unmarshal params: %v", err)
	}
	if p.Trigger != store.RefreshTriggerManual {
		t.Errorf("params.trigger = %q, want manual", p.Trigger)
	}

	// refresh_runs 回填 job_id(异步创建,轮询等待)
	run := waitRefreshRun(t, st, jobID)

	close(release)
	if status := waitJobStatus(t, st, jobID); status != jobs.StatusDone {
		t.Errorf("job status = %s, want done", status)
	}

	deadline := time.Now().Add(3 * time.Second)
	for run.Status == store.RefreshStatusRunning && time.Now().Before(deadline) {
		run, _ = st.GetRefreshRun(run.ID)
		time.Sleep(20 * time.Millisecond)
	}
	if run.Status != store.RefreshStatusSuccess {
		t.Errorf("refresh run status = %s, want success", run.Status)
	}

	// 完成后可再次触发(新任务)
	_, _, started3, err := agg.StartRefreshJob(store.RefreshTriggerManual)
	if err != nil || !started3 {
		t.Errorf("trigger after finish: started=%v err=%v, want new job", started3, err)
	}
}

func TestStartRefreshJob_AirportLevelConflict(t *testing.T) {
	agg, st := newTestAggregator(t)
	release := make(chan struct{})
	defer close(release)
	srv := gatedSubscriptionServer(t, release)
	airport, err := st.CreateAirport("慢机场", srv.URL)
	if err != nil {
		t.Fatalf("CreateAirport() error = %v", err)
	}

	// 全量进行中:单机场与全量互斥
	if _, _, _, err := agg.StartRefreshJob(store.RefreshTriggerManual); err != nil {
		t.Fatalf("StartRefreshJob() error = %v", err)
	}
	if _, _, _, err := agg.startRefresh(0, store.RefreshTriggerManual, airport.ID); !errors.Is(err, ErrRefreshConflict) {
		t.Errorf("single-airport during full: err = %v, want ErrRefreshConflict", err)
	}
	// 全量 vs 全量不算冲突(同 key 附加)
	if _, _, _, err := agg.StartRefreshJob(store.RefreshTriggerScheduled); err != nil {
		t.Errorf("duplicate full refresh should attach, err = %v", err)
	}
}

func TestCancelRefresh_InterruptsAndKeepsPartial(t *testing.T) {
	agg, st := newTestAggregator(t)
	release := make(chan struct{})
	srv := gatedSubscriptionServer(t, release)
	if _, err := st.CreateAirport("慢机场", srv.URL); err != nil {
		t.Fatalf("CreateAirport() error = %v", err)
	}

	jobID, key, _, err := agg.StartRefreshJob(store.RefreshTriggerManual)
	if err != nil {
		t.Fatalf("StartRefreshJob() error = %v", err)
	}
	run := waitRefreshRun(t, st, jobID)

	if !agg.CancelRefresh(key) {
		t.Fatal("CancelRefresh() = false, want true (job running)")
	}
	close(release) // 放行在途拉取,让任务收口

	if status := waitJobStatus(t, st, jobID); status != jobs.StatusCancelled {
		t.Errorf("job status = %s, want cancelled", status)
	}

	deadline := time.Now().Add(3 * time.Second)
	for run.Status == store.RefreshStatusRunning && time.Now().Before(deadline) {
		run, _ = st.GetRefreshRun(run.ID)
		time.Sleep(20 * time.Millisecond)
	}
	if run.Status != store.RefreshStatusCancelled {
		t.Errorf("refresh run status = %s, want cancelled", run.Status)
	}

	// 取消不回滚:已拉取部分照常入池(1 个节点 + 自建 0)
	if got := len(agg.Nodes()); got != 1 {
		t.Errorf("pool size = %d, want 1 (partial fetch kept)", got)
	}
}

func TestRefreshJob_InterruptedOnRestart(t *testing.T) {
	agg, st := newTestAggregator(t)
	release := make(chan struct{})
	defer close(release)
	srv := gatedSubscriptionServer(t, release)
	if _, err := st.CreateAirport("慢机场", srv.URL); err != nil {
		t.Fatalf("CreateAirport() error = %v", err)
	}

	jobID, _, _, err := agg.StartRefreshJob(store.RefreshTriggerManual)
	if err != nil {
		t.Fatalf("StartRefreshJob() error = %v", err)
	}

	// 模拟重启:新建 Aggregator(新 Manager),遗留 running 的 refresh 任务应被标 interrupted,
	// 且不能误标其他 kind(写一条别的 kind 的 running 记录验证 RecoverOwn 的隔离性)
	otherID, err := st.Jobs().Insert("exam", "node-1", nil)
	if err != nil {
		t.Fatalf("Jobs().Insert() error = %v", err)
	}

	agg2 := newTestAggregatorWithStore(t, st)
	_ = agg2

	rec, err := st.Jobs().Get(jobID)
	if err != nil {
		t.Fatalf("Jobs().Get() error = %v", err)
	}
	if rec.Status != jobs.StatusInterrupted {
		t.Errorf("refresh job status = %s, want interrupted after restart", rec.Status)
	}

	other, err := st.Jobs().Get(otherID)
	if err != nil {
		t.Fatalf("Jobs().Get(other) error = %v", err)
	}
	if other.Status != jobs.StatusRunning {
		t.Errorf("other kind job status = %s, want running (RecoverOwn must not touch foreign kinds)", other.Status)
	}
}

func TestStartAirportRefreshJob_FetchOnlyNoHealthCheck(t *testing.T) {
	agg, st := newTestAggregator(t)
	release := make(chan struct{})
	close(release) // 不阻塞
	srv := gatedSubscriptionServer(t, release)
	airport, err := st.CreateAirport("目标机场", srv.URL)
	if err != nil {
		t.Fatalf("CreateAirport() error = %v", err)
	}

	jobID, key, started, err := agg.StartAirportRefreshJob(store.RefreshTriggerManual, airport.ID)
	if err != nil {
		t.Fatalf("StartAirportRefreshJob() error = %v", err)
	}
	if !started || key != "airport-1" {
		t.Fatalf("got key=%q started=%v, want airport-1/true", key, started)
	}

	if status := waitJobStatus(t, st, jobID); status != jobs.StatusDone {
		t.Fatalf("job status = %s, want done", status)
	}

	// 节点入池但不跑健康检查:Latency 保持 0(未检测),Available 默认 false
	nodes := agg.Nodes()
	if len(nodes) != 1 {
		t.Fatalf("pool size = %d, want 1", len(nodes))
	}
	if nodes[0].Source != "目标机场" {
		t.Errorf("node source = %q, want 目标机场", nodes[0].Source)
	}
	if nodes[0].Latency != 0 || nodes[0].Available {
		t.Errorf("health check should not run: latency=%d available=%v, want 0/false",
			nodes[0].Latency, nodes[0].Available)
	}

	// refresh_runs 成功且关联 job
	run := waitRefreshRun(t, st, jobID)
	deadline := time.Now().Add(3 * time.Second)
	for run.Status == store.RefreshStatusRunning && time.Now().Before(deadline) {
		run, _ = st.GetRefreshRun(run.ID)
		time.Sleep(20 * time.Millisecond)
	}
	if run.Status != store.RefreshStatusSuccess {
		t.Errorf("refresh run status = %s, want success", run.Status)
	}
}

func TestStartAirportRefreshJob_ParallelDifferentAirports(t *testing.T) {
	agg, st := newTestAggregator(t)

	releaseA := make(chan struct{})
	releaseB := make(chan struct{})
	srvA := gatedSubscriptionServerNamed(t, releaseA, "127.0.0.1:1", "A 01")
	srvB := gatedSubscriptionServerNamed(t, releaseB, "127.0.0.1:2", "B 01")
	apA, err := st.CreateAirport("机场A", srvA.URL)
	if err != nil {
		t.Fatalf("CreateAirport(A) error = %v", err)
	}
	apB, err := st.CreateAirport("机场B", srvB.URL)
	if err != nil {
		t.Fatalf("CreateAirport(B) error = %v", err)
	}

	jobA, _, _, err := agg.StartAirportRefreshJob(store.RefreshTriggerManual, apA.ID)
	if err != nil {
		t.Fatalf("start A: %v", err)
	}
	jobB, _, _, err := agg.StartAirportRefreshJob(store.RefreshTriggerManual, apB.ID)
	if err != nil {
		t.Fatalf("start B: %v (different airports must run in parallel)", err)
	}
	// 同机场重复触发:附加不冲突
	if _, _, started, err := agg.StartAirportRefreshJob(store.RefreshTriggerManual, apA.ID); err != nil || started {
		t.Errorf("duplicate A: started=%v err=%v, want attach", started, err)
	}
	// 全量与单机场互斥
	if _, _, _, err := agg.StartRefreshJob(store.RefreshTriggerManual); !errors.Is(err, ErrRefreshConflict) {
		t.Errorf("full during single-airport: err = %v, want ErrRefreshConflict", err)
	}

	close(releaseA)
	close(releaseB)
	if status := waitJobStatus(t, st, jobA); status != jobs.StatusDone {
		t.Errorf("jobA status = %s, want done", status)
	}
	if status := waitJobStatus(t, st, jobB); status != jobs.StatusDone {
		t.Errorf("jobB status = %s, want done", status)
	}

	// 两个机场的节点都在池中(并行写不丢更新)
	sources := map[string]bool{}
	for _, n := range agg.Nodes() {
		sources[n.Source] = true
	}
	if !sources["机场A"] || !sources["机场B"] {
		t.Errorf("pool sources = %v, want both 机场A and 机场B", sources)
	}
}

// gatedSubscriptionServerNamed 同 gatedSubscriptionServer,server/节点名可定制。
// 注意两个机场必须用不同 server:port,否则 NodeKey 相同互相覆盖。
func gatedSubscriptionServerNamed(t *testing.T, release <-chan struct{}, addr, marker string) *httptest.Server {
	t.Helper()
	content := base64.StdEncoding.EncodeToString([]byte("trojan://pw@" + addr + "#HK " + marker))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		w.Write([]byte(content))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestMergePartialOnCancel_UnfetchedAirportsNotMarkedStale 回归测试:取消全量刷新时,
// 未拉取(被取消)的机场节点不得标 stale(曾整池 stale 导致订阅空池)。
// 直接单测 mergePartialOnCancel(并发时序在集成层不可控,见开发记录)。
func TestMergePartialOnCancel_UnfetchedAirportsNotMarkedStale(t *testing.T) {
	agg, st := newTestAggregator(t)

	// 预置:机场X/机场Y 的旧节点都在池中。X 的旧节点与 X 订阅返回的同 key(carry-forward);
	// Y 的旧节点端口与 Y 订阅返回的不同——若 Y 被错误并入 MergePool 它必 stale。
	oldX := &subscription.Node{Name: "X 01", Type: "trojan", Server: "127.0.0.1", Port: 1, Password: "pw", Region: "HK", Source: "机场X"}
	oldY := &subscription.Node{Name: "Y old", Type: "trojan", Server: "127.0.0.1", Port: 5, Password: "pw", Region: "HK", Source: "机场Y"}
	if err := st.SaveNodePool([]*subscription.Node{oldX, oldY}); err != nil {
		t.Fatalf("SaveNodePool() error = %v", err)
	}
	agg.restoreNodePool()

	// 构造取消时刻的拉取结果:X 拉成功,Y 未启动(skipped,airportNodes[Y]=nil,
	// Y 是现存启用机场,进 preserve 集——取消 ≠ 机场消失)
	newX := &subscription.Node{Name: "X 01", Type: "trojan", Server: "127.0.0.1", Port: 1, Password: "pw", Region: "HK", Source: "机场X"}
	fetched := &fetchResult{
		airportNodes: map[string][]*subscription.Node{
			"机场X": {newX},
			"机场Y": nil,
		},
		allNodes: []*subscription.Node{newX},
		enabled:  2,
		preserve: map[string]bool{"机场Y": true},
	}

	agg.mergePartialOnCancel(&runLog{}, fetched)

	// Y(未拉取):旧节点原样保留且不 stale(取消 ≠ 机场消失)
	var nodeY *subscription.Node
	var nodeX *subscription.Node
	for _, n := range agg.Nodes() {
		switch n.Source {
		case "机场Y":
			nodeY = n
		case "机场X":
			nodeX = n
		}
	}
	if nodeY == nil {
		t.Fatal("机场Y old node missing from pool after cancel")
	}
	if nodeY.Stale {
		t.Error("机场Y node marked stale after cancel, want preserved active")
	}
	if nodeX == nil || nodeX.Stale {
		t.Errorf("机场X node = %+v, want present and active (fetched)", nodeX)
	}

	// 持久化同样正确(下轮 restore 读到的也是不 stale 的 Y)
	reloaded, err := st.LoadNodePool()
	if err != nil {
		t.Fatalf("LoadNodePool() error = %v", err)
	}
	for _, n := range reloaded {
		if n.Source == "机场Y" && n.Stale {
			t.Error("机场Y node stale in DB after cancel, want preserved")
		}
	}
}

// closedChan 返回已关闭的 channel(闸门立即放行)。
func closedChan() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}
