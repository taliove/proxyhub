package airporttest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/taliove/proxyhub/internal/subscription"
)

// TestRunDiagnostic_BodyOverLimit 诊断拉取的响应体有上限(issue #143,与 fetcher
// 同口径):超过 subscription.DefaultMaxBodyBytes 即失败收口,不再无上限裸读 body。
func TestRunDiagnostic_BodyOverLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 上限 + 1MiB:必然触发超限;内容无关紧要,零值字节即可。
		chunk := make([]byte, 1<<20)
		for i := int64(0); i < subscription.DefaultMaxBodyBytes/int64(len(chunk))+1; i++ {
			if _, err := w.Write(chunk); err != nil {
				return // 客户端(被测方)判超限后断开,停止发流
			}
		}
	}))
	t.Cleanup(srv.Close)

	store := NewFakeStore(t)
	orch := NewOrchestrator(store, &FakeHealthChecker{}, &FakePoolWriter{})
	run, err := orch.RunDiagnostic(context.Background(), 1, "测试机场", srv.URL, false)
	if err != nil {
		t.Fatalf("RunDiagnostic should persist failed run instead of returning error, got %v", err)
	}
	if run.Status != StatusFailed {
		t.Fatalf("status = %s, want %s", run.Status, StatusFailed)
	}
	if !strings.Contains(run.ErrorMessage, subscription.ErrSubscriptionTooLarge.Error()) {
		t.Errorf("ErrorMessage = %q, want mention %q", run.ErrorMessage, subscription.ErrSubscriptionTooLarge)
	}
}

// TestOrchestrator_FetchClientShared 诊断拉取 client 是 Orchestrator 字段(连接池跨
// 诊断复用,不再每次新建 Transport);SetFetchTimeouts 改超时即重建 client。
func TestOrchestrator_FetchClientShared(t *testing.T) {
	orch := NewOrchestrator(NewFakeStore(t), &FakeHealthChecker{}, &FakePoolWriter{})
	if orch.fetchClient == nil {
		t.Fatal("fetchClient is nil, want shared client built at construction")
	}
	first := orch.fetchClient
	orch.SetFetchTimeouts(time.Second, time.Second)
	if orch.fetchClient == first {
		t.Error("SetFetchTimeouts should rebuild fetchClient (connect timeout is transport-level)")
	}
}
