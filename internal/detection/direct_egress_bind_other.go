//go:build !darwin && !linux

package detection

import "errors"

// bindStrictPlatform 其他平台(Windows 等,本期不支持绑定):尽力语义,
// 由 NewDirectDialer 降级为仅 DoH 并 warn 日志。
const bindStrictPlatform = false

// bindControlFor 平台不支持网卡绑定,固定返回不支持错误(调用方降级)。
func bindControlFor(_ string) (socketControl, error) {
	return nil, errors.New("direct egress: interface bind unsupported on this platform")
}
