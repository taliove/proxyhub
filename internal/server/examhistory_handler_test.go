package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/taliove/proxyhub/internal/detection"
	"github.com/taliove/proxyhub/internal/jobs"
	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

// examNode fixture:全零 UUID + example.com,绝不含真实凭证。
func examNode() *subscription.Node {
	return &subscription.Node{
		Name:   "exam-node",
		Server: "example.com",
		Port:   443,
		Type:   "vmess",
		UUID:   "00000000-0000-0000-0000-000000000000",
		Source: "airport",
	}
}

// injectFastExam 注入假探测器 + 短配置,让 SSE 秒级跑完(全程不触真实网络)。
// 四段(稳定性/多地域/解锁/出网)都注入假探测器:避免真实网络的不确定时延。
func injectFastExam(t *testing.T, srv *Server) {
	t.Helper()
	det := srv.detectionService.detector
	det.SetStabilityProbeFactory(func(*subscription.Node) (detection.StabilityProbe, error) {
		return func(context.Context) (int, bool) { return 42, true }, nil
	})
	det.SetExamConfigProvider(func() detection.ExamConfig {
		return detection.ExamConfig{StabilityDurationSec: 1, StabilityIntervalMs: 300, ProbeURL: "https://example.com/generate_204", ProbeTimeoutSec: 5}
	})
	det.SetRegionSpeedProbeFactory(func(*subscription.Node) (detection.RegionSpeedProbe, error) {
		return func(_ context.Context, r detection.Region) detection.RegionResult {
			return detection.RegionResult{Code: r.Code, Name: r.Name, TTFBms: 42, DownMbps: 18.5}
		}, nil
	})
	det.SetUnlockProbeFactory(func(*subscription.Node) (detection.UnlockProbe, error) {
		return func(_ context.Context, target detection.Target) detection.Result {
			return detection.Result{TargetName: target.Name, Available: true, Level: detection.LevelFull, Region: "US"}
		}, nil
	})
	det.SetEgressProbeFactory(func(*subscription.Node) (detection.EgressProbe, error) {
		return detection.EgressProbe{
			IPv4: func(context.Context) detection.EgressIPv4 {
				return detection.EgressIPv4{IP: "203.0.113.7", Country: "United States", CountryCode: "US", Hosting: true}
			},
			IPv6: func(context.Context) detection.EgressIPv6 { return detection.EgressIPv6{Available: false} },
			DNS: func(context.Context) detection.EgressDNS {
				return detection.EgressDNS{ResolverIP: "198.51.100.9", ResolverGeo: "United States - Example DNS"}
			},
		}, nil
	})
}

