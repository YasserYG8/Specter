package strace

import (
	"fmt"
	"os"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/YasserYG8/Specter/internal/source"
	"github.com/YasserYG8/Specter/internal/util"
)

// maxTracerCmd is how much of the tracer's command line goes into a message.
const maxTracerCmd = 60

// tracerInfo describes the process that traces another one. PID 0 means the process is not traced. Every other
// field is empty when it could not be read, and messages use what is known.
type tracerInfo struct {
	PID        int
	Cmd        string // base name of the program, then its arguments, cut to maxTracerCmd
	ParentPID  int
	ParentName string // the "comm" of the tracer's parent, for example "specter"
}

// tracerOf reads who traces pid, from the TracerPid line of /proc/<pid>/status. It never fails: when the status
// cannot be read the process counts as not traced, which leaves today's behavior in place.
func tracerOf(procRoot string, pid int) tracerInfo {
	status, err := os.ReadFile(filepath.Join(procRoot, strconv.Itoa(pid), "status"))
	if err != nil {
		return tracerInfo{}
	}
	tracer := statusInt(string(status), "TracerPid:")
	if tracer <= 0 {
		return tracerInfo{}
	}

	info := tracerInfo{PID: tracer}
	if data, err := os.ReadFile(filepath.Join(procRoot, strconv.Itoa(tracer), "cmdline")); err == nil {
		info.Cmd = formatCmdline(data)
	}
	if data, err := os.ReadFile(filepath.Join(procRoot, strconv.Itoa(tracer), "status")); err == nil {
		if ppid := statusInt(string(data), "PPid:"); ppid > 0 {
			info.ParentPID = ppid
			if comm, err := os.ReadFile(filepath.Join(procRoot, strconv.Itoa(ppid), "comm")); err == nil {
				info.ParentName = strings.TrimSpace(string(comm))
			}
		}
	}
	return info
}

// statusInt returns the number after key on its line of a /proc/<pid>/status file, or 0.
func statusInt(status, key string) int {
	for _, line := range strings.Split(status, "\n") {
		if rest, ok := strings.CutPrefix(line, key); ok {
			n, err := strconv.Atoi(strings.TrimSpace(rest))
			if err != nil {
				return 0
			}
			return n
		}
	}
	return 0
}

// formatCmdline turns the NUL-separated /proc/<pid>/cmdline into "program arg arg", with the program shown by its
// base name and the whole text cut to maxTracerCmd characters.
func formatCmdline(data []byte) string {
	var parts []string
	for _, p := range strings.Split(string(data), "\x00") {
		if p != "" {
			parts = append(parts, p)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	parts[0] = pathpkg.Base(parts[0])
	text := strings.Join(parts, " ")
	if len(text) > maxTracerCmd {
		text = text[:maxTracerCmd-3] + "..."
	}
	return text
}

// attachFailureRegex matches the line strace prints when it cannot attach to a process:
// "strace: attach: ptrace(PTRACE_SEIZE, 773): Operation not permitted".
var attachFailureRegex = regexp.MustCompile(`attach: ptrace\(PTRACE_\w+, (\d+)\)`)

// failedAttachTo reports whether line says that attaching to pid failed.
func failedAttachTo(line string, pid int) bool {
	m := attachFailureRegex.FindStringSubmatch(line)
	if m == nil {
		return false
	}
	n, err := strconv.Atoi(m[1])
	return err == nil && n == pid
}

// isSpecter reports whether a process name is a Specter binary.
func isSpecter(comm string) bool {
	return strings.HasPrefix(comm, "specter")
}

// alreadyTracedError explains that pid has a tracer already. Linux allows only one, so the advice is to stop the
// other tracer, never to use sudo. Specter does not signal the other tracer.
func alreadyTracedError(pid int, t tracerInfo) *util.DiagnosticError {
	title := fmt.Sprintf("PID %d is already being traced by PID %d", pid, t.PID)
	if t.Cmd != "" {
		title += fmt.Sprintf(" (%s)", t.Cmd)
	}

	explanation := "Linux allows only one tracer per process."
	remediation := fmt.Sprintf("Stop or detach the other tracer (PID %d) first, then run this command again.", t.PID)
	if isSpecter(t.ParentName) {
		explanation += fmt.Sprintf(" PID %d was started by %s (PID %d).", t.PID, t.ParentName, t.ParentPID)
		remediation = "Stop the other Specter first (Ctrl+C in its terminal), then run this command again."
	}

	return &util.DiagnosticError{
		Err:         util.ErrAlreadyTraced,
		Title:       title,
		Explanation: explanation,
		Remediation: remediation,
	}
}

// checkTracers runs before strace starts. It returns an error when the target itself is already traced, and
// otherwise the target without the workers that are traced by someone else, which strace could not attach to.
// A notice names those workers, so their missing output is explained.
func (s *Source) checkTracers(target source.Target) (source.Target, error) {
	root, _ := s.procAccess()

	if t := tracerOf(root, target.PID); t.PID != 0 {
		return target, alreadyTracedError(target.PID, t)
	}

	var kept []int
	var traced []string // "PID 1039 (traced by PID 1500)"
	var first tracerInfo
	var firstPID int
	for _, child := range target.Children {
		if t := tracerOf(root, child); t.PID != 0 {
			if len(traced) == 0 {
				first, firstPID = t, child
			}
			traced = append(traced, fmt.Sprintf("PID %d (traced by PID %d)", child, t.PID))
			continue
		}
		kept = append(kept, child)
	}
	switch len(traced) {
	case 0:
	case 1:
		target.Children = kept
		s.notify(fmt.Sprintf("Worker PID %d is already traced by PID %d; its output is not captured.", firstPID, first.PID))
	default:
		target.Children = kept
		s.notify(fmt.Sprintf("Workers %s are already traced; their output is not captured.", strings.Join(traced, ", ")))
	}
	return target, nil
}

// diagnoseAttach turns the last strace error line into the error shown to the user. "Operation not permitted"
// is checked against the target's TracerPid again, because a tracer can appear between the pre-check and the
// attach, and then sudo would not help.
func (s *Source) diagnoseAttach(captured string, target source.Target) error {
	switch {
	case strings.Contains(captured, "Operation not permitted"):
		root, _ := s.procAccess()
		if t := tracerOf(root, target.PID); t.PID != 0 {
			return alreadyTracedError(target.PID, t)
		}
		return &util.DiagnosticError{
			Err:         util.ErrPtraceDenied,
			Title:       fmt.Sprintf("Cannot attach to PID %d: Operation not permitted", target.PID),
			Explanation: "The kernel blocked ptrace attachment. Process may belong to another user or ptrace is restricted.",
			Remediation: fmt.Sprintf("Run Specter with sudo: sudo specter --pid %d (or setcap cap_sys_ptrace+ep on strace)", target.PID),
		}
	case strings.Contains(captured, "No such process"):
		return fmt.Errorf("%w: PID %d exited or does not exist", util.ErrProcessNotFound, target.PID)
	case captured != "":
		return fmt.Errorf("strace error: %s", captured)
	}
	return nil
}
