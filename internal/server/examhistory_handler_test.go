package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/taliove/proxyhub/internal/detection"
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

// injectFastExam 注入假探测器 + 短配置,让 SSE 秒级跑完(不触真实网络)。
func injectFastExam(t *testing.T, srv *Server) {
	t.Helper()
	det := srv.detectionService.detector
	det.SetStabilityProbeFactory(func(*subscription.Node) (detection.StabilityProbe, error) {
		return func(context.Context) (int, bool) { return 42, true }, nil
	})
	det.SetExamConfigProvider(func() detection.ExamConfig {
		return detection.ExamConfig{StabilityDurationSec: 1, StabilityIntervalMs: 300, ProbeURL: "https://example.com/generate_204", ProbeTimeoutSec: 5}
	})
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

	latest, err := st.LatestExamHistory(node.NodeKey())
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if latest == nil {
		t.Fatalf("no exam history persisted after successful exam")
	}
	if latest.Report.Stability == nil {
		t.Errorf("persisted report missing stability section")
	}
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
