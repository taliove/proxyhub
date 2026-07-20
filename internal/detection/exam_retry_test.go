package detection

import (
	"context"
	"testing"
)

// --- 传输类错误分类(纯函数):判定结论绝不误判为可重试 ---

func TestIsTransientNetError(t *testing.T) {
	tests := []struct {
		name string
		msg  string
		want bool
	}{
		{name: "empty (success) -> not transient", msg: "", want: false},
		{name: "i/o timeout -> transient", msg: "dial tcp 203.0.113.7:443: i/o timeout", want: true},
		{name: "context deadline exceeded -> transient", msg: "Get \"https://example.com\": context deadline exceeded", want: true},
		{name: "connection reset -> transient", msg: "read tcp: connection reset by peer", want: true},
		{name: "connection refused -> transient", msg: "dial tcp: connection refused", want: true},
		{name: "EOF -> transient", msg: "unexpected EOF", want: true},
		{name: "no route to host -> transient", msg: "dial tcp: no route to host", want: true},
		{name: "chinese timeout hint -> transient", msg: "IPv6 出口探测超时", want: true},
		{name: "wrapped chinese prefix keeps go token", msg: "请求失败: dial tcp: i/o timeout", want: true},
		{name: "http status verdict -> not transient", msg: "status 403", want: false},
		{name: "netflix inconclusive verdict -> not transient", msg: "netflix classification inconclusive (status 500)", want: false},
		{name: "parse failure -> not transient", msg: "解析 IPv4 出口响应失败: invalid character", want: false},
		{name: "unknown kind config error -> not transient", msg: "target \"Bad\" is not an unlock kind", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isTransientNetError(tt.msg); got != tt.want {
				t.Errorf("isTransientNetError(%q) = %v, want %v", tt.msg, got, tt.want)
			}
		})
	}
}

// --- 通用重试循环:捞回 / 耗尽 / 结论不重试 / ctx 取消 / 零重试 ---

func TestRetryResult_RecoversAfterRetry(t *testing.T) {
	calls := 0
	got := retryResult(context.Background(), 1,
		func() string {
			calls++
			if calls == 1 {
				return "transient"
			}
			return "ok"
		},
		func(s string) bool { return s == "transient" },
	)
	if got != "ok" {
		t.Errorf("result = %q, want ok (recovered on retry)", got)
	}
	if calls != 2 {
		t.Errorf("attempts = %d, want 2 (one retry)", calls)
	}
}

func TestRetryResult_ExhaustsAndReturnsLast(t *testing.T) {
	calls := 0
	got := retryResult(context.Background(), 1,
		func() string { calls++; return "transient" },
		func(s string) bool { return s == "transient" },
	)
	if got != "transient" {
		t.Errorf("result = %q, want transient (exhausted)", got)
	}
	if calls != 2 {
		t.Errorf("attempts = %d, want 2 (initial + 1 retry then give up)", calls)
	}
}

func TestRetryResult_NoRetryWhenNotRetryable(t *testing.T) {
	calls := 0
	got := retryResult(context.Background(), 3,
		func() string { calls++; return "verdict" },
		func(s string) bool { return s == "transient" },
	)
	if got != "verdict" {
		t.Errorf("result = %q, want verdict", got)
	}
	if calls != 1 {
		t.Errorf("attempts = %d, want 1 (verdict never retries)", calls)
	}
}

func TestRetryResult_StopsOnCancelledCtx(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	got := retryResult(ctx, 5,
		func() string { calls++; return "transient" },
		func(s string) bool { return true },
	)
	if got != "transient" {
		t.Errorf("result = %q, want transient", got)
	}
	if calls != 1 {
		t.Errorf("attempts = %d, want 1 (cancelled ctx suppresses retry)", calls)
	}
}

func TestRetryResult_ZeroRetries(t *testing.T) {
	calls := 0
	retryResult(context.Background(), 0,
		func() int { calls++; return 0 },
		func(int) bool { return true },
	)
	if calls != 1 {
		t.Errorf("attempts = %d, want 1 (maxRetries=0 means single attempt)", calls)
	}
}
