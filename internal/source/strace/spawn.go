package strace

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	pathpkg "path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/YasserYG8/Specter/internal/source"
	"github.com/YasserYG8/Specter/internal/util"
)

// Options configures the strace source behavior.
type Options struct {
	FollowTree bool // Follow child processes and threads via -f
}

// Source implements source.Source using Linux strace.
type Source struct {
	mu            sync.Mutex
	cmd           *exec.Cmd
	parser        *Parser
	capturedError string
	processStatus string
	options       Options

	// Notices and tests
	notifier           func(msg string)
	procRoot           string // proc filesystem root, "/proc" when empty
	readlink           func(path string) (string, error)
	isRegularFile      func(path string) bool
	maxLine            int // longest strace line to process, maxStraceLine when zero
	seenFDs            map[[2]int]bool
	seenLogPaths       map[string]bool
	oversizedPending   int
	lastOversizeNotice time.Time
}

// NewSource creates a new StraceSource instance with optional configuration.
func NewSource(opts ...Options) *Source {
	s := &Source{}
	if len(opts) > 0 {
		s.options = opts[0]
	}
	return s
}

// Name returns the identifier for this source mechanism.
func (s *Source) Name() string {
	return "strace"
}

// Start begins tracing the target process PID and streams chunks.
func (s *Source) Start(ctx context.Context, target source.Target) (<-chan source.Chunk, <-chan error) {
	chunks := make(chan source.Chunk, 1024)
	errs := make(chan error, 1)

	// Verify strace is installed upfront
	if installErr := CheckStraceInstalled(); installErr != nil {
		errs <- installErr
		close(chunks)
		close(errs)
		return chunks, errs
	}

	s.parser = NewParser(target.PID)

	args := BuildStraceArgs(target)

	cmd := exec.CommandContext(ctx, "strace", args...)

	// Guarantee clean detachment on context cancellation (send SIGINT to strace, avoiding SIGKILL)
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			return cmd.Process.Signal(os.Interrupt)
		}
		return nil
	}
	cmd.WaitDelay = 3 * time.Second

	stderr, err := cmd.StderrPipe()
	if err != nil {
		errs <- fmt.Errorf("failed to create strace stderr pipe: %w", err)
		close(chunks)
		close(errs)
		return chunks, errs
	}

	if err := cmd.Start(); err != nil {
		errs <- fmt.Errorf("failed to start strace: %w", err)
		close(chunks)
		close(errs)
		return chunks, errs
	}

	s.mu.Lock()
	s.cmd = cmd
	s.mu.Unlock()

	go func() {
		defer close(chunks)
		defer close(errs)

		s.readLoop(ctx, stderr, chunks)

		// Await command termination
		waitErr := cmd.Wait()

		// If exited unexpectedly without context cancellation, diagnose the error
		if waitErr != nil && ctx.Err() == nil {
			s.mu.Lock()
			captured := s.capturedError
			s.mu.Unlock()

			if strings.Contains(captured, "Operation not permitted") {
				errs <- &util.DiagnosticError{
					Err:         util.ErrPtraceDenied,
					Title:       fmt.Sprintf("Cannot attach to PID %d: Operation not permitted", target.PID),
					Explanation: "The kernel blocked ptrace attachment. Process may belong to another user or ptrace is restricted.",
					Remediation: fmt.Sprintf("Run Specter with sudo: sudo specter %d (or setcap cap_sys_ptrace+ep on strace)", target.PID),
				}
				return
			}

			if strings.Contains(captured, "No such process") {
				errs <- fmt.Errorf("%w: PID %d exited or does not exist", util.ErrProcessNotFound, target.PID)
				return
			}

			if captured != "" {
				errs <- fmt.Errorf("strace error: %s", captured)
				return
			}

			errs <- fmt.Errorf("strace exited unexpectedly: %w", waitErr)
		}
	}()

	return chunks, errs
}

// maxStraceLine is the longest strace line Specter processes. At -s 65536 a hex-encoded write is about
// 262 KB, so this leaves room for a long writev vector.
const maxStraceLine = 1 << 20

// oversizeNoticeInterval limits how often the over-limit line notice is printed.
const oversizeNoticeInterval = 5 * time.Second

func (s *Source) readLoop(ctx context.Context, r io.Reader, chunks chan<- source.Chunk) {
	limit := s.maxLine
	if limit <= 0 {
		limit = maxStraceLine
	}
	s.parser.OnOtherFD = s.checkOtherFD

	scanLines(r, limit, func(line string) bool {
		// Capture strace diagnostic messages or errors
		if strings.HasPrefix(line, "strace: ") || strings.Contains(line, "attach:") {
			s.mu.Lock()
			s.capturedError = line
			s.mu.Unlock()
			return true
		}

		// Detect target process termination
		if strings.Contains(line, "+++ exited with ") || strings.Contains(line, "+++ killed by ") {
			s.mu.Lock()
			s.processStatus = strings.TrimSpace(line)
			s.mu.Unlock()
			return true
		}

		chunk, err := s.parser.ParseLine(line, time.Now())
		if err != nil {
			// Skip corrupt line without terminating stream
			return true
		}
		if chunk != nil {
			select {
			case chunks <- *chunk:
			case <-ctx.Done():
				return false
			}
		}
		return true
	}, s.noteOversizedLine)
}

