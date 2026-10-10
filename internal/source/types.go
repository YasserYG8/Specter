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
