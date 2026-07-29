//go:build windows

package xraymgr

import "syscall"

// detachProcAttr is a no-op on Windows: Setpgid does not exist there, and
// process detachment relies on the default behavior.
func detachProcAttr() *syscall.SysProcAttr {
	return nil
}
