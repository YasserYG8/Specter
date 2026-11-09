//go:build !linux

package ebpf

import "runtime"

// Available reports that eBPF cannot run: it needs Linux.
func Available() error {
	return checkAvailable(environment{goos: runtime.GOOS, goarch: runtime.GOARCH})
}
