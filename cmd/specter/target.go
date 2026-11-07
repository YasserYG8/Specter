package main

import (
	"context"
	"errors"
	"fmt"
	pathpkg "path"
	"strings"

	"github.com/YasserYG8/Specter/internal/resolver"
	"github.com/YasserYG8/Specter/internal/source"
	"github.com/YasserYG8/Specter/internal/util"
)

// maxPort is the highest TCP port number. A larger number cannot be a port, so it can only be a PID.
const maxPort = 65535

// targetResolver is the part of the resolver that numeric arguments and --pid need. It is narrow so tests can
// use a fake.
type targetResolver interface {
	ResolvePort(ctx context.Context, port int) (*source.Target, error)
	ResolvePID(ctx context.Context, pid int) (*source.Target, error)
}

// pidNotFoundError is returned when a PID the user gave does not exist. It wraps util.ErrProcessNotFound.
type pidNotFoundError struct{ pid int }

func (e *pidNotFoundError) Error() string { return fmt.Sprintf("no process with PID %d exists", e.pid) }
func (e *pidNotFoundError) Unwrap() error { return util.ErrProcessNotFound }

// resolveNumericTarget resolves a positive whole number given as the positional argument. A number from 1 to
// 65535 is a port and nothing else: when nothing listens on it, the result is a *resolver.NoProcessOnPortError
// and no process is ever attached, because the process that happens to have that PID is almost never the one the
// user meant. A number above 65535 cannot be a port, so it is a PID.
func resolveNumericTarget(ctx context.Context, res targetResolver, val int) (*source.Target, error) {
	if val > maxPort {
		return resolvePIDTarget(ctx, res, val)
	}

	t, portErr := res.ResolvePort(ctx, val)
	if portErr == nil {
		return t, nil
	}
	if !strings.Contains(portErr.Error(), util.ErrNoProcessFound.Error()) && !errors.Is(portErr, util.ErrNoProcessFound) {
		// Surface permission or tool errors immediately
		return nil, portErr
	}

	var noProcErr *resolver.NoProcessOnPortError
	if !errors.As(portErr, &noProcErr) {
		return nil, portErr
	}
	// Only read /proc to learn whether the number is also a PID, so the message can say how to attach to it
	if proc, err := res.ResolvePID(ctx, val); err == nil && proc != nil {
		noProcErr.HintPID = val
		noProcErr.HintName = processName(proc)
	}
	return nil, noProcErr
}

// resolvePIDTarget resolves a PID the user asked for, whatever its value.
func resolvePIDTarget(ctx context.Context, res targetResolver, pid int) (*source.Target, error) {
	t, err := res.ResolvePID(ctx, pid)
	if err != nil {
		if errors.Is(err, util.ErrProcessNotFound) {
			return nil, &pidNotFoundError{pid: pid}
		}
		return nil, err
	}
	return t, nil
}

// processName is the name of the program a target runs: the base name of its first command-line word.
func processName(t *source.Target) string {
	if len(t.Cmdline) == 0 {
		return ""
	}
	return pathpkg.Base(t.Cmdline[0])
}

// validatePIDFlag checks the --pid values before anything is resolved. The flag accepts several values so that
// several targets can be added later without changing it, but only one is supported now.
func validatePIDFlag(pids []int, args []string) error {
	if len(pids) == 0 {
		return nil
	}
	if len(pids) > 1 {
		return &util.DiagnosticError{
			Err:         util.ErrInvalidFlag,
			Title:       "More than one --pid was given",
			Explanation: "Only one target is supported for now.",
			Remediation: "Give a single PID, for example: specter --pid 14280",
		}
	}
	if len(args) > 0 {
		return &util.DiagnosticError{
			Err:         util.ErrInvalidFlag,
			Title:       fmt.Sprintf("Both a target (%s) and --pid were given", args[0]),
			Explanation: "Give either a target or --pid, not both.",
			Remediation: fmt.Sprintf("Run either: specter %s    or: specter --pid %d", args[0], pids[0]),
		}
	}
	if pids[0] <= 0 {
		return &util.DiagnosticError{
			Err:         util.ErrInvalidFlag,
			Title:       fmt.Sprintf("Invalid --pid %d", pids[0]),
			Explanation: "The PID must be a positive whole number.",
			Remediation: "Find the PID with: ps aux | grep <name>    or: ss -ltnp",
		}
	}
	return nil
}
