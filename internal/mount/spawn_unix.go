//go:build !windows

package mount

import "syscall"

func mountDetachAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Setsid: true,
	}
}
