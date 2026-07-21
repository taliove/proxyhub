package detection

import (
	"context"
	"errors"
	"fmt"
	"io"
	"syscall"
	"testing"
)

// fakeTimeoutErr 满足 net.Error 且 Timeout()==true,但 Error() 用非常规措辞,
// 用于验证结构化超时判定不依赖错误文本(mihomo/net 措辞变化仍正确归类)。
type fakeTimeoutErr struct{}

func (fakeTimeoutErr) Error() string   { return "some brand new timeout phrasing v2" }
func (fakeTimeoutErr) Timeout() bool   { return true }
func (fakeTimeoutErr) Temporary() bool { return false }

// --- 结构化传输错误判定:errors.Is/As 可达,措辞无关 ---

func TestIsTransientNetErr_Structured(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil -> not transient", err: nil, want: false},
		{name: "sentinel -> transient", err: errTransientNet, want: true},
		{name: "marked cause -> transient", err: markTransient(errors.New("boom")), want: true},
		{name: "deadline exceeded -> transient", err: context.DeadlineExceeded, want: true},
		{name: "wrapped deadline -> transient", err: fmt.Errorf("get x: %w", context.DeadlineExceeded), want: true},
		{name: "EOF -> transient", err: io.EOF, want: true},
		{name: "unexpected EOF -> transient", err: io.ErrUnexpectedEOF, want: true},
		{name: "ECONNREFUSED -> transient", err: syscall.ECONNREFUSED, want: true},
		{name: "ECONNRESET -> transient", err: syscall.ECONNRESET, want: true},
		{name: "wrapped syscall -> transient", err: fmt.Errorf("dial: %w", syscall.ECONNRESET), want: true},
		{name: "net.Error timeout novel wording -> transient", err: fakeTimeoutErr{}, want: true},
		{name: "context canceled -> not transient", err: context.Canceled, want: false},
		{name: "http status verdict -> not transient", err: errors.New("status 403"), want: false},
		{name: "parse failure -> not transient", err: errors.New("解析 IPv4 出口响应失败"), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isTransientNetErr(tt.err); got != tt.want {
				t.Errorf("isTransientNetErr(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// --- markTransient:标记后 errors.Is 双向可达,原始链保留,幂等 ---

func TestMarkTransient_ReachableAndPreservesChain(t *testing.T) {
	if markTransient(nil) != nil {
		t.Fatal("markTransient(nil) must stay nil (never fabricate errors)")
	}

	base := errors.New("underlying cause")
	marked := markTransient(fmt.Errorf("dial via proxy: %w", base))

	if !errors.Is(marked, errTransientNet) {
		t.Error("marked error must satisfy errors.Is(_, errTransientNet)")
	}
	if !errors.Is(marked, base) {
		t.Error("marked error must preserve the original chain (errors.Is base)")
	}

	// 幂等:重复标记不改变文案、不叠层。
	double := markTransient(marked)
	if !errors.Is(double, errTransientNet) || double.Error() != marked.Error() {
		t.Errorf("markTransient must be idempotent, got %q vs %q", double.Error(), marked.Error())
	}
}

// --- retryableTransient:结构化优先,文本兜底 ---

func TestRetryableTransient_StructuredFirstTextFallback(t *testing.T) {
	tests := []struct {
		name  string
		cause error
		msg   string
		want  bool
	}{
		{
			name:  "structured transient wins even with novel wording",
			cause: fakeTimeoutErr{},
			msg:   "some brand new timeout phrasing v2",
			want:  true,
		},
		{
			name:  "structured marker wins with opaque message",
			cause: markTransient(errors.New("mihomo: xtls handshake aborted")),
			msg:   "request failed: mihomo: xtls handshake aborted",
			want:  true,
		},
		{
			name:  "no cause -> text fallback matches legacy token",
			cause: nil,
			msg:   "dial tcp 203.0.113.7:443: i/o timeout",
			want:  true,
		},
		{
			name:  "no cause + verdict text -> not transient",
			cause: nil,
			msg:   "status 403",
			want:  false,
		},
		{
			name:  "empty -> not transient",
			cause: nil,
			msg:   "",
			want:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := retryableTransient(tt.cause, tt.msg); got != tt.want {
				t.Errorf("retryableTransient(%v, %q) = %v, want %v", tt.cause, tt.msg, got, tt.want)
			}
		})
	}
}

// --- 调用点集成:结构化 cause 使措辞变化的 transient 仍正确重试(文本匹配漏判也不影响) ---

// novelTransientText 一段文本匹配器绝不会命中的"新措辞"传输错误串,
// 用于证明结构化 cause 才是判定依据(而非 transientNetTokens 里的旧子串)。
const novelTransientText = "请求失败: mihomo xtls session aborted mid-handshake (v2 wording)"

// TestWithEgressRetry_StructuredTransientNovelWording 出网 IPv4:结构化 cause 命中即重试捞回,
// 即便 Error 文本用 transientNetTokens 覆盖不到的新措辞。
func TestWithEgressRetry_StructuredTransientNovelWording(t *testing.T) {
	// 前置断言:该文本经旧的文本匹配器判为"非 transient",证明重试只能来自结构化 cause。
	if isTransientNetError(novelTransientText) {
		t.Fatalf("test premise broken: %q should NOT match text tokens", novelTransientText)
	}
	calls := 0
	probe := EgressProbe{
		IPv4: func(context.Context) EgressIPv4 {
			calls++
			if calls == 1 {
				return EgressIPv4{Error: novelTransientText, cause: markTransient(errors.New("xtls aborted"))}
			}
			return EgressIPv4{IP: "203.0.113.7", Country: "United States"}
		},
		IPv6: func(context.Context) EgressIPv6 { return EgressIPv6{Available: false} },
		DNS:  func(context.Context) EgressDNS { return EgressDNS{ResolverIP: "198.51.100.9"} },
	}
	res := withEgressRetry(probe).IPv4(context.Background())
	if res.Error != "" || res.IP != "203.0.113.7" {
		t.Errorf("ipv4 = %+v, want recovered via structured cause", res)
	}
	if calls != 2 {
		t.Errorf("ipv4 calls = %d, want 2 (structured transient retried)", calls)
	}
}

// TestWithEgressRetry_StructuredNonTransientNotRetried 出网 DNS:结构化 cause 为非传输类
// (无 cause、纯解析失败措辞多变)时不重试 —— 结论/解析失败绝不误重试。
func TestWithEgressRetry_StructuredNonTransientNotRetried(t *testing.T) {
	calls := 0
	probe := EgressProbe{
		IPv4: func(context.Context) EgressIPv4 { return EgressIPv4{IP: "203.0.113.7"} },
		IPv6: func(context.Context) EgressIPv6 { return EgressIPv6{Available: false} },
		DNS: func(context.Context) EgressDNS {
			calls++
			// 解析失败:无结构化 cause,文本也非传输类 -> 不重试。
			return EgressDNS{Error: "解析 DNS 出口响应失败: invalid character 'x'"}
		},
	}
	withEgressRetry(probe).DNS(context.Background())
	if calls != 1 {
		t.Errorf("parse failure retried (%d calls), want 1", calls)
	}
}

// TestWithUnlockRetry_StructuredTransientNovelWording 解锁:结构化 cause 命中即重试捞回,
// 措辞变化不影响;判定结论(Level 非空)绝不重试。
func TestWithUnlockRetry_StructuredTransientNovelWording(t *testing.T) {
	if isTransientNetError(novelTransientText) {
		t.Fatalf("test premise broken: %q should NOT match text tokens", novelTransientText)
	}
	calls := 0
	probe := func(context.Context, Target) Result {
		calls++
		if calls == 1 {
			return Result{TargetName: "OpenAI", Error: novelTransientText, cause: fakeTimeoutErr{}}
		}
		return Result{TargetName: "OpenAI", Available: true, Level: LevelFull}
	}
	res := withUnlockRetry(probe)(context.Background(), Target{Name: "OpenAI"})
	if res.Level != LevelFull || !res.Available {
		t.Errorf("unlock = %+v, want recovered full via structured cause", res)
	}
	if calls != 2 {
		t.Errorf("unlock calls = %d, want 2 (structured transient retried)", calls)
	}
}

// TestWithUnlockRetry_VerdictNeverRetried 判定结论(Level 非空)即便带结构化 cause 也绝不重试:
// 结论逻辑不受错误分类改动影响。
func TestWithUnlockRetry_VerdictNeverRetried(t *testing.T) {
	calls := 0
	probe := func(context.Context, Target) Result {
		calls++
		// 明确结论 blocked;cause 即便被误设也不应触发重试(Level 非空短路)。
		return Result{TargetName: "Netflix", Level: LevelBlocked, cause: markTransient(errors.New("x"))}
	}
	res := withUnlockRetry(probe)(context.Background(), Target{Name: "Netflix"})
	if res.Level != LevelBlocked {
		t.Errorf("verdict = %+v, want blocked untouched", res)
	}
	if calls != 1 {
		t.Errorf("verdict retried (%d calls), want 1 (Level!=\"\" short-circuits)", calls)
	}
}

// TestClassifyRegionError_WrappedDeadlineStructured 区域:被包裹的 deadline(经 markTransient
// 的拨号超时)仍归为 timeout,不因包装层/措辞退化为 transport(errors.Is 结构化匹配)。
func TestClassifyRegionError_WrappedDeadlineStructured(t *testing.T) {
	wrapped := markTransient(fmt.Errorf("dial via proxy: %w", context.DeadlineExceeded))
	got := classifyRegionError(wrapped)
	if got != "timeout: 连接超时" {
		t.Errorf("classifyRegionError(wrapped deadline) = %q, want timeout (structured errors.Is)", got)
	}
}
