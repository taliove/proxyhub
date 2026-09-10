package subscription

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// 超时拆分 / 重试 / 体上限(issue #143):旧 http.Client.Timeout 一刀切导致
// 大订阅体读不完即判失败;这里验证分离语义、重试口径与超限错误类。

const timeoutTestNode = "trojan://pw@node1.example.com:443#HK 01"

// newTestFetcher 缩小退避基数,避免重试测试被 backoff 拖慢。
func newTestFetcher(connectTimeout, readTimeout time.Duration) *Fetcher {
	f := NewFetcher(connectTimeout, readTimeout)
	f.backoffBase = time.Millisecond
	return f
}

// slowBodyServer 分段慢发响应体:先发首段并 flush,之后每段间隔 interval。
func slowBodyServer(t *testing.T, chunks []string, interval time.Duration) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("ResponseWriter does not support Flush")
			return
		}
		for i, chunk := range chunks {
			if _, err := w.Write([]byte(chunk)); err != nil {
				return // 客户端超时断开,静默退出
			}
			flusher.Flush()
			if i < len(chunks)-1 && interval > 0 {
				time.Sleep(interval)
			}
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestFetch_SlowBodyWithinReadTimeout 核心断言(超时分离):body 分段慢发,
// 总时长远超旧 30s 一刀切语义下 connect 的超时量级,但在 read_timeout 内,
// 拉取必须成功。connect_timeout 远大于单段间隔,证明建连/响应头与体读取是
// 两本账。
func TestFetch_SlowBodyWithinReadTimeout(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte(timeoutTestNode))
	// 拆成 5 段慢发,每段间隔 40ms,总耗时约 200ms。
	var chunks []string
	for i := 0; i < len(encoded); i += 8 {
		end := i + 8
		if end > len(encoded) {
			end = len(encoded)
		}
		chunks = append(chunks, encoded[i:end])
	}
	srv := slowBodyServer(t, chunks, 40*time.Millisecond)

	f := newTestFetcher(2*time.Second, 2*time.Second)
	sub, diag, err := f.FetchWithDiagnostics("测试机场", srv.URL)
	if err != nil {
		t.Fatalf("FetchWithDiagnostics() error = %v, want success for slow body within read timeout", err)
	}
	if len(sub.Nodes) != 1 {
		t.Errorf("nodes = %d, want 1", len(sub.Nodes))
	}
	if diag.TimedOut {
		t.Error("TimedOut = true, want false on success")
	}
	if diag.BodyBytes != int64(len(encoded)) {
		t.Errorf("BodyBytes = %d, want %d", diag.BodyBytes, len(encoded))
	}
}

// TestFetch_SlowBodyExceedsReadTimeout body 总时长超过 read_timeout 必须失败,
// 且诊断标记 TimedOut(与「订阅过大」等错误类区分)。超时是可重试故障,
// 尝试次数应为 3(1 首试 + 2 重试)。
func TestFetch_SlowBodyExceedsReadTimeout(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		flusher, _ := w.(http.Flusher)
		w.Write([]byte("chunk1"))
		flusher.Flush()
		// 第一段后睡过 read_timeout:体读取必被 ctx 截止打断。
		time.Sleep(500 * time.Millisecond)
		w.Write([]byte("chunk2"))
	}))
	t.Cleanup(srv.Close)

	f := newTestFetcher(2*time.Second, 50*time.Millisecond)
	_, diag, err := f.FetchWithDiagnostics("测试机场", srv.URL)
	if err == nil {
		t.Fatal("FetchWithDiagnostics() should fail when body read exceeds read timeout")
	}
	if !diag.TimedOut {
		t.Error("TimedOut = false, want true on body read timeout")
	}
	if got := attempts.Load(); got != maxFetchAttempts {
		t.Errorf("attempts = %d, want %d (timeout is retryable)", got, maxFetchAttempts)
	}
}

// TestFetch_RetriesOnRetryableStatus 5xx 与 429 恰好重试 2 次(共 3 次尝试)。
func TestFetch_RetriesOnRetryableStatus(t *testing.T) {
	for _, status := range []int{http.StatusInternalServerError, http.StatusBadGateway, http.StatusTooManyRequests} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var attempts atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				attempts.Add(1)
				w.WriteHeader(status)
			}))
			t.Cleanup(srv.Close)

			f := newTestFetcher(time.Second, time.Second)
			_, diag, err := f.FetchWithDiagnostics("测试机场", srv.URL)
			if err == nil {
				t.Fatal("FetchWithDiagnostics() should fail on persistent error status")
			}
			if got := attempts.Load(); got != maxFetchAttempts {
				t.Errorf("attempts = %d, want %d for status %d", got, maxFetchAttempts, status)
			}
			if diag.HTTPStatus != status {
				t.Errorf("HTTPStatus = %d, want %d (last attempt)", diag.HTTPStatus, status)
			}
		})
	}
}

