package aggregator

import (
	"errors"
	"testing"

	"github.com/taliove/proxyhub/internal/store"
)

// 跨 kind 互斥(issue 0025):刷新在跑时发起同机场(或全量视角任一)机场测试 → 冲突。
func TestStartAirportTestExclusive_ConflictWithRefresh(t *testing.T) {
	agg, st := newTestAggregator(t)
	release := make(chan struct{})
	defer close(release)
	srv := gatedSubscriptionServer(t, release)
	airport, err := st.CreateAirport("慢机场", srv.URL)
	if err != nil {
		t.Fatalf("CreateAirport() error = %v", err)
	}

	startCalled := false
	start := func() (int64, string, bool, error) {
		startCalled = true
		return 0, "", false, nil
	}

	// 全量刷新在跑:任何机场的测试都冲突
	if _, _, _, err := agg.StartRefreshJob(store.RefreshTriggerManual); err != nil {
		t.Fatalf("StartRefreshJob() error = %v", err)
	}
	if _, key, _, err := agg.StartAirportTestExclusive(airport.ID, start); !errors.Is(err, ErrAirportTestConflict) {
		t.Errorf("test during full refresh: err = %v, want ErrAirportTestConflict", err)
	} else if key != refreshJobKey(airport.ID) {
		t.Errorf("conflict key = %q, want %q", key, refreshJobKey(airport.ID))
	}
	if startCalled {
		t.Error("start callback invoked despite conflict")
	}
}

// 同机场单机场刷新在跑:同机场测试冲突;不同机场不互斥。
func TestStartAirportTestExclusive_SameAirportOnly(t *testing.T) {
	agg, st := newTestAggregator(t)
	release := make(chan struct{})
	defer close(release)
	srvA := gatedSubscriptionServerNamed(t, release, "127.0.0.1:1", "A 01")
	apA, err := st.CreateAirport("机场A", srvA.URL)
	if err != nil {
		t.Fatalf("CreateAirport(A) error = %v", err)
	}
	apB, err := st.CreateAirport("机场B", "https://example.com/sub-b")
	if err != nil {
		t.Fatalf("CreateAirport(B) error = %v", err)
	}

	if _, _, _, err := agg.StartAirportRefreshJob(store.RefreshTriggerManual, apA.ID); err != nil {
		t.Fatalf("StartAirportRefreshJob(A) error = %v", err)
	}

	noop := func() (int64, string, bool, error) { return 0, "", false, nil }
	// 同机场:冲突
	if _, _, _, err := agg.StartAirportTestExclusive(apA.ID, noop); !errors.Is(err, ErrAirportTestConflict) {
		t.Errorf("test A during refresh A: err = %v, want ErrAirportTestConflict", err)
	}
	// 不同机场:放行(start 被调用)
	called := false
	if _, _, _, err := agg.StartAirportTestExclusive(apB.ID, func() (int64, string, bool, error) {
		called = true
		return 0, "", false, nil
	}); err != nil || !called {
		t.Errorf("test B during refresh A: err = %v called = %v, want started (no cross-airport mutex)", err, called)
	}
}

// 反方向:机场测试在跑(checker 注入)时发起刷新 → ErrRefreshConflict;
// 全量刷新视角下任一机场测试都算冲突,单机场刷新只与同机场测试冲突。
func TestStartRefresh_ConflictWithAirportTest(t *testing.T) {
	agg, st := newTestAggregator(t)
	apA, err := st.CreateAirport("机场A", "https://example.com/sub-a")
	if err != nil {
		t.Fatalf("CreateAirport(A) error = %v", err)
	}
	apB, err := st.CreateAirport("机场B", "https://example.com/sub-b")
	if err != nil {
		t.Fatalf("CreateAirport(B) error = %v", err)
	}

	// 模拟 server 侧测试运行时:机场A 有测试在跑
	agg.SetAirportTestConflictChecker(func(airportID int64) (string, bool) {
		if airportID <= 0 || airportID == apA.ID {
			return refreshJobKey(apA.ID), true
		}
		return "", false
	})

	if _, _, _, err := agg.StartRefreshJob(store.RefreshTriggerManual); !errors.Is(err, ErrRefreshConflict) {
		t.Errorf("full refresh during test A: err = %v, want ErrRefreshConflict", err)
	}
	if _, _, _, err := agg.StartAirportRefreshJob(store.RefreshTriggerManual, apA.ID); !errors.Is(err, ErrRefreshConflict) {
		t.Errorf("refresh A during test A: err = %v, want ErrRefreshConflict", err)
	}
	// 不同机场:不互斥(放行,B 的订阅拉取会失败,但发起本身不冲突)
	jobB, _, _, err := agg.StartAirportRefreshJob(store.RefreshTriggerManual, apB.ID)
	if err != nil {
		t.Errorf("refresh B during test A: err = %v, want no conflict", err)
	} else {
		waitJobStatus(t, st, jobB) // 等收口,避免 TempDir 清理竞态
	}
}

// 未注入 checker(无测试运行时):刷新发起不受跨 kind 检查影响。
func TestStartRefresh_NoCheckerNoConflict(t *testing.T) {
	agg, st := newTestAggregator(t)
	if _, err := st.CreateAirport("机场A", "https://example.com/sub-a"); err != nil {
		t.Fatalf("CreateAirport() error = %v", err)
	}
	jobID, _, _, err := agg.StartRefreshJob(store.RefreshTriggerManual)
	if err != nil {
		t.Errorf("StartRefreshJob() without checker: err = %v, want nil", err)
	} else {
		waitJobStatus(t, st, jobID) // 等收口,避免 TempDir 清理竞态
	}
}
