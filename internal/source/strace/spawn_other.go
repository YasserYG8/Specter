//go:build !linux

package strace

import "os/exec"

// guardAgainstOrphan does nothing here: the parent-death signal exists only on Linux, and strace runs only on
// Linux. The file exists so Specter still compiles for macOS and Windows.
func guardAgainstOrphan(cmd *exec.Cmd) {}
