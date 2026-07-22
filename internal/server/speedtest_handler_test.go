package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/taliove/proxyhub/internal/detection"
	"github.com/taliove/proxyhub/internal/jobs"
	"github.com/taliove/proxyhub/internal/subscription"
)

// speedtestNode fixture:example.com + 全零 UUID,绝不含真实凭证。
func speedtestNode() *subscription.Node {
	return &subscription.Node{
		Name:   "speed-node",
		Server: "example.com",
		Port:   443,
		Type:   "vmess",
		UUID:   "00000000-0000-0000-0000-000000000000",
		Source: "airport",
	}
}

// injectFakeSpeedtestJobs 注入假 runner 的批量快速测速管理器(不触真实网络),
// onComplete 用真实的 srv.onSpeedtestComplete(同时覆盖写回逻辑)。
func injectFakeSpeedtestJobs(srv *Server, jobStore *jobs.Store, downMbps float64) {
	srv.speedtestJobs = detection.NewBatchSpeedtestJobManager(
		func(ctx context.Context, n *subscription.Node) detection.TestResult {
			return detection.TestResult{Available: true, Mode: "bandwidth", DownMbps: downMbps}
		},
		srv.onSpeedtestComplete,
		detection.WithBatchSpeedtestJobStore(jobStore),
	)
}

