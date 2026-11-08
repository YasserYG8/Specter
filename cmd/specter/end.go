package main

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/YasserYG8/Specter/internal/source"
	"github.com/YasserYG8/Specter/internal/source/strace"
)

// errTargetEnded is returned by RunE when the target stopped on its own, so Specter exits with status 1. The
// message is already printed by reportEnd, so cobra must not print it again.
var errTargetEnded = errors.New("target ended")

// errTargetGone is the cause the process watcher gives when it cancels the source.
var errTargetGone = errors.New("target process is gone")

// drainGrace is how long the pipeline may keep printing what it already read after the target is gone, so the
// last lines before a crash are not lost.
const drainGrace = 500 * time.Millisecond

// endReport is everything known about how a run ended.
type endReport struct {
	Interrupted bool          // the user pressed Ctrl+C (or sent SIGTERM) to Specter, or --duration ran out
	After       time.Duration // set when --duration ran out: how long Specter was asked to stay attached
	PID         int           // the target's PID
	Container   string        // the container name, when the target is a container

	// DockerEnded is set when the Docker log stream ended on its own. ContainerCode is then the exit code if
	// Docker could say.
	DockerEnded   bool
	ContainerCode *int

	// Exit is the target's own exit line from strace, with the code or the signal.
	Exit *strace.ExitStatus

	// WatcherGone is set when the process watcher saw the target disappear.
	WatcherGone bool
}

// reportEnd prints the one closing line about how the run ended and returns the exit status for Specter: 0 when
// the user interrupted it, 1 when the target ended on its own.
func reportEnd(w io.Writer, tag string, r endReport) int {
	switch {
	case r.Interrupted && r.After > 0:
		// The user asked for this stop with --duration, so it is a clean exit like Ctrl+C
		if r.Container != "" {
			fmt.Fprintf(w, "\n%s Detached from container %s after %s.\n", tag, r.Container, formatDuration(r.After))
		} else {
			fmt.Fprintf(w, "\n%s Detached from PID %d after %s.\n", tag, r.PID, formatDuration(r.After))
		}
		return 0

	case r.Interrupted:
		if r.Container != "" {
			fmt.Fprintf(w, "\n%s Detached from container %s cleanly.\n", tag, r.Container)
		} else {
			fmt.Fprintf(w, "\n%s Detached from PID %d cleanly.\n", tag, r.PID)
		}
		return 0

	case r.DockerEnded:
		if r.ContainerCode != nil {
			fmt.Fprintf(w, "\n%s Container %s exited with code %d.\n", tag, r.Container, *r.ContainerCode)
		} else {
			fmt.Fprintf(w, "\n%s Container %s stopped.\n", tag, r.Container)
		}

	case r.Exit != nil && r.Exit.Killed():
		fmt.Fprintf(w, "\n%s Target PID %d was killed by %s.\n", tag, r.PID, r.Exit.Signal)
		if r.Exit.Signal == "SIGKILL" {
			fmt.Fprintf(w, "%s SIGKILL often comes from the kernel's out-of-memory killer. To check: dmesg | grep -i oom\n", tag)
		}

	case r.Exit != nil:
		fmt.Fprintf(w, "\n%s Target PID %d exited with code %d.\n", tag, r.PID, r.Exit.Code)

	case r.WatcherGone:
		// The exit code of a process that is not Specter's child cannot be read once it is gone
		fmt.Fprintf(w, "\n%s Target PID %d exited.\n", tag, r.PID)

	default:
		// The source stopped by itself and nothing says why, for example journalctl ending
		fmt.Fprintf(w, "\n%s Capture of PID %d ended because its log source stopped.\n", tag, r.PID)
	}
	return 1
}

// watchesPID reports whether the process watcher should run for this source. It follows a host PID: strace and
// the file source always do, the journal does when it follows _PID, and Docker never does. A journal that follows
// a systemd unit is left alone, because systemd restarts the unit under a new PID and journalctl keeps following.
func watchesPID(src source.Source, target *source.Target) bool {
	if target.PID <= 0 {
		return false
	}
	switch src.Name() {
	case "docker":
		return false
	case "journal":
		f, ok := src.(interface{ FollowsPID() bool })
		return ok && f.FollowsPID()
	}
	return true
}
