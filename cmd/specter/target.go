package main

import (
	"context"
	"errors"
	"fmt"
	pathpkg "path"
	"slices"
	"strconv"
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

// maxTargets is how many targets one run can follow. Every target costs a capture session (and an eBPF target
// costs a kernel program), so the number is bounded.
const maxTargets = 8

// validatePIDFlag checks the --pid values before anything is resolved. The flag may be repeated and may be mixed
// with positional targets; every value must be a positive whole number.
func validatePIDFlag(pids []int) error {
	for _, pid := range pids {
		if pid <= 0 {
			return &util.DiagnosticError{
				Err:         util.ErrInvalidFlag,
				Title:       fmt.Sprintf("Invalid --pid %d", pid),
				Explanation: "The PID must be a positive whole number.",
				Remediation: "Find the PID with: ps aux | grep <name>    or: ss -ltnp",
			}
		}
	}
	return nil
}

// validateTargetCount refuses more than maxTargets targets (positional arguments and --pid together). cobra's own
// message would not say what the limit is, so Specter checks it.
func validateTargetCount(args []string, pids []int) error {
	if n := len(args) + len(pids); n > maxTargets {
		return &util.DiagnosticError{
			Err:         util.ErrInvalidFlag,
			Title:       fmt.Sprintf("Too many targets (%d)", n),
			Explanation: fmt.Sprintf("Specter follows at most %d targets in one run.", maxTargets),
			Remediation: fmt.Sprintf("Give %d targets or fewer, or run Specter twice.", maxTargets),
		}
	}
	return nil
}

// targetSpec is one target as the user gave it, before it is resolved.
type targetSpec struct {
	Label string // how the target is named in the output: ":3000", "pid:1234", or the argument as written
	Arg   string // the positional argument; empty for --pid
	PID   int    // the --pid value; 0 for a positional argument
}

// targetLabel names a positional argument. A whole number from 1 to 65535 is a port, a larger one is a PID, and
// anything else (a container or a project) keeps the text the user typed.
func targetLabel(arg string) string {
	if val, err := strconv.Atoi(arg); err == nil && val > 0 {
		if val > maxPort {
			return fmt.Sprintf("pid:%d", val)
		}
		return fmt.Sprintf(":%d", val)
	}
	return arg
}

// buildTargetSpecs lists the targets in the order they were given: positional arguments first, then --pid values.
func buildTargetSpecs(args []string, pids []int) []targetSpec {
	specs := make([]targetSpec, 0, len(args)+len(pids))
	for _, a := range args {
		specs = append(specs, targetSpec{Label: targetLabel(a), Arg: a})
	}
	for _, p := range pids {
		specs = append(specs, targetSpec{Label: fmt.Sprintf("pid:%d", p), PID: p})
	}
	return specs
}

// fullResolver is everything resolving one argument needs. Tests use a fake.
type fullResolver interface {
	targetResolver
	ResolveContainer(ctx context.Context, nameOrID string) (*source.Target, error)
	DiscoverServices(ctx context.Context) ([]resolver.DiscoveredService, error)
}

// resolveSpec resolves one target with the same rules as a single target: --pid is never a port, a positive
// number is a port or (above 65535) a PID, and anything else is tried as a container, then as a project name.
func resolveSpec(ctx context.Context, res fullResolver, spec targetSpec) (*source.Target, error) {
	if spec.Arg == "" {
		return resolvePIDTarget(ctx, res, spec.PID)
	}

	if val, err := strconv.Atoi(spec.Arg); err == nil && val > 0 {
		return resolveNumericTarget(ctx, res, val)
	}

	t, err := res.ResolveContainer(ctx, spec.Arg)
	if err == nil {
		return t, nil
	}
	// Check if the argument matches an active project name
	if services, sErr := res.DiscoverServices(ctx); sErr == nil {
		for _, s := range services {
			if !strings.EqualFold(s.ProjectName, spec.Arg) {
				continue
			}
			if pt := resolveProjectService(ctx, res, s); pt != nil {
				return pt, nil
			}
		}
	}
	return nil, fmt.Errorf("argument %q is not a valid port, container, or project (to attach by PID, use --pid): %w", spec.Arg, err)
}

// resolveProjectService resolves a discovered service of a project to a target, or nil when none of its ways work.
func resolveProjectService(ctx context.Context, res fullResolver, s resolver.DiscoveredService) *source.Target {
	if s.Kind == resolver.ServiceKindDocker {
		if s.Port > 0 {
			if t, err := res.ResolvePort(ctx, s.Port); err == nil {
				return t
			}
		}
		name := s.ContainerName
		if name == "" {
			name = s.ContainerID
		}
		if t, err := res.ResolveContainer(ctx, name); err == nil {
			return t
		}
		return nil
	}
	if s.Port > 0 {
		if t, err := res.ResolvePort(ctx, s.Port); err == nil {
			return t
		}
	} else if s.PID > 0 {
		if t, err := res.ResolvePID(ctx, s.PID); err == nil {
			return t
		}
	}
	return nil
}

// resolvedTarget is a target that was found, with its label.
type resolvedTarget struct {
	Label  string
	Target *source.Target
}

// dedupeTargets removes targets that are the same process or container as an earlier one, and says why in one
// notice per target it drops. It must run after child discovery, so a PID that is a worker of an earlier target
// is known. The first target keeps the label.
func dedupeTargets(in []resolvedTarget) (kept []resolvedTarget, notices []string) {
	for _, cur := range in {
		dup := ""
		for _, k := range kept {
			switch {
			case cur.Target.PID > 0 && cur.Target.PID == k.Target.PID:
				dup = fmt.Sprintf("%s is the same process as %s (PID %d). Following it once, as %s.", cur.Label, k.Label, k.Target.PID, k.Label)
			case cur.Target.PID > 0 && slices.Contains(k.Target.Children, cur.Target.PID):
				dup = fmt.Sprintf("%s (PID %d) is a worker that %s already follows. Following it once, as %s.", cur.Label, cur.Target.PID, k.Label, k.Label)
			case cur.Target.Container != "" && cur.Target.Container == k.Target.Container:
				dup = fmt.Sprintf("%s is the same container as %s (%s). Following it once, as %s.", cur.Label, k.Label, k.Target.Container, k.Label)
			}
			if dup != "" {
				break
			}
		}
		if dup != "" {
			notices = append(notices, dup)
			continue
		}
		kept = append(kept, cur)
	}
	return kept, notices
}
