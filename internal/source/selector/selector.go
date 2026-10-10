package selector

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/specter-cli/specter/internal/source"
	"github.com/specter-cli/specter/internal/source/docker"
	"github.com/specter-cli/specter/internal/source/file"
	"github.com/specter-cli/specter/internal/source/journal"
	"github.com/specter-cli/specter/internal/source/strace"
)

// SupportedSources defines the valid CLI flag options.
var SupportedSources = []string{"docker", "file", "journal", "strace"}

// Selector determines the appropriate log acquisition source for a target.
type Selector struct {
	ForceSource string // User-enforced source from --source flag
	FollowTree  bool   // Follow child processes in strace
	ProcRoot    string // Root proc filesystem (defaults to /proc)
}

// NewSelector creates a new Selector instance.
func NewSelector(forceSource string, followTree bool) *Selector {
	return &Selector{
		ForceSource: strings.ToLower(strings.TrimSpace(forceSource)),
		FollowTree:  followTree,
		ProcRoot:    "/proc",
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

	// 5. Universal default: strace
	return strace.NewSource(strace.Options{FollowTree: s.FollowTree}), "standard process tracing (strace)", nil
}