// waitExamHistory 轮询等待某节点的体检历史落库并返回。
// 落历史由任务生命周期(ExamJobManager.runJob)在 SSE 流收口之后异步完成(任务模型下任务寿命长于连接),
// 故 SSE handler 返回不代表已落盘;此处短轮询等待该异步副作用,而非假设其与流终止同步。
func waitExamHistory(t *testing.T, st *store.Store, nodeKey string) *store.ExamHistoryEntry {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		latest, err := st.LatestExamHistory(nodeKey)
		if err != nil {
			t.Fatalf("latest: %v", err)
		}
		if latest != nil {
			return latest
		}
		if time.Now().After(deadline) {
			t.Fatalf("no exam history persisted within timeout")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// waitExamJobDone 轮询等待该节点的体检任务在 jobs 表落终态(done/failed/cancelled/interrupted)。
// 落终态由任务生命周期最后一步 runJob->store.Finish 写入,严格晚于落历史那几笔副作用
// (SaveExamHistory/RecomputeNodeTags/writeback);故任务终态可见 = 该任务再无后台写库。
// 测试据此在拆台(st.Close + t.TempDir 清理)前等清后台写,消除写库与清理竞速。
// 用途区别于 waitExamHistory:后者只等第一笔(落历史),此处等整条任务寿命收尾。
func waitExamJobDone(t *testing.T, st *store.Store, nodeKey string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		records, err := st.Jobs().LoadAll()
		if err != nil {
			t.Fatalf("load jobs: %v", err)
		}
		if examJobTerminal(records, nodeKey) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("exam job for %q did not reach terminal state within timeout", nodeKey)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// examJobTerminal 报告是否存在该节点的 exam 任务且已落终态(非 running)。
func examJobTerminal(records []jobs.Record, nodeKey string) bool {
	for _, rec := range records {
		if rec.Kind == "exam" && rec.Key == nodeKey && rec.Status != jobs.StatusRunning {
			return true
		}
	}
	return false
}

// 体检成功完成 -> 自动落一条历史,latest 可读回。
func TestHandleNodeExamStream_PersistsOnComplete(t *testing.T) {
	node := examNode()
	srv, st := newTestServer(t, []*subscription.Node{node})
	injectFastExam(t, srv)

	req := httptest.NewRequest(http.MethodGet, "/api/nodes/exam/stream?node_key="+node.NodeKey(), nil)
	w := httptest.NewRecorder()
	srv.handleNodeExamStream(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	latest := waitExamHistory(t, st, node.NodeKey())
	if latest.Report.Stability == nil {
		t.Errorf("persisted report missing stability section")
	}
	// 等任务寿命收尾,避免后台写库与拆台清理竞速。
	waitExamJobDone(t, st, node.NodeKey())
}

// 体检中途失败(建会话失败)-> 不落历史。语义在此钉死:失败不落盘。
func TestHandleNodeExamStream_DoesNotPersistOnFailure(t *testing.T) {
	node := examNode()
	srv, st := newTestServer(t, []*subscription.Node{node})

	det := srv.detectionService.detector
	det.SetStabilityProbeFactory(func(*subscription.Node) (detection.StabilityProbe, error) {
		return nil, errors.New("create proxy session failed")
	})

	req := httptest.NewRequest(http.MethodGet, "/api/nodes/exam/stream?node_key="+node.NodeKey(), nil)
	w := httptest.NewRecorder()
	srv.handleNodeExamStream(w, req)

	latest, err := st.LatestExamHistory(node.NodeKey())
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if latest != nil {
		t.Fatalf("failed exam must not be persisted, got %+v", latest)
	}
}

// 体检被取消(请求上下文已取消)-> 不落历史。语义在此钉死:取消不落盘。
func TestHandleNodeExamStream_DoesNotPersistOnCancel(t *testing.T) {
	node := examNode()
	srv, st := newTestServer(t, []*subscription.Node{node})
	injectFastExam(t, srv)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 进入前即取消

	req := httptest.NewRequest(http.MethodGet, "/api/nodes/exam/stream?node_key="+node.NodeKey(), nil).WithContext(ctx)
	w := httptest.NewRecorder()
	srv.handleNodeExamStream(w, req)

	latest, err := st.LatestExamHistory(node.NodeKey())
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if latest != nil {
		t.Fatalf("cancelled exam must not be persisted, got %+v", latest)
	}
}

// latest 查询接口:无历史返回 JSON null,不报错。
func TestHandleGetExamLatest_EmptyReturnsNull(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/nodes/exam/latest?node_key=example.com:443", nil)
	w := httptest.NewRecorder()
	srv.handleGetExamLatest(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if got := strings.TrimSpace(w.Body.String()); got != "null" {
		t.Errorf("body = %q, want null", got)
	}
}

// history 查询接口:无历史返回空数组,不报错。
func TestHandleGetExamHistory_EmptyReturnsEmptyArray(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/nodes/exam/history?node_key=example.com:443", nil)
	w := httptest.NewRecorder()
	srv.handleGetExamHistory(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if got := strings.TrimSpace(w.Body.String()); got != "[]" {
		t.Errorf("body = %q, want []", got)
	}
}

// 缺 node_key/self_node_id -> 400。
func TestHandleGetExamLatest_MissingKey(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/nodes/exam/latest", nil)
	w := httptest.NewRecorder()
	srv.handleGetExamLatest(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// 体检完成后 latest 查询接口读回报告。
func TestHandleGetExamLatest_AfterExam(t *testing.T) {
	node := examNode()
	srv, st := newTestServer(t, []*subscription.Node{node})
	injectFastExam(t, srv)

	// 先跑一次体检落历史。
	examReq := httptest.NewRequest(http.MethodGet, "/api/nodes/exam/stream?node_key="+node.NodeKey(), nil)
	srv.handleNodeExamStream(httptest.NewRecorder(), examReq)

	// 落历史是任务收口后的异步副作用,等其完成再查 latest(见 waitExamHistory)。
	waitExamHistory(t, st, node.NodeKey())
	// 再等整条任务寿命收尾(jobs 表落终态),确保后台写库全部完成,不与拆台清理竞速。
	waitExamJobDone(t, st, node.NodeKey())

	// 再查 latest。
	req := httptest.NewRequest(http.MethodGet, "/api/nodes/exam/latest?node_key="+node.NodeKey(), nil)
	w := httptest.NewRecorder()
	srv.handleGetExamLatest(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var entry store.ExamHistoryEntry
	if err := json.Unmarshal(w.Body.Bytes(), &entry); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
	}
	if entry.Report.Stability == nil {
		t.Errorf("latest report missing stability section")
	}
}
