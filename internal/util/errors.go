package util

import (
	"errors"
	"fmt"
)

var (
	// ErrNoProcessFound indicates no process is listening on the requested port.
	ErrNoProcessFound = errors.New("no process listening on port")

	// ErrProcessNotFound indicates the specified PID does not exist or has terminated.
	ErrProcessNotFound = errors.New("target process not found")

	// ErrPtraceDenied indicates attachment was denied by kernel security policy (ptrace_scope).
	ErrPtraceDenied = errors.New("ptrace attachment denied")

	// ErrPermissionDenied indicates process information cannot be inspected without elevated privileges.
	ErrPermissionDenied = errors.New("permission denied")

	// ErrToolMissing indicates a required system utility (strace, lsof, ss) is not installed.
	ErrToolMissing = errors.New("required system utility not found")

	// ErrUnsupportedPlatform indicates the current OS is not supported natively.
	ErrUnsupportedPlatform = errors.New("unsupported platform: Specter requires Linux or WSL2")
)

// DiagnosticError provides user-actionable instructions alongside a system error.
type DiagnosticError struct {
	Err         error
	Title       string
	Explanation string
	Remediation string
}

func (e *DiagnosticError) Error() string {
	if e.Remediation != "" {
		return fmt.Sprintf("%s: %s\nFix: %s", e.Title, e.Explanation, e.Remediation)
	}
	return fmt.Sprintf("%s: %s", e.Title, e.Explanation)
}

func (e *DiagnosticError) Unwrap() error {
	return e.Err
}
