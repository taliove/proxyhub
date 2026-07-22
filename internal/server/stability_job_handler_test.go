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
	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

// waitJobTerminal 轮询等待指定 kind/key 的任务在 jobs 表落终态(非 running)。
// 与 waitExamJobDone 同构:终态可见 = 该任务再无后台写库,消除与拆台清理的竞速。
func waitJobTerminal(t *testing.T, st *store.Store, kind, key string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		records, err := st.Jobs().LoadAll()
		if err != nil {
			t.Fatalf("load jobs: %v", err)
		}
		for _, rec := range records {
			if rec.Kind == kind && rec.Key == key && rec.Status != jobs.StatusRunning {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("job %s/%s did not reach terminal state within timeout", kind, key)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// stabilityFixtureNodes 批量检查 fixture:全零 UUID + example.com,绝无真实凭证。
func stabilityFixtureNodes() []*subscription.Node {
	return []*subscription.Node{
		{Name: "st-node-1", Server: "a.example.com", Port: 443, Type: "vmess", UUID: "00000000-0000-0000-0000-000000000000", Source: "airport"},
		{Name: "st-node-2", Server: "b.example.com", Port: 443, Type: "vmess", UUID: "00000000-0000-0000-0000-000000000000", Source: "airport"},
	}
}

// 批量"出网+稳定性":启动 -> 每节点落 stability_check 历史 -> 任务终态;
// 完整体检口径查询不被抢占;stable-* 标签按既有派生规则更新。
func TestHandleBatchStability_RunPersistsMarkedHistory(t *testing.T) {
	nodes := stabilityFixtureNodes()
	srv, st := newTestServer(t, nodes)
	injectFastExam(t, srv)

	body := strings.NewReader(`{"node_keys":["` + nodes[0].NodeKey() + `","` + nodes[1].NodeKey() + `"]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/nodes/stability/batch", body)
	w := httptest.NewRecorder()
	srv.handleBatchStability(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["key"] != "batch_stability" {
		t.Errorf("key = %q, want batch_stability (global singleton)", resp["key"])
	}

	waitJobTerminal(t, st, "batch_stability", "batch_stability")

	for _, n := range nodes {
		// 每节点落一条带 source=stability_check 的历史。
		latest, err := st.LatestExamHistory(n.NodeKey())
		if err != nil {
			t.Fatalf("latest %s: %v", n.NodeKey(), err)
		}
		if latest == nil {
			t.Fatalf("node %s: no history persisted", n.Name)
		}
		if latest.Report.Source != detection.ExamSourceStabilityCheck {
			t.Errorf("node %s: source = %q, want stability_check", n.Name, latest.Report.Source)
		}
		if latest.Report.Stability == nil {
			t.Errorf("node %s: missing stability section", n.Name)
		}
		if latest.Report.RegionSpeed != nil || latest.Report.Unlock != nil {
			t.Errorf("node %s: unexpected speed/unlock section in stability check report", n.Name)
		}

		// "最近体检"语义保护:节点只有 stability_check 历史时,完整体检口径查询视为未体检。
		complete, err := st.LatestCompleteExamHistory(n.NodeKey())
		if err != nil {
			t.Fatalf("latest complete %s: %v", n.NodeKey(), err)
		}
		if complete != nil {
			t.Errorf("node %s: complete-exam caliber sees stability_check report", n.Name)
		}

		// stable-* 标签按既有派生规则更新(假探测器全成功 -> stable-good)。
		tags, err := st.ListNodeTags([]string{n.NodeKey()})
		if err != nil {
			t.Fatalf("tags %s: %v", n.NodeKey(), err)
		}
		var sawStable bool
		for _, tag := range tags[n.NodeKey()] {
			if strings.HasPrefix(tag, "stable-") {
				sawStable = true
			}
		}
		if !sawStable {
			t.Errorf("node %s: no stable-* tag derived after stability check, got %v", n.Name, tags[n.NodeKey()])
		}
	}
}

// 批量任务取消:经通用任务 API(POST /api/jobs/{kind}/{key}/cancel)按 kind 分发。
func TestHandleCancelJob_BatchStability(t *testing.T) {
	node := examNode()
	srv, st := newTestServer(t, []*subscription.Node{node})
	injectFastExam(t, srv)
	// 稳定性探测阻塞直到任务 ctx 取消,让任务保持 running 以便取消。
	srv.detectionService.detector.SetStabilityProbeFactory(func(*subscription.Node) (detection.StabilityProbe, error) {
		return func(ctx context.Context) (int, bool) {
			<-ctx.Done()
			return 0, false
		}, nil
	})

	body := strings.NewReader(`{"node_keys":["` + node.NodeKey() + `"]}`)
	startReq := httptest.NewRequest(http.MethodPost, "/api/nodes/stability/batch", body)
	startW := httptest.NewRecorder()
	srv.handleBatchStability(startW, startReq)
	if startW.Code != http.StatusOK {
		t.Fatalf("start status = %d, body = %s", startW.Code, startW.Body.String())
	}

	cancelReq := httptest.NewRequest(http.MethodPost, "/api/jobs/batch_stability/batch_stability/cancel", nil)
	cancelReq.SetPathValue("kind", "batch_stability")
	cancelReq.SetPathValue("key", "batch_stability")
	cancelW := httptest.NewRecorder()
	srv.handleCancelJob(cancelW, cancelReq)
	if cancelW.Code != http.StatusOK {
		t.Fatalf("cancel status = %d, body = %s", cancelW.Code, cancelW.Body.String())
	}

	waitJobTerminal(t, st, "batch_stability", "batch_stability")

	// 取消不落历史(任务在稳定性采样中途被取消,无完整段落盘)。
	latest, err := st.LatestExamHistory(node.NodeKey())
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if latest != nil {
		t.Errorf("cancelled batch stability check must not persist history, got %+v", latest)
	}
}

// 单节点入口:SSE 触发同语义检查,落 stability_check 历史,
// "最近体检"接口仍指向之前的完整体检(端到端语义保护)。
func TestHandleNodeStabilityStream_PersistsAndProtectsLatestExam(t *testing.T) {
	node := examNode()
	srv, st := newTestServer(t, []*subscription.Node{node})
	injectFastExam(t, srv)

	// 先有一次完整体检(90 分)。
	seedFullExam(t, st, node.NodeKey(), 90)
	seeded, err := st.LatestExamHistory(node.NodeKey())
	if err != nil || seeded == nil {
		t.Fatalf("seed full exam: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/nodes/stability/stream?node_key="+node.NodeKey(), nil)
	w := httptest.NewRecorder()
	srv.handleNodeStabilityStream(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	// 落历史是任务收口后的异步副作用(见 waitExamHistory);此处必须等"新于种子行"的
	// 一行——种子行会立即命中 returns-any 语义,不区分新旧会读到种子行(竞态)。
	// 最新一条(任意来源)应是本次 stability_check。
	latest := waitNewExamHistory(t, st, node.NodeKey(), seeded.ID)
	if latest.Report.Source != detection.ExamSourceStabilityCheck {
		t.Errorf("latest history source = %q, want stability_check", latest.Report.Source)
	}
	if latest.Report.RegionSpeed != nil || latest.Report.Unlock != nil {
		t.Error("stability check report must not contain speed/unlock sections")
	}
	waitJobTerminal(t, st, "exam_stability", node.NodeKey())

	// "最近体检"接口仍返回之前的完整体检(90 分),不被缺段报告抢占。
	latestReq := httptest.NewRequest(http.MethodGet, "/api/nodes/exam/latest?node_key="+node.NodeKey(), nil)
	latestW := httptest.NewRecorder()
	srv.handleGetExamLatest(latestW, latestReq)
	var entry store.ExamHistoryEntry
	if err := json.Unmarshal(latestW.Body.Bytes(), &entry); err != nil {
		t.Fatalf("decode latest: %v", err)
	}
	if entry.Report.Source == detection.ExamSourceStabilityCheck {
		t.Error("exam/latest preempted by stability check, want complete-exam caliber")
	}
	if entry.Report.Stability == nil || entry.Report.Stability.Score != 90 {
		t.Errorf("exam/latest score = %+v, want the full exam with score 90", entry.Report.Stability)
	}

	// stable-* 标签已由本次检查的新鲜评分更新(假探测器全成功 -> stable-good)。
	tags, err := st.ListNodeTags([]string{node.NodeKey()})
	if err != nil {
		t.Fatalf("tags: %v", err)
	}
	var sawStableGood bool
	for _, tag := range tags[node.NodeKey()] {
		if tag == "stable-good" {
			sawStableGood = true
		}
	}
	if !sawStableGood {
		t.Errorf("stable-good tag not derived after stability check, got %v", tags[node.NodeKey()])
	}
}

// 单节点取消:无进行中任务返回 409。
func TestHandleNodeStabilityCancel_NoActive(t *testing.T) {
	node := examNode()
	srv, _ := newTestServer(t, []*subscription.Node{node})

	req := httptest.NewRequest(http.MethodPost, "/api/nodes/stability/cancel?node_key="+node.NodeKey(), nil)
	w := httptest.NewRecorder()
	srv.handleNodeStabilityCancel(w, req)
	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409 (no active check)", w.Code)
	}
}

// 单节点取消:进行中的检查被取消,不落历史。
func TestHandleNodeStabilityCancel_Active(t *testing.T) {
	node := examNode()
	srv, st := newTestServer(t, []*subscription.Node{node})
	injectFastExam(t, srv)
	srv.detectionService.detector.SetStabilityProbeFactory(func(*subscription.Node) (detection.StabilityProbe, error) {
		return func(ctx context.Context) (int, bool) {
			<-ctx.Done()
			return 0, false
		}, nil
	})

	// SSE 在后台跑(阻塞在稳定性采样)。
	streamReq := httptest.NewRequest(http.MethodGet, "/api/nodes/stability/stream?node_key="+node.NodeKey(), nil)
	go srv.handleNodeStabilityStream(httptest.NewRecorder(), streamReq)

	// 等任务进入 running。
	deadline := time.Now().Add(2 * time.Second)
	for {
		if srv.stabilityExamJobs.Cancel(node.NodeKey()) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("stability exam job did not start within timeout")
		}
		time.Sleep(10 * time.Millisecond)
	}

	waitJobTerminal(t, st, "exam_stability", node.NodeKey())

	latest, err := st.LatestExamHistory(node.NodeKey())
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if latest != nil {
		t.Errorf("cancelled stability check must not persist history, got %+v", latest)
	}
}

// waitNewExamHistory 轮询等待某节点落"新于 afterID"的体检记录并返回。
// 与 waitExamHistory(returns-any)的区别:调用方已先种子一行历史时,后者会立即
// 命中种子行造成竞态;此处明确等待本次动作产出的新行。
func waitNewExamHistory(t *testing.T, st *store.Store, nodeKey string, afterID int64) *store.ExamHistoryEntry {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		latest, err := st.LatestExamHistory(nodeKey)
		if err != nil {
			t.Fatalf("latest: %v", err)
		}
		if latest != nil && latest.ID > afterID {
			return latest
		}
		if time.Now().After(deadline) {
			t.Fatalf("no new exam history (id > %d) persisted within timeout", afterID)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
