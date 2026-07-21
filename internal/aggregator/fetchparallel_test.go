package aggregator

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// delayedSubscriptionServer 返回带人为延迟的订阅服务器,节点名带 marker 便于区分机场。
func delayedSubscriptionServer(t *testing.T, delay time.Duration, marker string) *httptest.Server {
	t.Helper()
	link := fmt.Sprintf("trojan://pw@127.0.0.1:1#HK %s", marker)
	content := base64.StdEncoding.EncodeToString([]byte(link))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(delay)
		w.Write([]byte(content))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFetchConcurrency_DefaultAndClamp(t *testing.T) {
	agg, st := newTestAggregator(t)

	// 缺失 -> 默认 4
	if got := agg.fetchConcurrency(); got != defaultFetchConcurrency {
		t.Errorf("missing setting = %d, want %d", got, defaultFetchConcurrency)
	}

	cases := []struct {
		value string
		want  int
	}{
		{"7", 7},
		{"1", 1},
		{"0", minFetchConcurrency},       // 下越界 clamp 到 1(串行)
		{"99", maxFetchConcurrency},      // 上越界 clamp 到 10
		{"abc", defaultFetchConcurrency}, // 非法值回退默认
	}
	for _, c := range cases {
		if err := st.SetSetting("fetch_concurrency", c.value); err != nil {
			t.Fatalf("SetSetting() error = %v", err)
		}
		if got := agg.fetchConcurrency(); got != c.want {
			t.Errorf("value %q = %d, want %d", c.value, got, c.want)
		}
	}
}

func TestFetchAirports_ParallelBeatsSerial(t *testing.T) {
	agg, st := newTestAggregator(t)

	const delay = 200 * time.Millisecond
	for i := 0; i < 3; i++ {
		srv := delayedSubscriptionServer(t, delay, fmt.Sprintf("n%d", i))
		if _, err := st.CreateAirport(fmt.Sprintf("机场%d", i), srv.URL); err != nil {
			t.Fatalf("CreateAirport() error = %v", err)
		}
	}

	rl := &runLog{} // runID=0,事件 no-op

	// 并行(度=3):总耗时应接近单机场延迟,远小于串行的 3x
	if err := st.SetSetting("fetch_concurrency", "3"); err != nil {
		t.Fatalf("SetSetting() error = %v", err)
	}
	start := time.Now()
	if _, err := agg.fetchAirports(context.Background(), rl, nil); err != nil {
		t.Fatalf("fetchAirports() error = %v", err)
	}
	parallel := time.Since(start)
	if parallel >= 500*time.Millisecond {
		t.Errorf("parallel fetch took %v, want < 500ms (serial would be >=600ms)", parallel)
	}

	// 串行(度=1):总耗时 >= 3x 延迟(留 10% 余量)
	if err := st.SetSetting("fetch_concurrency", "1"); err != nil {
		t.Fatalf("SetSetting() error = %v", err)
	}
	start = time.Now()
	if _, err := agg.fetchAirports(context.Background(), rl, nil); err != nil {
		t.Fatalf("fetchAirports() error = %v", err)
	}
	serial := time.Since(start)
	if serial < 550*time.Millisecond {
		t.Errorf("serial fetch took %v, want >= 550ms", serial)
	}
}

func TestFetchAirports_OrderStableRegardlessOfCompletion(t *testing.T) {
	agg, st := newTestAggregator(t)

	// ListAirports 按 id DESC 返回(后建在前)。后建的「慢机场」排在列表第一,
	// 但拉取慢、完成在后:完成序与列表序相反,归并必须仍按列表序。
	fast := delayedSubscriptionServer(t, 0, "FAST")
	slow := delayedSubscriptionServer(t, 200*time.Millisecond, "SLOW")
	if _, err := st.CreateAirport("快机场", fast.URL); err != nil {
		t.Fatalf("CreateAirport() error = %v", err)
	}
	if _, err := st.CreateAirport("慢机场", slow.URL); err != nil {
		t.Fatalf("CreateAirport() error = %v", err)
	}
	if err := st.SetSetting("fetch_concurrency", "2"); err != nil {
		t.Fatalf("SetSetting() error = %v", err)
	}

	result, err := agg.fetchAirports(context.Background(), &runLog{}, nil)
	if err != nil {
		t.Fatalf("fetchAirports() error = %v", err)
	}
	if len(result.allNodes) != 2 {
		t.Fatalf("len(allNodes) = %d, want 2", len(result.allNodes))
	}
	if result.allNodes[0].Source != "慢机场" || result.allNodes[1].Source != "快机场" {
		t.Errorf("order = [%s %s], want [慢机场 快机场] (airport order, not completion order)",
			result.allNodes[0].Source, result.allNodes[1].Source)
	}
}

func TestFetchAirports_FailureIsolation(t *testing.T) {
	agg, st := newTestAggregator(t)

	good := delayedSubscriptionServer(t, 0, "GOOD")
	if _, err := st.CreateAirport("好机场", good.URL); err != nil {
		t.Fatalf("CreateAirport() error = %v", err)
	}
	if _, err := st.CreateAirport("坏机场", "http://127.0.0.1:1"); err != nil {
		t.Fatalf("CreateAirport() error = %v", err)
	}
	if err := st.SetSetting("fetch_concurrency", "2"); err != nil {
		t.Fatalf("SetSetting() error = %v", err)
	}

	result, err := agg.fetchAirports(context.Background(), &runLog{}, nil)
	if err != nil {
		t.Fatalf("fetchAirports() error = %v", err)
	}
	if result.failed != 1 {
		t.Errorf("failed = %d, want 1", result.failed)
	}
	if len(result.allNodes) != 1 || result.allNodes[0].Source != "好机场" {
		t.Errorf("allNodes = %v, want only 好机场's node", result.allNodes)
	}
	if result.airportNodes["坏机场"] != nil {
		t.Errorf("airportNodes[坏机场] = %v, want nil (failed)", result.airportNodes["坏机场"])
	}
}