// scanLines calls onLine for each line of r, until onLine returns false or r ends. A line longer than limit
// bytes is discarded up to its end and reported to onTooLong with its length, and reading continues. Unlike
// bufio.Scanner, a long line never ends the loop, so strace's pipe keeps draining and the target is not held up.
func scanLines(r io.Reader, limit int, onLine func(string) bool, onTooLong func(n int)) {
	br := bufio.NewReaderSize(r, 64*1024)
	var acc []byte
	skipped := 0
	tooLong := false

	finish := func() bool {
		defer func() {
			acc = acc[:0]
			skipped = 0
			tooLong = false
		}()
		if tooLong {
			onTooLong(skipped)
			return true
		}
		return onLine(strings.TrimRight(string(acc), "\r\n"))
	}

	for {
		frag, err := br.ReadSlice('\n')
		if len(frag) > 0 {
			switch {
			case tooLong:
				skipped += len(frag)
			case len(acc)+len(frag) > limit:
				tooLong = true
				skipped = len(acc) + len(frag)
				acc = acc[:0]
			default:
				acc = append(acc, frag...)
			}
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		if err != nil {
			// End of stream; a final line without a newline still counts
			if len(acc) > 0 || tooLong {
				finish()
			}
			return
		}
		if !finish() {
			return
		}
	}
}

// SetNotifier registers a function that receives status messages for the user, such as a target logging to
// its own file. The messages are plain text; the caller decides where to print them.
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

// noteOversizedLine reports a strace line that was too long to process. Reports are rate limited so a
// flood of long lines does not flood the terminal; the count covers the lines skipped since the last report.
func (s *Source) noteOversizedLine(n int) {
	s.mu.Lock()
	s.oversizedPending++
	if !s.lastOversizeNotice.IsZero() && time.Since(s.lastOversizeNotice) < oversizeNoticeInterval {
		s.mu.Unlock()
		return
	}
	count := s.oversizedPending
	s.oversizedPending = 0
	s.lastOversizeNotice = time.Now()
	s.mu.Unlock()

	if count == 1 {
		s.notify(fmt.Sprintf("Skipped a strace output line of %d bytes because it is too long to process; capture continues.", n))
	} else {
		s.notify(fmt.Sprintf("Skipped %d strace output lines because they are too long to process; capture continues.", count))
	}
}

// checkOtherFD is called for writes to a descriptor other than stdout and stderr. It resolves each (pid, fd)
// pair once and, when it is a regular file with "log" in its name, tells the user once per path.
func (s *Source) checkOtherFD(pid, fd int) {
	key := [2]int{pid, fd}
	s.mu.Lock()
	if s.seenFDs == nil {
		s.seenFDs = make(map[[2]int]bool)
	}
	if s.seenFDs[key] {
		s.mu.Unlock()
		return
	}
	s.seenFDs[key] = true
	root := s.procRoot
	readlink, isRegular := s.readlink, s.isRegularFile
	s.mu.Unlock()

	if root == "" {
		root = "/proc"
	}
	if readlink == nil {
		readlink = os.Readlink
	}
	if isRegular == nil {
		isRegular = func(path string) bool {
			fi, err := os.Stat(path)
			return err == nil && fi.Mode().IsRegular()
		}
	}

	path, err := readlink(filepath.Join(root, strconv.Itoa(pid), "fd", strconv.Itoa(fd)))
	// Sockets and pipes resolve to names like socket:[123], which are not absolute paths
	if err != nil || !strings.HasPrefix(path, "/") || !strings.Contains(strings.ToLower(pathpkg.Base(path)), "log") {
		return
	}
	if !isRegular(path) {
		return
	}

	s.mu.Lock()
	if s.seenLogPaths == nil {
		s.seenLogPaths = make(map[string]bool)
	}
	already := s.seenLogPaths[path]
	s.seenLogPaths[path] = true
	s.mu.Unlock()
	if already {
		return
	}

	s.notify(fmt.Sprintf("Target writes to its own log file %s, which Specter does not follow. To read it: tail -f %s", path, path))
}

// Stop cleanly terminates the strace process.
func (s *Source) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cmd != nil && s.cmd.Process != nil {
		// Interrupt signal triggers strace to detach cleanly
		return s.cmd.Process.Signal(os.Interrupt)
	}
	return nil
}

// ProcessStatus returns any exit status detected from strace output.
func (s *Source) ProcessStatus() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.processStatus
}

// BuildStraceArgs constructs the command-line arguments for strace execution,
// including multi-PID attachment for pre-existing child worker processes.
func BuildStraceArgs(target source.Target) []string {
	// Build strace command: strace -f -qq -xx -s 65536 -e trace=write,writev -p PID [-p CHILD_PID...]
	// Note: -f is required so strace attaches to all threads of target processes
	args := []string{
		"-f",
		"-qq",
		"-xx",
		"-s", "65536",
		"-e", "trace=write,writev",
		"-p", strconv.Itoa(target.PID),
	}

	seen := map[int]bool{target.PID: true}
	// Limit to max 16 child PIDs to avoid exceeding strace argument constraints
	maxChildren := 16
	count := 0
	for _, childPID := range target.Children {
		if childPID > 0 && !seen[childPID] {
			seen[childPID] = true
			args = append(args, "-p", strconv.Itoa(childPID))
			count++
			if count >= maxChildren {
				break
			}
		}
	}

	return args
}
