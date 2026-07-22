package detection

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"syscall"
	"testing"

	"github.com/taliove/proxyhub/internal/subscription"
)

// TestClassifyFailure_Structured 结构化错误优先经 errors.Is/As 分类,不依赖错误文本。
func TestClassifyFailure_Structured(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"nil", nil, ""},
		{"context deadline", fmt.Errorf("dial: %w", context.DeadlineExceeded), FailReasonTimeout},
		{"conn refused", fmt.Errorf("latency check: %w", &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}), FailReasonRefused},
		{"host unreachable", fmt.Errorf("dial: %w", &net.OpError{Op: "dial", Err: syscall.EHOSTUNREACH}), FailReasonUnreachable},
		{"net unreachable", fmt.Errorf("dial: %w", &net.OpError{Op: "dial", Err: syscall.ENETUNREACH}), FailReasonUnreachable},
		{"dns", fmt.Errorf("dial: %w", &net.DNSError{IsNotFound: true}), FailReasonUnreachable},
		{"conn reset", fmt.Errorf("read: %w", &net.OpError{Op: "read", Err: syscall.ECONNRESET}), FailReasonHandshake},
		{"eof", fmt.Errorf("read: %w", io.ErrUnexpectedEOF), FailReasonHandshake},
		{"unknown", errors.New("something odd happened"), FailReasonOther},
	}
	for _, c := range cases {
		if got := ClassifyFailure(c.err); got != c.want {
			t.Errorf("%s: ClassifyFailure = %q, want %q", c.name, got, c.want)
		}
	}
}

// timeoutNetErr 模拟 net.Error.Timeout()==true 的错误(如 i/o timeout)。
type timeoutNetErr struct{}

func (timeoutNetErr) Error() string   { return "i/o timeout" }
func (timeoutNetErr) Timeout() bool   { return true }
func (timeoutNetErr) Temporary() bool { return true }

// TestClassifyFailure_NetTimeout net.Error.Timeout() 判为 timeout(覆盖 i/o timeout 形态)。
func TestClassifyFailure_NetTimeout(t *testing.T) {
	err := fmt.Errorf("dial tcp 203.0.113.1:443: %w", timeoutNetErr{})
	if got := ClassifyFailure(err); got != FailReasonTimeout {
		t.Errorf("ClassifyFailure = %q, want %q", got, FailReasonTimeout)
	}
}

// TestClassifyFailureText 文本兜底分类:只有错误串可用时(判定类失败/历史文本)也能归类。
func TestClassifyFailureText(t *testing.T) {
	cases := []struct {
		msg  string
		want string
	}{
		{"", ""},
		{"dial tcp: i/o timeout", FailReasonTimeout},
		{"context deadline exceeded", FailReasonTimeout},
		{"connect: connection refused", FailReasonRefused},
		{"lookup x.example.com: no such host", FailReasonUnreachable},
		{"no route to host", FailReasonUnreachable},
		{"network is unreachable", FailReasonUnreachable},
		{"tls: handshake failure", FailReasonHandshake},
		{"unexpected EOF", FailReasonHandshake},
		{"connection reset by peer", FailReasonHandshake},
		{"unexpected status 403", FailReasonProtocol},
		{"missing keyword 'ok'", FailReasonProtocol},
		{"blocked: found 'deny'", FailReasonProtocol},
		{"create proxy adapter: parse mihomo proxy: bad config", FailReasonProtocol},
		{"detection cancelled", FailReasonOther},
		{"unknown weirdness", FailReasonOther},
	}
	for _, c := range cases {
		if got := ClassifyFailureText(c.msg); got != c.want {
			t.Errorf("ClassifyFailureText(%q) = %q, want %q", c.msg, got, c.want)
		}
	}
}

// TestTruncateFailDetail 详情截断:按 rune 截断,不截断多字节字符;短串原样返回。
func TestTruncateFailDetail(t *testing.T) {
	short := "connection refused"
	if got := TruncateFailDetail(short); got != short {
		t.Errorf("短串应原样返回, got %q", got)
	}
	long := strings.Repeat("排", MaxFailDetailLen+50)
	got := TruncateFailDetail(long)
	if len([]rune(got)) != MaxFailDetailLen {
		t.Errorf("截断后长度 = %d, want %d", len([]rune(got)), MaxFailDetailLen)
	}
}

// TestTestNode_FailReason 即时测试产出失败原因分类:
// quick/real 对拒绝连接的地址(127.0.0.1:1)必判 refused;adapter 构造失败判 protocol。
func TestTestNode_FailReason(t *testing.T) {
	d := NewDetector(1, 2_000_000_000, 5_000_000_000) // tcpTimeout=2s, requestTimeout=5s

	refusedNode := &subscription.Node{
		Name: "n", Type: "ss", Server: "127.0.0.1", Port: 1,
		Cipher: "aes-256-gcm", Password: "p",
	}
	for _, mode := range []string{"quick", "real"} {
		res := d.TestNode(context.Background(), refusedNode, mode)
		if res.Available {
			t.Fatalf("%s: 127.0.0.1:1 不应可用", mode)
		}
		if res.FailReason != FailReasonRefused {
			t.Errorf("%s: FailReason = %q, want %q", mode, res.FailReason, FailReasonRefused)
		}
		if res.Error == "" {
			t.Errorf("%s: Error 详情不应为空", mode)
		}
	}

	// adapter 构造失败(不支持的协议类型)-> protocol。
	// 起本地监听让 TCP 快检通过,确保失败发生在 adapter 构造环节(不触真实网络)。
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	badProto := &subscription.Node{
		Name: "n", Type: "bogus-protocol", Server: "127.0.0.1", Port: port,
	}
	res := d.TestNode(context.Background(), badProto, "real")
	if res.Available {
		t.Fatal("bogus 协议不应可用")
	}
	if res.FailReason != FailReasonProtocol {
		t.Errorf("adapter 失败 FailReason = %q, want %q", res.FailReason, FailReasonProtocol)
	}
}
