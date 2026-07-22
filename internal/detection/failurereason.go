package detection

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"syscall"
)

// 检测失败原因分类(ticket 0017):有限枚举,落库 nodes.detection_fail_reason,
// 透出 nodeView.detection_fail_reason,供节点详情"可用性诊断"区块展示。
// 控制在 6 类(一屏可读),语义覆盖真实检测链路(mihomo 代理)与 TCP 快检的全部失败形态。
const (
	// FailReasonTimeout 连接或请求超时(对端无响应)
	FailReasonTimeout = "timeout"
	// FailReasonRefused 连接被拒绝(对端端口未开放/服务已下线)
	FailReasonRefused = "refused"
	// FailReasonUnreachable 网络不可达(无路由/DNS 解析失败/网络中断)
	FailReasonUnreachable = "unreachable"
	// FailReasonHandshake 握手失败(TLS/协议握手失败、连接被重置、EOF)
	FailReasonHandshake = "handshake"
	// FailReasonProtocol 协议或响应错误(代理配置无法构造 adapter、响应状态/内容不符合预期)
	FailReasonProtocol = "protocol"
	// FailReasonOther 其他/未分类失败(含检测被取消)
	FailReasonOther = "other"
)

// MaxFailDetailLen 失败短详情的最大长度(字符数)。详情仅作排障辅助,
// 分类才是主信号;截断防止底层错误串(可能很长)灌入当前态列。
const MaxFailDetailLen = 200

// TruncateFailDetail 截断失败详情到 MaxFailDetailLen(按 rune,避免截断多字节字符)。
func TruncateFailDetail(s string) string {
	r := []rune(s)
	if len(r) <= MaxFailDetailLen {
		return s
	}
	return string(r[:MaxFailDetailLen])
}

// ClassifyFailure 把结构化错误分类为失败原因枚举。
// 优先 errors.Is/As 结构化判定(net 错误/系统 errno),判不出再退化到文本启发式;
// nil 返回空串(无失败)。
func ClassifyFailure(err error) string {
	if err == nil {
		return ""
	}
	// 超时:context 截止 / net.Error.Timeout / os 层超时
	if errors.Is(err, context.DeadlineExceeded) {
		return FailReasonTimeout
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return FailReasonTimeout
	}
	// 连接被拒绝
	if errors.Is(err, syscall.ECONNREFUSED) {
		return FailReasonRefused
	}
	// 网络/主机不可达
	if errors.Is(err, syscall.EHOSTUNREACH) || errors.Is(err, syscall.ENETUNREACH) {
		return FailReasonUnreachable
	}
	// DNS 解析失败
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return FailReasonUnreachable
	}
	// 握手/传输中断:连接被重置、对端提前关闭
	if errors.Is(err, syscall.ECONNRESET) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return FailReasonHandshake
	}
	// 结构化判不出,退化文本启发式
	return ClassifyFailureText(err.Error())
}

// ClassifyFailureText 从错误文本分类失败原因(兜底路径:只有文本可用时)。
// 空串返回空串;任何命中不了的归为 other,绝不返回枚举外的值。
func ClassifyFailureText(msg string) string {
	if msg == "" {
		return ""
	}
	m := strings.ToLower(msg)
	switch {
	case strings.Contains(m, "timeout"),
		strings.Contains(m, "deadline exceeded"):
		return FailReasonTimeout
	case strings.Contains(m, "connection refused"),
		strings.Contains(m, "refused"):
		return FailReasonRefused
	case strings.Contains(m, "no such host"),
		strings.Contains(m, "no route to host"),
		strings.Contains(m, "network is unreachable"):
		return FailReasonUnreachable
	case strings.Contains(m, "handshake"),
		strings.Contains(m, "tls"),
		strings.Contains(m, "eof"),
		strings.Contains(m, "reset"):
		return FailReasonHandshake
	case strings.Contains(m, "unexpected status"),
		strings.Contains(m, "missing keyword"),
		strings.Contains(m, "blocked:"),
		strings.Contains(m, "proxy adapter"),
		strings.Contains(m, "mihomo"),
		strings.Contains(m, "clash config"):
		return FailReasonProtocol
	default:
		return FailReasonOther
	}
}
