//go:build linux

package ebpf

import (
	"os"
	"runtime"

	"golang.org/x/sys/unix"
)

// Available reports whether the eBPF source can run on this machine, without loading anything: it is Linux, the
// kernel is 5.8 or later, the kernel has type information, and this process may load tracing programs. It returns
// nil, or an *Unavailable that says why in plain words. The real proof is loading the program in Start, which can
// still fail, for example under a kernel lockdown policy.
func Available() error {
	return checkAvailable(realEnvironment())
}

func realEnvironment() environment {
	env := environment{goos: runtime.GOOS}

	var uts unix.Utsname
	if err := unix.Uname(&uts); err == nil {
		env.kernelRelease = unix.ByteSliceToString(uts.Release[:])
	}
	if _, err := os.Stat("/sys/kernel/btf/vmlinux"); err == nil {
		env.btfPresent = true
	}
	if data, err := os.ReadFile("/proc/self/status"); err == nil {
		env.capEff, _ = parseCapEff(string(data))
	}
	return env
}