// TestFetch_NoRetryOnOther4xx 其他 4xx(404/403)不重试,首试即收口。
func TestFetch_NoRetryOnOther4xx(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusForbidden} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var attempts atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				attempts.Add(1)
				w.WriteHeader(status)
			}))
			t.Cleanup(srv.Close)

			f := newTestFetcher(time.Second, time.Second)
			_, _, err := f.FetchWithDiagnostics("测试机场", srv.URL)
			if err == nil {
				t.Fatal("FetchWithDiagnostics() should fail on 4xx")
			}
			if got := attempts.Load(); got != 1 {
				t.Errorf("attempts = %d, want 1 for status %d (non-retryable)", got, status)
			}
		})
	}
}

// TestFetch_RetrySucceedsAfterTransientFailure 先 503 后 200:第二次尝试成功。
func TestFetch_RetrySucceedsAfterTransientFailure(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte(base64.StdEncoding.EncodeToString([]byte(timeoutTestNode))))
	}))
	t.Cleanup(srv.Close)

	f := newTestFetcher(time.Second, time.Second)
	sub, diag, err := f.FetchWithDiagnostics("测试机场", srv.URL)
	if err != nil {
		t.Fatalf("FetchWithDiagnostics() error = %v, want success after retry", err)
	}
	if len(sub.Nodes) != 1 {
		t.Errorf("nodes = %d, want 1", len(sub.Nodes))
	}
	if got := attempts.Load(); got != 2 {
		t.Errorf("attempts = %d, want 2", got)
	}
	if diag.HTTPStatus != http.StatusOK {
		t.Errorf("HTTPStatus = %d, want 200 (last attempt)", diag.HTTPStatus)
	}
}

// TestFetch_ResponseHeaderTimeout 建连快但响应头迟迟不来:由 transport 的
// ResponseHeaderTimeout(= connect_timeout)打断,诊断标记 TimedOut 且可重试。
func TestFetch_ResponseHeaderTimeout(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		time.Sleep(500 * time.Millisecond) // 不写头,挂起
	}))
	t.Cleanup(srv.Close)

	f := newTestFetcher(50*time.Millisecond, 5*time.Second)
	_, diag, err := f.FetchWithDiagnostics("测试机场", srv.URL)
	if err == nil {
		t.Fatal("FetchWithDiagnostics() should fail on response header timeout")
	}
	if !diag.TimedOut {
		t.Error("TimedOut = false, want true on response header timeout")
	}
	if got := attempts.Load(); got != maxFetchAttempts {
		t.Errorf("attempts = %d, want %d (timeout is retryable)", got, maxFetchAttempts)
	}
}

// TestFetch_BodyTooLarge body 超上限(测试注入小上限)报 ErrSubscriptionTooLarge,
// 与读取超时区分(TimedOut=false),且不可重试。
func TestFetch_BodyTooLarge(t *testing.T) {
	var attempts atomic.Int32
	body := base64.StdEncoding.EncodeToString([]byte(timeoutTestNode))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	f := newTestFetcher(time.Second, time.Second)
	f.maxBodyBytes = 8 // 注入小上限,免造 32MiB 体
	_, diag, err := f.FetchWithDiagnostics("测试机场", srv.URL)
	if err == nil {
		t.Fatal("FetchWithDiagnostics() should fail when body exceeds limit")
	}
	if !errors.Is(err, ErrSubscriptionTooLarge) {
		t.Errorf("err = %v, want errors.Is ErrSubscriptionTooLarge", err)
	}
	if diag.TimedOut {
		t.Error("TimedOut = true, want false (too large is not a timeout)")
	}
	if got := attempts.Load(); got != 1 {
		t.Errorf("attempts = %d, want 1 (too large is non-retryable)", got)
	}
}

// TestFetch_CallerDeadlineNotRetried 调用方 ctx 自带截止到期:不重试
// (重试也会立刻撞同一截止),仍按超时标记。
func TestFetch_CallerDeadlineNotRetried(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		time.Sleep(500 * time.Millisecond)
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	f := newTestFetcher(5*time.Second, 5*time.Second)
	_, diag, err := f.FetchContext(ctx, "测试机场", srv.URL)
	if err == nil {
		t.Fatal("FetchContext() should fail on caller ctx deadline")
	}
	if got := attempts.Load(); got != 1 {
		t.Errorf("attempts = %d, want 1 (caller deadline is not retried)", got)
	}
	if !diag.TimedOut {
		t.Error("TimedOut = false, want true (caller deadline is still a timeout)")
	}
	if !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Errorf("err = %v, want context deadline exceeded", err)
	}
}
