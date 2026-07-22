package server

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/taliove/proxyhub/internal/detection"
	"github.com/taliove/proxyhub/internal/subscription"
)

// newBatchExamTestServer builds a minimal Server with a batch exam job manager whose
// runners report invocation through the returned channels.
func newBatchExamTestServer(t *testing.T, nodes []*subscription.Node) (*Server, chan string, chan string) {
	t.Helper()

	simplifiedCh := make(chan string, 8)
	fullCh := make(chan string, 8)

	mgr := detection.NewBatchExamJobManager(
		func(ctx context.Context, n *subscription.Node, emit func(detection.ExamEvent)) detection.ExamReport {
			simplifiedCh <- n.Name
			return detection.ExamReport{Stability: &detection.StabilityMetrics{Total: 1, Succeeded: 1, Score: 100}}
		},
		func(ctx context.Context, n *subscription.Node, emit func(detection.ExamEvent)) detection.ExamReport {
			fullCh <- n.Name
			return detection.ExamReport{Stability: &detection.StabilityMetrics{Total: 1, Succeeded: 1, Score: 100}}
		},
		nil,
	)

	s := &Server{
		nodes:            &fakeNodes{nodes: nodes},
		logger:           slog.Default(),
		detectionService: &DetectionService{},
		batchExamJobs:    mgr,
	}
	return s, simplifiedCh, fullCh
}

func postBatchExam(t *testing.T, s *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/nodes/exam/batch", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	s.handleBatchExam(rec, req)
	return rec
}

// waitRunnerInvocation waits for a runner invocation or fails after a short timeout.
func waitRunnerInvocation(t *testing.T, ch <-chan string, want string) {
	t.Helper()
	select {
	case got := <-ch:
		if got != want {
			t.Errorf("runner invoked with node %q, want %q", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Errorf("runner not invoked within timeout, want node %q", want)
	}
}

// assertNoRunnerInvocation asserts a runner is not invoked within a short window.
func assertNoRunnerInvocation(t *testing.T, ch <-chan string, name string) {
	t.Helper()
	select {
	case got := <-ch:
		t.Errorf("%s runner unexpectedly invoked with node %q", name, got)
	case <-time.After(150 * time.Millisecond):
	}
}

func batchExamFixtureNode() *subscription.Node {
	return &subscription.Node{
		Name:   "exam-node",
		Type:   "ss",
		Server: "example.com",
		Port:   443,
	}
}

// TestHandleBatchExam_InvalidMode_BadRequest verifies unknown mode is rejected with 400.
func TestHandleBatchExam_InvalidMode_BadRequest(t *testing.T) {
	s, _, _ := newBatchExamTestServer(t, []*subscription.Node{batchExamFixtureNode()})

	rec := postBatchExam(t, s, `{"node_keys":["k1"],"mode":"bogus"}`)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

// TestHandleBatchExam_DefaultMode_RunsSimplified verifies missing mode keeps the
// simplified runner (legacy clients unchanged).
func TestHandleBatchExam_DefaultMode_RunsSimplified(t *testing.T) {
	node := batchExamFixtureNode()
	s, simplifiedCh, fullCh := newBatchExamTestServer(t, []*subscription.Node{node})

	body, _ := json.Marshal(map[string]any{"node_keys": []string{node.NodeKey()}})
	rec := postBatchExam(t, s, string(body))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body: %s)", rec.Code, http.StatusOK, rec.Body.String())
	}

	waitRunnerInvocation(t, simplifiedCh, node.Name)
	assertNoRunnerInvocation(t, fullCh, "full")
}

// TestHandleBatchExam_FullMode_PassesThrough verifies mode=full is propagated to the
// job params and selects the full runner.
func TestHandleBatchExam_FullMode_PassesThrough(t *testing.T) {
	node := batchExamFixtureNode()
	s, simplifiedCh, fullCh := newBatchExamTestServer(t, []*subscription.Node{node})

	body, _ := json.Marshal(map[string]any{
		"node_keys": []string{node.NodeKey()},
		"mode":      "full",
	})
	rec := postBatchExam(t, s, string(body))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body: %s)", rec.Code, http.StatusOK, rec.Body.String())
	}

	waitRunnerInvocation(t, fullCh, node.Name)
	assertNoRunnerInvocation(t, simplifiedCh, "simplified")
}
