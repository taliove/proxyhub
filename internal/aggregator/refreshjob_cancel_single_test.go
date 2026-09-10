package aggregator

import (
	"testing"
	"time"

	"github.com/taliove/proxyhub/internal/jobs"
	"github.com/taliove/proxyhub/internal/store"
)

// TestCancelSingleAirportRefresh_DuringInflightFetch issue #143:取消单机场刷新时,
// 若取消打断在途拉取(FetchContext 随任务 ctx 退出),refresh_runs 必须记 cancelled
// 而非 failed——与 jobs 行的权威终态口径一致(参照 executeForUser 的判定顺序)。
func TestCancelSingleAirportRefresh_DuringInflightFetch(t *testing.T) {
	agg, st := newTestAggregator(t)
	release := make(chan struct{})
	releaseNow := releaseOnce(release)
	defer releaseNow()

	gated := gatedSubscriptionServer(t, release) // 闸门不放行,拉取阻塞在途
	airport, err := st.CreateAirport("慢机场", gated.URL)
	if err != nil {
		t.Fatalf("CreateAirport() error = %v", err)
	}

	jobID, key, started, err := agg.StartAirportRefreshJob(store.RefreshTriggerManual, airport.ID)
	if err != nil || !started {
		t.Fatalf("StartAirportRefreshJob() started=%v err=%v, want started", started, err)
	}
	run := waitRefreshRun(t, st, jobID)

	// 确定性时序:拉取开始事件落库后才取消(此时拉取必在闸门上)。
	waitRefreshEvent(t, st, run.ID, "单机场刷新")

	if !agg.CancelRefresh(key) {
		t.Fatal("CancelRefresh() = false, want true (job running)")
	}

	if status := waitJobStatus(t, st, jobID); status != jobs.StatusCancelled {
		t.Errorf("job status = %s, want cancelled", status)
	}

	// 关键断言:refresh_runs 终态是 cancelled,不是 failed。
	deadline := time.Now().Add(3 * time.Second)
	for run.Status == store.RefreshStatusRunning && time.Now().Before(deadline) {
		run, _ = st.GetRefreshRun(run.ID)
		time.Sleep(20 * time.Millisecond)
	}
	if run.Status != store.RefreshStatusCancelled {
		t.Errorf("refresh run status = %s, want cancelled (cancel must not be misrecorded as failed)", run.Status)
	}
}
