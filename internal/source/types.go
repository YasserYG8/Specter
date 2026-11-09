package source

import (
	"context"
	"time"
)

// Chunk represents a contiguous slice of bytes emitted by a traced process.
type Chunk struct {
	Bytes []byte    // Raw decoded payload
	FD    int       // 1 for stdout, 2 for stderr
	PID   int       // Thread or process ID that emitted the write
	TS    time.Time // Timestamp when the chunk was captured
}

// Target represents an inspected process and its system stream destinations.
type Target struct {
	Port      int      // Listening TCP port
	PID       int      // Resolved process ID
	Cmdline   []string // Executed command arguments
	Cwd       string   // Current working directory
	Stdout    string   // Target path or descriptor type of fd 1 (e.g. /dev/pts/1, /dev/null, pipe:[...])
	Stderr    string   // Target path or descriptor type of fd 2
	Container string   // Docker container ID or name if containerized
	Unit      string   // Systemd service unit if running under systemd
	Children  []int    // Active child process PIDs (e.g. Uvicorn/Gunicorn workers)
}

// Source defines the interface for acquiring output chunks from a target process.
type Source interface {
	// Name returns the identifier of the source mechanism (e.g. "strace", "docker", "file", "journal").
	Name() string

	// Start begins streaming chunks from the target process until the context is canceled or process exits.
	Start(ctx context.Context, target Target) (<-chan Chunk, <-chan error)

	// Stop cleanly detaches or terminates the stream collector.
	Stop() error
}

// ExitStatus is how a followed process ended: with an exit code, or killed by a signal. Every engine reports the
// end of its target in this form, so the code that prints "exited with code 3" does not care which engine ran.
type ExitStatus struct {
	PID    int
	Code   int    // exit code, when Signal is empty
	Signal string // signal name such as "SIGKILL" when the process was killed

	// Unprefixed is true when the line had no "[pid N]" prefix, so PID is only the parser's default. strace
	// leaves the prefix off while a single process is traced, which is true for a target that is alone, and also
	// for the last child still running after the target ended. Other engines leave it false.
	Unprefixed bool
}

// Killed reports whether the process ended because of a signal.
func (e ExitStatus) Killed() bool { return e.Signal != "" }
