//go:build !windows

package xraymgr

import "syscall"

// detachProcAttr puts the child in its own process group so its lifecycle is
// detached from the manager (we track the PID in DB and signal it directly).
func detachProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}
