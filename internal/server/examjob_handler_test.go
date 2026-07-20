package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/taliove/proxyhub/internal/detection"
	"github.com/taliove/proxyhub/internal/subscription"
)

func TestHandleNodeExamCancel_NoActiveExam(t *testing.T) {
	node := examNode()
	srv, _ := newTestServer(t, []*subscription.Node{node})

	req := httptest.NewRequest(http.MethodPost, "/api/nodes/exam/cancel?node_key="+node.NodeKey(), nil)
	w := httptest.NewRecorder()
	srv.handleNodeExamCancel(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (no active exam)", w.Code)
	}
}

func TestHandleNodeExamCancel_CancelsRunningJobNoHistory(t *testing.T) {
	node := examNode()
	srv, _ := newTestServer(t, []*subscription.Node{node})

	started := make(chan struct{})
	var saved int
	var mu sync.Mutex
	// runner:先推一帧,再阻塞到任务 ctx 取消;取消时报告即便含指标也不该落历史。
	srv.examJobs = detection.NewExamJobManager(
		func(ctx context.Context, _ *subscription.Node, emit func(detection.ExamEvent)) detection.ExamReport {
			emit(detection.ExamEvent{Phase: "sample", Section: "stability"})
			close(started)
			<-ctx.Done()
			return detection.ExamReport{Stability: &detection.StabilityMetrics{Succeeded: 1}}
		},
		func(string, detection.ExamReport) { mu.Lock(); saved++; mu.Unlock() },
	)

	sub := srv.examJobs.Open(node.NodeKey(), node)
	defer sub.Close()
	<-started

	req := httptest.NewRequest(http.MethodPost, "/api/nodes/exam/cancel?node_key="+node.NodeKey(), nil)
	w := httptest.NewRecorder()
	srv.handleNodeExamCancel(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("cancel status = %d, want 200", w.Code)
	}

	var phases []string
	for f := range sub.Live {
		phases = append(phases, f.Phase)
	}
	if len(phases) == 0 || phases[len(phases)-1] != "cancelled" {
		t.Fatalf("live phases = %v, want last = cancelled", phases)
	}

	mu.Lock()
	defer mu.Unlock()
	if saved != 0 {
		t.Fatalf("history saved %d times on cancel, want 0", saved)
	}
}

// TestHandleNodeExamStream_DisconnectKeepsJobAlive 是本票的核心行为:
// SSE 断开(请求 ctx 取消)只结束本次连接,后台任务继续跑到完成并落历史;
// 重连附加时先回放全部缓冲事件(带序号)再收尾于 done。
func TestHandleNodeExamStream_DisconnectKeepsJobAlive(t *testing.T) {
	node := examNode()
	srv, _ := newTestServer(t, []*subscription.Node{node})

	gate := make(chan detection.ExamEvent)
	finish := make(chan detection.ExamReport, 1)
	var savedKey string
	var mu sync.Mutex
	// runner 的 ctx 是任务自带 ctx(源自 Background),连接断开不会取消它。
	// 若实现误从请求 ctx 派生,此处会在断开时取消,返回空报告 -> 不落历史 -> 断言失败。
	srv.examJobs = detection.NewExamJobManager(
		func(ctx context.Context, _ *subscription.Node, emit func(detection.ExamEvent)) detection.ExamReport {
			for {
				select {
				case <-ctx.Done():
					return detection.ExamReport{}
				case e, ok := <-gate:
					if !ok {
						return <-finish
					}
					emit(e)
				}
			}
		},
		func(k string, _ detection.ExamReport) { mu.Lock(); savedKey = k; mu.Unlock() },
	)

	// 连接 1:可取消的请求 ctx 模拟客户端断开。
	ctx1, cancel1 := context.WithCancel(context.Background())
	req1 := httptest.NewRequest(http.MethodGet, "/api/nodes/exam/stream?node_key="+node.NodeKey(), nil).WithContext(ctx1)
	w1 := httptest.NewRecorder()
	conn1Done := make(chan struct{})
	go func() {
		srv.handleNodeExamStream(w1, req1)
		close(conn1Done)
	}()

	gate <- detection.ExamEvent{Phase: "sample", Section: "stability"}
	cancel1()   // 断开连接 1
	<-conn1Done // handler 返回后再读 w1 才安全

	// 任务仍在后台:推完剩余事件并让它自然完成。
	gate <- detection.ExamEvent{Phase: "done"}
	finish <- detection.ExamReport{Stability: &detection.StabilityMetrics{Succeeded: 1}}
	close(gate)

	// 连接 2:重连附加,应回放全部帧并收尾于 done。
	req2 := httptest.NewRequest(http.MethodGet, "/api/nodes/exam/stream?node_key="+node.NodeKey(), nil)
	w2 := httptest.NewRecorder()
	srv.handleNodeExamStream(w2, req2)

	frames := parseSSEFrames(t, w2.Body.String())
	if len(frames) == 0 {
		t.Fatal("reconnect replayed no frames")
	}
	var sawSample, sawDone bool
	prevSeq := -1
	for i, f := range frames {
		seqRaw, ok := f["seq"]
		if !ok {
			t.Fatalf("frame[%d] missing seq: %v", i, f)
		}
		seq := int(seqRaw.(float64))
		if seq <= prevSeq {
			t.Fatalf("seq not strictly increasing at frame[%d]: %d after %d", i, seq, prevSeq)
		}
		prevSeq = seq
		switch f["phase"] {
		case "sample":
			sawSample = true
		case "done":
			sawDone = true
		}
	}
	if !sawSample || !sawDone {
		t.Fatalf("reconnect frames missing sample/done: sample=%v done=%v", sawSample, sawDone)
	}

	mu.Lock()
	defer mu.Unlock()
	if savedKey != node.NodeKey() {
		t.Fatalf("history saved key = %q, want %q (job must survive disconnect and complete)", savedKey, node.NodeKey())
	}
}
