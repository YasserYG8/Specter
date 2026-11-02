//go:build linux

package main

import (
	"os"
	"syscall"
	"unsafe"
)

// writevStdout writes the parts to stdout with a single writev(2) call, the way Ruby's puts and musl's stdio do.
func writevStdout(parts ...[]byte) error {
	iovs := make([]syscall.Iovec, len(parts))
	for i, p := range parts {
		if len(p) > 0 {
			iovs[i].Base = &p[0]
		}
		iovs[i].SetLen(len(p))
	}
	_, _, errno := syscall.Syscall(syscall.SYS_WRITEV, os.Stdout.Fd(), uintptr(unsafe.Pointer(&iovs[0])), uintptr(len(iovs)))
	if errno != 0 {
		return errno
	}
	return nil
}
