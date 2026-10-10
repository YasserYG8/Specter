package strace

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/specter-cli/specter/internal/util"
)

// CheckStraceInstalled verifies that strace is installed on the system and provides distro-specific install hints.
func CheckStraceInstalled() error {
	_, err := exec.LookPath("strace")
	if err == nil {
		return nil
	}

	installCmd := "sudo apt install strace"
	if data, readErr := os.ReadFile("/etc/os-release"); readErr == nil {
		content := strings.ToLower(string(data))
		if strings.Contains(content, "fedora") || strings.Contains(content, "rhel") || strings.Contains(content, "centos") {
			installCmd = "sudo dnf install strace"
		} else if strings.Contains(content, "arch") || strings.Contains(content, "manjaro") {
			installCmd = "sudo pacman -S strace"
		} else if strings.Contains(content, "alpine") {
			installCmd = "apk add strace"
		} else if strings.Contains(content, "suse") {
			installCmd = "sudo zypper install strace"
		}
	}

	return &util.DiagnosticError{
		Err:         util.ErrToolMissing,
		Title:       "strace utility is not installed",
		Explanation: "Specter relies on strace to capture stdout and stderr system calls non-invasively.",
		Remediation: fmt.Sprintf("Install strace: %s", installCmd),
	}
}

// CheckPtracePermissions checks /proc/sys/kernel/yama/ptrace_scope and returns an error with remediation if restricted.
func CheckPtracePermissions(procRoot string) error {
	if procRoot == "" {
		procRoot = "/proc"
	}
	scopePath := procRoot + "/sys/kernel/yama/ptrace_scope"
	data, err := os.ReadFile(scopePath)
	if err != nil {
		// Not a yama-enabled kernel or non-Linux system
		return nil
	}

	scope := strings.TrimSpace(string(data))
	switch scope {
	case "0":
		// Normal ptrace allowed
		return nil
	case "1":
		return &util.DiagnosticError{
			Err:         util.ErrPtraceDenied,
			Title:       "Kernel ptrace restricted (yama ptrace_scope = 1)",
			Explanation: "The kernel restricts attaching to processes not spawned by this shell.",
			Remediation: "Run with sudo or grant strace capabilities: sudo setcap cap_sys_ptrace+ep $(which strace)",
		}
	case "2":
		return &util.DiagnosticError{
			Err:         util.ErrPtraceDenied,
			Title:       "Admin-only ptrace (yama ptrace_scope = 2)",
			Explanation: "Only root can attach ptrace to running processes.",
			Remediation: "Run Specter with sudo: sudo specter <port|pid>",
		}
	case "3":
		return &util.DiagnosticError{
			Err:         util.ErrPtraceDenied,
			Title:       "Ptrace disabled (yama ptrace_scope = 3)",
			Explanation: "The kernel completely disables ptrace attachment.",
			Remediation: "Attachment is disallowed by kernel security configuration.",
		}
	}
	return nil
}
