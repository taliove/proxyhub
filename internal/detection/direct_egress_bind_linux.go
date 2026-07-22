//go:build linux

package detection

import (
	"fmt"
	"net"
	"syscall"

	"golang.org/x/sys/unix"
)

// bindStrictPlatform Linux 尽力语义:SO_BINDTODEVICE 需要 CAP_NET_RAW/root,
// 而 ProxyHub 服务常非 root 运行;无 TUN 劫持的 Linux 部署绑网卡本无必要。
// 故绑定失败不报错,由 NewDirectDialer 降级为仅 DoH 并 warn 日志。
const bindStrictPlatform = false

// bindControlFor 返回把套接字绑定到指定网卡的 Control(Linux SO_BINDTODEVICE)。
// 构造期 probe 一次:权限不足等失败返回 error(调用方按平台语义降级)。
func bindControlFor(iface string) (socketControl, error) {
	if _, err := net.InterfaceByName(iface); err != nil {
		return nil, fmt.Errorf("direct egress: interface %q not found: %w", iface, err)
	}
	if err := probeBindToDevice(iface); err != nil {
		return nil, fmt.Errorf("direct egress: bind to interface %q failed: %w", iface, err)
	}
	return func(_, _ string, c syscall.RawConn) error {
		var serr error
		if err := c.Control(func(fd uintptr) {
			serr = unix.SetsockoptString(int(fd), unix.SOL_SOCKET, unix.SO_BINDTODEVICE, iface)
		}); err != nil {
			return err
		}
		return serr
	}, nil
}

func probeBindToDevice(iface string) error {
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	return unix.SetsockoptString(fd, unix.SOL_SOCKET, unix.SO_BINDTODEVICE, iface)
}
