package selector

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/YasserYG8/Specter/internal/source"
	"github.com/YasserYG8/Specter/internal/source/docker"
	"github.com/YasserYG8/Specter/internal/source/ebpf"
	"github.com/YasserYG8/Specter/internal/source/file"
	"github.com/YasserYG8/Specter/internal/source/journal"
	"github.com/YasserYG8/Specter/internal/source/strace"
	"github.com/YasserYG8/Specter/internal/util"
)

// SupportedSources defines the valid CLI flag options.
var SupportedSources = []string{"docker", "file", "journal", "ebpf", "strace"}

// Selector determines the appropriate log acquisition source for a target.
type Selector struct {
	ForceSource string // User-enforced source from --source flag
	FollowTree  bool   // Follow child processes in strace
	ProcRoot    string // Root proc filesystem (defaults to /proc)

	// CheckEBPF reports why the eBPF engine cannot run here, or nil when it can. It looks only at the system (the
	// kernel, its type information, the privileges) and loads nothing. Tests replace it.
	CheckEBPF func() error

	// EBPFNote is set by Select. When strace was chosen automatically because eBPF cannot run, it holds the reason
	// in one sentence, such as "Run with sudo to use the fast eBPF engine.", for the slowdown notice. It stays empty
	// when eBPF was chosen, when another source was chosen, and when the user asked for strace with --source.
	EBPFNote string
}

// NewSelector creates a new Selector instance.
func NewSelector(forceSource string, followTree bool) *Selector {
	return &Selector{
		ForceSource: strings.ToLower(strings.TrimSpace(forceSource)),
		FollowTree:  followTree,
		ProcRoot:    "/proc",
		CheckEBPF:   ebpf.Available,
	}
}

// checkEBPF runs the availability check, which a Selector built without NewSelector may not have.
func (s *Selector) checkEBPF() error {
	if s.CheckEBPF == nil {
		return ebpf.Available()
	}
	return s.CheckEBPF()
}

// ForcedEBPFError is the diagnostic for --source ebpf when eBPF cannot run: on a machine that fails the checks, or
// when the kernel refuses to load the program. It names the reason and the remedy, and nothing falls back to strace.
func ForcedEBPFError(err error) error {
	var u *ebpf.Unavailable
	if errors.As(err, &u) {
		return &util.DiagnosticError{
			Err:         util.ErrEBPFUnavailable,
			Title:       "The eBPF source cannot run here",
			Explanation: u.Explanation,
			Remediation: u.Remedy,
		}
	}
	return &util.DiagnosticError{
		Err:         util.ErrEBPFUnavailable,
		Title:       "The eBPF source cannot run here",
		Explanation: err.Error(),
		Remediation: "Leave out --source ebpf to use strace.",
	}
}

// Select picks the optimal source for target, returning the source instance, an explanation, or an error.
func (s *Selector) Select(target *source.Target) (source.Source, string, error) {
	// 1. Check user override
	if s.ForceSource != "" {
		switch s.ForceSource {
		case "docker":
			return docker.NewSource(), "forced via --source docker", nil
		case "file":
			return file.NewSource(), "forced via --source file", nil
		case "journal":
			return journal.NewSource(), "forced via --source journal", nil
		case "ebpf":
			// Forced eBPF never falls back: if it cannot run, say why before anything is attached
			if err := s.checkEBPF(); err != nil {
				return nil, "", ForcedEBPFError(err)
			}
			return ebpf.NewSource(ebpf.Options{FollowTree: s.FollowTree}), "forced via --source ebpf", nil
		case "strace":
			return strace.NewSource(strace.Options{FollowTree: s.FollowTree}), "forced via --source strace", nil
		default:
			return nil, "", fmt.Errorf("invalid --source %q (supported: %s)", s.ForceSource, strings.Join(SupportedSources, ", "))
		}
	}

	// 2. Check Docker container
	containerID := target.Container
	if containerID == "" && target.PID > 0 {
		if cid, err := docker.DetectContainer(target.PID, s.ProcRoot); err == nil && cid != "" {
			containerID = cid
		}
	}
	if containerID != "" {
		if _, err := exec.LookPath("docker"); err == nil {
			target.Container = containerID
			shortID := containerID
			if len(shortID) > 12 {
				shortID = shortID[:12]
			}
			return docker.NewSource(), fmt.Sprintf("detected Docker container (%s)", shortID), nil
		}
		if target.PID <= 0 {
			return nil, "", fmt.Errorf("docker executable not found in PATH to attach to container %q", containerID)
		}
	}

	if target.PID <= 0 && target.Container == "" {
		return nil, "", fmt.Errorf("target must have a valid PID or container name")
	}

	// 3. Check Systemd Service Unit
	unit := target.Unit
	if unit == "" && target.PID > 0 {
		if u, err := journal.DetectSystemdUnit(target.PID, s.ProcRoot); err == nil && u != "" {
			unit = u
		}
	}
	if unit != "" && journal.IsJournalctlAvailable() {
		target.Unit = unit
		return journal.NewSource(), fmt.Sprintf("detected systemd service (%s)", unit), nil
	}

	// 4. Check Regular Log Files
	if file.IsRegularFile(target.Stdout) || file.IsRegularFile(target.Stderr) {
		return file.NewSource(), "detected regular log file target", nil
	}
	if target.PID > 0 {
		if fileTargets, err := file.DetectFileTargets(target.PID, s.ProcRoot); err == nil && fileTargets.HasRegularFiles() {
			if fileTargets.StdoutPath != "" {
				target.Stdout = fileTargets.StdoutPath
			}
			if fileTargets.StderrPath != "" {
				target.Stderr = fileTargets.StderrPath
			}
			return file.NewSource(), "detected regular log file target", nil
		}
	}

	// 5. eBPF, when this machine can run it: it captures without stopping the target
	if err := s.checkEBPF(); err == nil {
		return ebpf.NewSource(ebpf.Options{FollowTree: s.FollowTree}), "kernel capture, the target is not slowed", nil
	} else {
		s.EBPFNote = err.Error()
	}

	// 6. Universal default: strace, which works everywhere but slows the target
	return strace.NewSource(strace.Options{FollowTree: s.FollowTree}), "standard process tracing (strace)", nil
}
