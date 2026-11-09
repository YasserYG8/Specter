// Package ebpf captures a target's output with eBPF programs that run inside the kernel. Unlike the strace source it
// never stops the target: the programs watch the write and writev system calls, keep only the bytes that belong to
// the target's output, and hand them to Specter through a ring buffer.
//
// The compiled BPF program is committed (specter_bpfel.o and specter_bpfel.go), so building Specter needs no C
// compiler. See gen_linux.go to regenerate it.
package ebpf

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/YasserYG8/Specter/internal/source"
)

// ErrNeedsLinux is reported by Start on systems other than Linux, where Specter compiles but cannot capture.
var ErrNeedsLinux = errors.New("eBPF needs Linux")

// Options configures the source.
type Options struct {
	// FollowTree also follows descendants of the target's direct children, up to 3 levels below the target, as
	// the strace source does with --tree.
	FollowTree bool

	// RingBytes is the size of the ring buffer between the kernel and Specter, a power of two that is a multiple of
	// the page size. Zero keeps the default of 8 MB. Tests set a small one to make a burst overflow it.
	RingBytes uint32
}

// Source is the eBPF capture engine. It implements source.Source.
type Source struct {
	opts Options

	mu           sync.Mutex
	stop         func() // set by Start; closes the programs, the maps and the ring buffer reader
	notifier     func(msg string)
	exit         *source.ExitStatus // how the target ended, once the kernel has reported it
	seenLogPaths map[string]bool
	sess         interface{ counters() string } // the running session, kept for diagnostics (Linux only)
}

var _ source.Source = (*Source)(nil)

// NewSource returns a source that has not started yet.
func NewSource(opts Options) *Source {
	if opts.RingBytes == 0 {
		opts.RingBytes = ringBytesFrom(os.Getenv(ringBytesEnv))
	}
	return &Source{opts: opts}
}

// Name returns the identifier of the engine, as it appears on the Source: line and in --source.
func (s *Source) Name() string {
	return "ebpf"
}

// Stop detaches the programs. It is safe to call more than once, and before Start.
func (s *Source) Stop() error {
	s.mu.Lock()
	stop := s.stop
	s.stop = nil
	s.mu.Unlock()
	if stop != nil {
		stop()
	}
	return nil
}

// SetNotifier registers a function that receives status messages for the user, such as a target logging to its own
// file or writes that were lost. The messages are plain text; the caller decides where to print them.
func (s *Source) SetNotifier(fn func(msg string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notifier = fn
}

func (s *Source) notify(msg string) {
	s.mu.Lock()
	fn := s.notifier
	s.mu.Unlock()
	if fn != nil {
		fn(msg)
	}
}

// ExitStatus returns how the target ended, when the kernel has reported it. The second result is false while the
// target is still running.
func (s *Source) ExitStatus() (source.ExitStatus, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.exit == nil {
		return source.ExitStatus{}, false
	}
	return *s.exit, true
}

// ringBytesEnv is a test-only knob: it sets the size of the ring buffer in bytes, so an E2E test can make a burst
// overflow a small one. Users have no reason to set it. A value that is not a power of two of at least one page
// (4096) is ignored, because the kernel would refuse the ring buffer.
const ringBytesEnv = "SPECTER_EBPF_RING_BYTES"

// ringBytesFrom reads a ring buffer size from the text of the environment variable. It returns 0, which keeps the
// default, for an empty, invalid or unusable value.
func ringBytesFrom(text string) uint32 {
	n, err := strconv.ParseUint(strings.TrimSpace(text), 10, 32)
	if err != nil || n < 4096 || n&(n-1) != 0 {
		return 0
	}
	return uint32(n)
}
