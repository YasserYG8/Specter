//go:build linux

package strace

import (
	"os/exec"
	"syscall"
)

// guardAgainstOrphan makes the kernel send SIGINT to strace when Specter dies, whatever the reason, including
// SIGKILL, which Specter cannot catch. strace then detaches from the target the same way as on a normal stop.
// Without this, a killed Specter leaves strace tracing the target until the target next writes a line.
//
// The kernel ties the signal to the thread that started strace, not to the process. The caller must run on a thread
// that lives as long as Specter does: cmd/specter locks the main goroutine to its thread for that reason.
func guardAgainstOrphan(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Pdeathsig = syscall.SIGINT
}
