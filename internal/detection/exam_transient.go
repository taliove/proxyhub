package detection

import (
	"context"
	"errors"
	"io"
	"net"
	"syscall"
)

// 结构化传输错误标记:探针把 error 降级为字符串前,先在网络边界(mihomo adapter / net)
// 打上结构化 transient 标记,分类器经 errors.Is/As 判定,不再依赖易变的错误文本子串。
// isTransientNetError(文本匹配,见 exam_retry.go)退化为无结构化线索时的兜底。

// errTransientNet 是可重试传输类失败(网络抖动)的哨兵。探针边界经 markTransient 包裹
// 拨号/传输错误,使 errors.Is(err, errTransientNet) 成立;分类器据此判定,措辞变化不影响。
var errTransientNet = errors.New("transient network error")

// transientError 包裹底层 error 并标记为传输类失败:满足 errors.Is(_, errTransientNet),
// 同时经 Unwrap 保留原始错误链(原文案供日志与文本兜底,底层类型供 errors.As)。
type transientError struct{ cause error }

func (e *transientError) Error() string {
	if e.cause == nil {
		return errTransientNet.Error()
	}
	return e.cause.Error()
}

func (e *transientError) Unwrap() error { return e.cause }

func (e *transientError) Is(target error) bool { return target == errTransientNet }

// markTransient 把非 nil 的 err 包裹为传输类失败标记(nil 保持 nil,绝不凭空造错误)。
// 幂等:已含 errTransientNet 标记的错误原样返回,不叠层、不改文案。
func markTransient(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, errTransientNet) {
		return err
	}
	return &transientError{cause: err}
}

// transientSyscallErrs 传输类 syscall 错误码(拨号/读写边界经 net.OpError -> os.SyscallError
// -> syscall.Errno 链路,errors.Is 可达)。这些是可重试的网络抖动,与判定结论无关。
var transientSyscallErrs = []error{
	syscall.ECONNREFUSED, // connection refused
	syscall.ECONNRESET,   // reset by peer
	syscall.ECONNABORTED, // connection aborted
	syscall.EPIPE,        // broken pipe
	syscall.ENETUNREACH,  // network is unreachable
	syscall.EHOSTUNREACH, // no route to host
	syscall.ETIMEDOUT,    // connection timed out
}

// isTransientNetErr 结构化判定 err 是否为传输类(网络抖动)可重试失败,经 errors.Is/As 达成:
//   - errTransientNet 哨兵(探针边界显式标记的拨号/传输失败)
//   - context.DeadlineExceeded(超时;context.Canceled 不算 —— 主动取消重试无意义)
//   - net.Error 且 Timeout()==true(i/o timeout,措辞无关)
//   - io.EOF / io.ErrUnexpectedEOF(连接被对端中途关闭)
//   - 传输类 syscall 错误码(reset/refused/broken pipe/unreachable...)
//
// 判定结论(状态码/blocked/full)与解析失败不含上述结构,返回 false —— 结论绝不误触发重试。
func isTransientNetErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, errTransientNet) {
		return true
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	for _, se := range transientSyscallErrs {
		if errors.Is(err, se) {
			return true
		}
	}
	return false
}

// retryableTransient 判断一次失败的探针结果是否为可重试的传输抖动:结构化线索(cause)优先,
// 文本(msg)兜底。cause 非空且结构化命中即可重试;否则退化到 isTransientNetError 文本匹配
// (兼容尚未透传结构化 error 的边界,不造成覆盖回退)。判定结论(cause 为空、msg 为状态码/
// 解析失败)两条路径都判为不可重试,保证结论绝不误重试。
func retryableTransient(cause error, msg string) bool {
	if isTransientNetErr(cause) {
		return true
	}
	return isTransientNetError(msg)
}
