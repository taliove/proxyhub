package airporttest

import (
	"context"
	"strings"
	"testing"
)

// 安全回归:RunDiagnostic 的错误会落 run.ErrorMessage(随 run 持久化并经 API 展示),
// 不得包含机场订阅 URL 或其 token。

const orchLeakToken = "SECRETTOKEN123"

func TestRunDiagnostic_ErrorNeverLeaksSubscriptionURL(t *testing.T) {
	cases := []struct {
		name string
		url  string
	}{
		{"dial failure", "http://127.0.0.1:1/subscribe?token=" + orchLeakToken},
		{"malformed configured url", "http://exa mple.com/subscribe?token=" + orchLeakToken},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := NewFakeStore(t)
			orch := NewOrchestrator(store, &FakeHealthChecker{}, &FakePoolWriter{})

			run, err := orch.RunDiagnostic(context.Background(), 1, "测试机场", tc.url, false)
			if err != nil {
				t.Fatalf("RunDiagnostic should persist failed run instead of returning error, got %v", err)
			}
			if run == nil {
				t.Fatal("expected persisted failed run")
			}
			if strings.Contains(run.ErrorMessage, orchLeakToken) {
				t.Errorf("ErrorMessage leaks token: %q", run.ErrorMessage)
			}
			if strings.Contains(run.ErrorMessage, tc.url) {
				t.Errorf("ErrorMessage leaks full url: %q", run.ErrorMessage)
			}
		})
	}
}
