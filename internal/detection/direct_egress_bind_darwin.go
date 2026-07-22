//go:build darwin

package detection

import (
	"fmt"
	"net"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// bindStrictPlatform macOS 严格语义:绑定失败即报错(IP_BOUND_IF 无需特权,
// 失败只可能是网卡问题),绝不静默退化——绑定失败意味着出口仍会被 TUN 截获,
// 检测必须报错而非给出不可信结果。
const bindStrictPlatform = true

// bindControlFor 返回把套接字绑定到指定网卡的 Control(macOS IP_BOUND_IF /
// IPV6_BOUND_IF)。构造期先在临时 UDP 套接字上 probe 一次,失败立即报错。
func bindControlFor(iface string) (socketControl, error) {
	ifi, err := net.InterfaceByName(iface)
	if err != nil {
		return nil, fmt.Errorf("direct egress: interface %q not found: %w", iface, err)
	}
	if err := probeBindBoundIf(ifi.Index); err != nil {
		return nil, fmt.Errorf("direct egress: bind to interface %q failed: %w", iface, err)
	}
	idx := ifi.Index
	return func(network, _ string, c syscall.RawConn) error {
		var serr error
		if err := c.Control(func(fd uintptr) {
			if strings.HasSuffix(network, "6") {
				serr = unix.SetsockoptInt(int(fd), unix.IPPROTO_IPV6, unix.IPV6_BOUND_IF, idx)
			} else {
				serr = unix.SetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_BOUND_IF, idx)
			}
		}); err != nil {
			return err
		}
		return serr
	}, nil
}

func probeBindBoundIf(idx int) error {
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	return unix.SetsockoptInt(fd, unix.IPPROTO_IP, unix.IP_BOUND_IF, idx)
}
