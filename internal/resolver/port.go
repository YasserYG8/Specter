package resolver

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/specter-cli/specter/internal/util"
)

// CommandRunner executes a system command and returns stdout or an error.
type CommandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

// DefaultCommandRunner executes commands using os/exec.
func DefaultCommandRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, stderr.String())
	}
	return stdout.Bytes(), nil
}

// PortResolver resolves a listening TCP port to an active PID.
type PortResolver struct {
	runner CommandRunner
}

// NewPortResolver creates a PortResolver with the given CommandRunner.
func NewPortResolver(runner CommandRunner) *PortResolver {
	if runner == nil {
		runner = DefaultCommandRunner
	}
	return &PortResolver{runner: runner}
}

// isToolNotFoundError checks if an error indicates the command binary was not found.
func isToolNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not found") || strings.Contains(msg, "no such file or directory")
}

// ResolvePortToPID finds the PID listening on the given TCP port.
// It tries `lsof` first, falling back to `ss` if `lsof` is missing or fails.
func (r *PortResolver) ResolvePortToPID(ctx context.Context, port int) (int, error) {
	if port <= 0 || port > 65535 {
		return 0, fmt.Errorf("invalid port number: %d", port)
	}

	var lsofMissing, ssMissing bool

	// 1. Try lsof: lsof -tiTCP:PORT -sTCP:LISTEN
	pids, err := r.resolveWithLsof(ctx, port)
	if err == nil && len(pids) > 0 {
		return pids[0], nil
	}
	if isToolNotFoundError(err) {
		lsofMissing = true
	}

	// 2. Fallback to ss: ss -ltnp
	pids, portListeningWithoutPID, ssErr := r.resolveWithSS(ctx, port)
	if ssErr == nil && len(pids) > 0 {
		return pids[0], nil
	}
	if isToolNotFoundError(ssErr) {
		ssMissing = true
	}

	// Case: Port is listening in ss, but PID is hidden due to permissions (owned by root/another user)
	if portListeningWithoutPID {
		return 0, &util.DiagnosticError{
			Err:         util.ErrPermissionDenied,
			Title:       fmt.Sprintf("Listening process on port %d is owned by another user or root", port),
			Explanation: "The process listening on this port cannot be identified without root privileges.",
			Remediation: fmt.Sprintf("Run Specter with sudo: sudo specter %d", port),
		}
	}

	// Case: Neither lsof nor ss is installed
	if lsofMissing && ssMissing {
		return 0, &util.DiagnosticError{
			Err:         util.ErrToolMissing,
			Title:       "Network inspection utilities not found",
			Explanation: "Neither 'lsof' nor 'ss' (iproute2) is available in PATH.",
			Remediation: "Install lsof or iproute2: sudo apt install lsof (or dnf/pacman)",
		}
	}

	return 0, fmt.Errorf("%w: %d", util.ErrNoProcessFound, port)
}

func (r *PortResolver) resolveWithLsof(ctx context.Context, port int) ([]int, error) {
	out, err := r.runner(ctx, "lsof", fmt.Sprintf("-tiTCP:%d", port), "-sTCP:LISTEN")
	if err != nil {
		return nil, err
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var pids []int
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if pid, err := strconv.Atoi(line); err == nil && pid > 0 {
			pids = append(pids, pid)
		}
	}
	return pids, nil
}

// ssPIDPattern matches `pid=1234` in ss output (e.g. users:(("node",pid=4920,fd=18)))
var ssPIDPattern = regexp.MustCompile(`pid=(\d+)`)

func (r *PortResolver) resolveWithSS(ctx context.Context, port int) ([]int, bool, error) {
	// ss -ltnp sport = :PORT
	out, err := r.runner(ctx, "ss", "-H", "-ltnp", fmt.Sprintf("sport = :%d", port))
	if err != nil {
		// Try without filter arguments if the syntax fails
		out, err = r.runner(ctx, "ss", "-ltnp")
		if err != nil {
			return nil, false, err
		}
	}

	// Accurate port matching regex: must match :<port> followed by whitespace or end of field
	// e.g. "0.0.0.0:3000 " or "[::]:3000 " or "*:3000 "
	portRegex := regexp.MustCompile(fmt.Sprintf(`(?::|\b)%d\s+`, port))

	lines := strings.Split(string(out), "\n")
	var pids []int
	seen := make(map[int]bool)
	portListeningWithoutPID := false

	for _, line := range lines {
		// Verify this line is actually listening on our exact port
		if !portRegex.MatchString(line) {
			continue
		}

		// Check if process info is present
		matches := ssPIDPattern.FindAllStringSubmatch(line, -1)
		if len(matches) == 0 {
			// Port is in LISTEN state, but users:(...) was stripped due to non-root permissions!
			portListeningWithoutPID = true
			continue
		}

		for _, m := range matches {
			if len(m) >= 2 {
				if pid, err := strconv.Atoi(m[1]); err == nil && pid > 0 && !seen[pid] {
					seen[pid] = true
					pids = append(pids, pid)
				}
			}
		}
	}
	return pids, portListeningWithoutPID, nil
}
