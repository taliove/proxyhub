package detection

import (
	"context"
	"strings"
)

// 深度体检探测重试:高延迟链路(4s+)上单次探测易偶发超时,多地域一半区、解锁/出网
// 也常因一次传输抖动被误判为失败。本文件提供三段共用的重试骨架:
//   - retryResult:通用重试循环(区域/解锁/出网),只返回最后一次结果,重试期间不 emit 中间失败态。
//   - isTransientNetError:传输类失败分类(解锁/出网用),保证判定结论绝不误触发重试。

// examTransientMaxRetries 解锁/出网段网络类失败(超时/连接重置/EOF)的最大重试次数。
// 判定结论(full/blocked/originals_only)与解析失败绝不重试,仅传输抖动重试。
const examTransientMaxRetries = 1

// transientNetTokens 传输类错误的稳定子串标记(按小写正文匹配)。这是结构化判定的兜底:
// 首选 isTransientNetErr(errors.Is/As,见 exam_transient.go),仅当探针未透传结构化 cause 时
// 才退化到此文本匹配。命中即视为可重试的网络抖动;判定结论(状态码类)与解析失败不含这些标记。
var transientNetTokens = []string{
	"timeout",                // i/o timeout、http client timeout
	"deadline exceeded",      // context.DeadlineExceeded
	"connection reset",       // reset by peer
	"reset by peer",          //
	"connection refused",     // 拒绝连接
	"connection closed",      // 连接被关闭
	"broken pipe",            // 写入中断
	"no route to host",       // 无路由
	"network is unreachable", // 网络不可达
	"eof",                    // io.EOF / unexpected EOF
	"超时",                     // IPv6 出口探测超时(纯中文提示,无 Go 错误串)
}

// isTransientNetError 判断探针结果的错误文本是否为传输类(网络抖动)失败。文本兜底路径:
// 结构化判定不可达(探针未透传 cause)时才走此匹配;首选 isTransientNetErr(结构化)。
// 空串(成功)返回 false;判定结论类文本(如 "status 403"、"classification inconclusive")与
// 解析失败文本不含传输标记,返回 false —— 保证判定结论绝不触发重试。
func isTransientNetError(msg string) bool {
	if msg == "" {
		return false
	}
	lower := strings.ToLower(msg)
	for _, tok := range transientNetTokens {
		if strings.Contains(lower, tok) {
			return true
		}
	}
	return false
}

// retryResult 通用重试循环(区域/解锁/出网三段共用):执行 attempt 至多 1+maxRetries 次,
// 每次结果经 retryable 判定 —— 需重试、仍有次数且 ctx 未取消才继续,否则返回当前结果。
// 只返回最后一次结果:调用方在拿到最终结果后才 emit,重试期间不 emit 中间失败态
// (不可变:不原地修改任何已发出的结构,每次 attempt 产出独立新值)。
func retryResult[T any](ctx context.Context, maxRetries int, attempt func() T, retryable func(T) bool) T {
	res := attempt()
	for i := 0; i < maxRetries; i++ {
		if ctx.Err() != nil || !retryable(res) {
			break
		}
		res = attempt()
	}
	return res
}