// TestHandleBatchSpeedtest_StartRunsAndWritesBack 启动批量快速测速:
// 任务跑完后基准下行写回内存池带宽字段 + node_health(target_name=bandwidth),
// 池内既有上行值保留(批量只测下行,不得清零上行)。
func TestHandleBatchSpeedtest_StartRunsAndWritesBack(t *testing.T) {
	node := speedtestNode()
	node.BandwidthUpMbps = 33.3 // 池内既有上行:批量写回必须保留
	srv, st := newTestServer(t, []*subscription.Node{node})
	injectFakeSpeedtestJobs(srv, st.Jobs(), 88.8)

	body := strings.NewReader(`{"node_keys":["` + node.NodeKey() + `"]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/nodes/speedtest/batch", body)
	w := httptest.NewRecorder()
	srv.handleBatchSpeedtest(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("start status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("start response not JSON: %v", err)
	}
	if resp["key"] != "batch_speedtest" {
		t.Errorf("key = %q, want %q (全局单例)", resp["key"], "batch_speedtest")
	}

	// 等任务自然完成(轮询 jobs 表,最多 2s)
	deadline := time.Now().Add(2 * time.Second)
	for {
		recs, err := st.Jobs().LoadAll()
		if err == nil {
			for _, r := range recs {
				if r.Kind == "batch_speedtest" && r.Status == "done" {
					goto done
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("batch_speedtest job did not reach done within 2s")
		}
		time.Sleep(10 * time.Millisecond)
	}
done:

	// 内存池:下行更新,上行保留
	poolNode := srv.nodes.Nodes()[0]
	if poolNode.BandwidthDownMbps != 88.8 {
		t.Errorf("pool BandwidthDownMbps = %v, want 88.8", poolNode.BandwidthDownMbps)
	}
	if poolNode.BandwidthUpMbps != 33.3 {
		t.Errorf("pool BandwidthUpMbps = %v, want 33.3 (批量仅测下行,上行保留)", poolNode.BandwidthUpMbps)
	}

	// node_health:target_name=bandwidth 最新行,下行新值、上行保留
	views, err := st.GetLatestDetectionResults([]string{node.NodeKey()})
	if err != nil {
		t.Fatalf("GetLatestDetectionResults: %v", err)
	}
	var bw *struct{ Down, Up float64 }
	for _, v := range views[node.NodeKey()] {
		if v.TargetName == "bandwidth" {
			bw = &struct{ Down, Up float64 }{v.DownMbps, v.UpMbps}
		}
	}
	if bw == nil {
		t.Fatal("node_health has no bandwidth row after batch speedtest")
	}
	if bw.Down != 88.8 {
		t.Errorf("node_health down_mbps = %v, want 88.8", bw.Down)
	}
	if bw.Up != 33.3 {
		t.Errorf("node_health up_mbps = %v, want 33.3 (上行保留)", bw.Up)
	}
}

// TestHandleBatchSpeedtestStream_NoActive 无进行中任务时订阅返回 404。
func TestHandleBatchSpeedtestStream_NoActive(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/nodes/speedtest/batch/stream", nil)
	w := httptest.NewRecorder()
	srv.handleBatchSpeedtestStream(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

// TestHandleBatchSpeedtestCancel_NoActive 无进行中任务时取消返回 409。
func TestHandleBatchSpeedtestCancel_NoActive(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/nodes/speedtest/batch/cancel", nil)
	w := httptest.NewRecorder()
	srv.handleBatchSpeedtestCancel(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", w.Code)
	}
}

// TestHandleCancelJob_BatchSpeedtest 通用取消端点按 kind 分发到批量快速测速。
func TestHandleCancelJob_BatchSpeedtest(t *testing.T) {
	node := speedtestNode()
	srv, st := newTestServer(t, []*subscription.Node{node})

	started := make(chan struct{})
	srv.speedtestJobs = detection.NewBatchSpeedtestJobManager(
		func(ctx context.Context, n *subscription.Node) detection.TestResult {
			close(started)
			<-ctx.Done() // 阻塞到取消
			return detection.TestResult{Available: false, Mode: "bandwidth", Error: "cancelled"}
		},
		srv.onSpeedtestComplete,
		detection.WithBatchSpeedtestJobStore(st.Jobs()),
	)

	if _, err := srv.speedtestJobs.Start([]string{node.NodeKey()}, []*subscription.Node{node}, "selected"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	<-started

	req := httptest.NewRequest(http.MethodPost, "/api/jobs/batch_speedtest/batch_speedtest/cancel", nil)
	req.SetPathValue("kind", "batch_speedtest")
	req.SetPathValue("key", "batch_speedtest")
	w := httptest.NewRecorder()
	srv.handleCancelJob(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("cancel status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
}

// TestHandleTestNode_SpeedtestMode 单节点 speedtest 档:mode 合法、TCP 失败快速返回、
// 结果 Mode=bandwidth(写回节点视图带宽字段同轨)。
func TestHandleTestNode_SpeedtestMode(t *testing.T) {
	node := &subscription.Node{
		Name: "dead-node", Server: "127.0.0.1", Port: 1, Type: "vless",
		UUID: "00000000-0000-0000-0000-000000000000", Source: "airport",
	}
	srv, st := newTestServer(t, []*subscription.Node{node})

	body := strings.NewReader(`{"node_key":"` + node.NodeKey() + `","mode":"speedtest"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/nodes/test", body)
	w := httptest.NewRecorder()
	srv.handleTestNode(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var res detection.TestResult
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("response not TestResult JSON: %v", err)
	}
	if res.Available {
		t.Error("Available = true, want false (dead node)")
	}
	if res.Mode != "bandwidth" {
		t.Errorf("Mode = %q, want %q (写回带宽字段同轨)", res.Mode, "bandwidth")
	}
	if !strings.Contains(res.Error, "TCP") {
		t.Errorf("Error = %q, want TCP failure", res.Error)
	}

	// 失败结果也落 node_health(target_name=bandwidth 行)
	views, err := st.GetLatestDetectionResults([]string{node.NodeKey()})
	if err != nil {
		t.Fatalf("GetLatestDetectionResults: %v", err)
	}
	found := false
	for _, v := range views[node.NodeKey()] {
		if v.TargetName == "bandwidth" {
			found = true
		}
	}
	if !found {
		t.Error("node_health missing bandwidth row after speedtest mode")
	}
}

// TestHandleTestNodeStream_SpeedtestMode SSE 流式端点 mode=speedtest:契约不破坏
// (done 帧字段一致),死节点快速出 done。
func TestHandleTestNodeStream_SpeedtestMode(t *testing.T) {
	node := &subscription.Node{
		Name: "dead-node", Server: "127.0.0.1", Port: 1, Type: "vless",
		UUID: "00000000-0000-0000-0000-000000000000", Source: "airport",
	}
	srv, _ := newTestServer(t, []*subscription.Node{node})

	req := httptest.NewRequest(http.MethodGet, "/api/nodes/test/stream?mode=speedtest&node_key="+node.NodeKey(), nil)
	w := httptest.NewRecorder()
	srv.handleTestNodeStream(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"phase":"done"`) {
		t.Errorf("SSE body missing done frame: %q", body)
	}
	if !strings.Contains(body, `"available":false`) {
		t.Errorf("SSE done frame available != false: %q", body)
	}
}
