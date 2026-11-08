package main

import (
	"fmt"

	"github.com/YasserYG8/Specter/internal/util"
)

// systemName is how a GOOS value is shown to the user.
func systemName(goos string) string {
	switch goos {
	case "darwin":
		return "macOS"
	case "windows":
		return "Windows"
	default:
		return goos
	}
}

// unsupportedPlatformError is the diagnostic for running on a system other than Linux. Attaching to a process
// needs ptrace and /proc, so there is no way to do it natively on macOS or Windows. target is what the user
// would pass to Specter (a port, or "--pid N"), so the advice for Windows can repeat it.
func unsupportedPlatformError(goos, target string) error {
	system := systemName(goos)

	var remediation string
	switch goos {
	case "windows":
		remediation = fmt.Sprintf("Run it inside WSL2: wsl specter %s", target)
	case "darwin":
		remediation = "Run your server and Specter inside a Linux VM (for example OrbStack, UTM or Lima) or a Docker container."
	default:
		remediation = "Run your server and Specter on Linux."
	}

	return &util.DiagnosticError{
		Err:         util.ErrUnsupportedPlatform,
		Title:       fmt.Sprintf("Specter needs Linux to attach to a process (this is %s)", system),
		Explanation: fmt.Sprintf("It reads a running process through Linux kernel interfaces (ptrace and /proc), which %s does not provide.", system),
		Remediation: remediation,
	}
}
